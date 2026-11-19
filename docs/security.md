# Security model (as implemented)

Every claim below points at code. Anything not verifiable in the tree is
marked UNVERIFIED inline.

## 1. Trust boundaries

| Boundary | Untrusted side | Trusted side | Enforcement |
|---|---|---|---|
| MCP transport | Model / client args over stdio | `ToolHandlers` | JSON arg parsing per handler; `DecodeOperation` strict validation (`internal/operations/decode.go`) |
| Filesystem | Absolute paths from the model | Host FS | `ResolvePath` / `ResolveDest` containment (`internal/storage/storage.go`), roots = config `AllowedRoots` + store dir + cwd (`internal/server/fs_handlers.go:effectiveRoots`) |
| Plugin bridge | Local WS peer | Server process | Per-launch token, loopback-only pairing, origin gates (`internal/plugin/bridge.go`) |
| Python engine | `runner.py` stdout | Go bridge | NDJSON parse, id correlation, 64 MiB response cap, kill-on-timeout (`internal/engine/bridge.go`) |
| Document Builder | Builder binary output | Server process | Optional backend; argv-only exec, minimal env, throwaway workdir (`internal/builder/builder.go`) |
| Document content | `.docx/.xlsx/.pptx` bytes | Parsers | Capped readers (`internal/ooxml/edit.go`, `internal/validator/validator.go`, `internal/server/fs_handlers.go`) |

## 2. Filesystem sandbox

- Reads resolve with `storage.ResolvePath`: NUL rejected, deepest existing
  ancestor `Lstat`ed (never followed), symlinks evaluated, result must sit
  inside a root. A leaf symlink is *followed* for reads.
- Writes/deletes resolve with `storage.ResolveDest`: parent resolved, leaf
  symlink refused, never followed.
- Zip extraction (`handleExtractZip`, `internal/server/fs_handlers.go`):
  absolute paths, `..` segments, symlink entries and symlink escapes
  (parent re-resolved after `MkdirAll`) are all refused; entry count capped
  at `MaxZipEntries` (1000) and total bytes at `MaxZipUncompressed`
  (256 MiB).
- Write path: backups via `O_EXCL` create with `0600`
  (`internal/server/fs_handlers.go:writeBackup`); reads refuse files over
  `MaxReadBytes` (32 MiB); writes over `MaxWriteBytes` (64 MiB) are refused.
- Constants live in `internal/storage/storage.go`
  (`MaxWriteBytes`, `MaxReadBytes`, `MaxWalkEntries`, `MaxZipEntries`,
  `MaxZipUncompressed`, `MaxCreateZipSources`).

Known gaps:

- **TOCTOU**: resolution and use are separate syscalls; a swapped symlink
  between `ResolvePath` and `open` is not detected (no `openat2` /
  `RESOLVE_IN_ROOT` usage). Window is small; attacker needs local write
  access to the path being resolved.
- **Hardlinks**: nothing in the tree inspects hardlinks (no matches for
  link-related calls outside tests). A hardlink to an outside file inside
  an allowed root would read as a regular file. UNVERIFIED whether any
  caller mitigates this; treat as a gap.

## 3. Bridge auth (`internal/plugin/bridge.go`)

- Token: 32 random bytes per launch (`newToken`), persisted `0600` at
  `~/.config/mcp-onlyoffice/bridge-token` (`persistToken`), `0600` dir
  `0700`. Presented as `?token=` or `Authorization: Bearer`, compared with
  `subtle.ConstantTimeCompare` (`validToken`).
- Pairing: `/pair/challenge` issues a single-use nonce (60 s TTL, bound to
  caller IP, max 32 outstanding, oldest evicted). `/pair` consumes it
  before returning the token. Both endpoints are loopback-only and refuse
  browser-page origins (`allowedPairOrigin`: only empty / `null` /
  `file://` / `onlyoffice://plugin`).
- WebSocket: origin gate (`allowedWSOrigin`) additionally allows loopback
  `http(s)`; token required; first frame must be `hello` within 5 s;
  protocol version must match `version.BridgeProtocol`; unknown
  `plugin_instance_id` closes the connection.
- DoS bounds: 1 MiB frame cap (`maxMessageBytes`), 8 sessions
  (`registry.go:MaxSessions`), 32 in-flight commands per session
  (`MaxPendingPerSession`); unsolicited/duplicate responses are counted
  and ignored, never matched to another request.

## 4. Engine environment inheritance

- The Python child **inherits the full parent environment** plus
  `MCP_ONLYOFFICE_ENGINE_OUTPUT_DIR` (`internal/engine/bridge.go:doStartLocked`).
  No shell is involved (`exec.CommandContext(python, runner.py)`), and the
  capability name must match `module.method` (`validateCapability`), but
  secrets in the server's env (e.g. tokens) are visible to `runner.py`.
  Treat the engine as trusted-with-secrets, not sandboxed.
- Responses are capped at 64 MiB (`MaxResponseSize`); oversize kills the
  engine to resync the stream. Timeouts/cancel-after-write kill the engine
  and return `OutcomeUnknownError`: the operation may have executed, so
  callers must not blindly retry side effects.

## 5. Builder execution model (`internal/builder/builder.go`)

- The binary is **trusted**: this package cannot sandbox a hostile binary,
  only limit accidents. Detection never fails; absence disables builder
  tools, never the core.
- Runs are argv-only (no shell), with a minimal env (`PATH`, `LANG`,
  `TMPDIR` plus `HOME` pointed at a per-call temp dir that is also the
  working directory, removed afterwards). Process group kill on timeout
  (`Setpgid`, Unix-only).
- Inputs must resolve inside roots via `ResolvePath`, be regular files,
  and stay under 512 MiB (`MaxInputBytes`); URL-looking paths refused.
  Outputs resolve via `ResolveDest` with parent creation. Outputs over
  1 GiB (`MaxOutputBytes`) are removed and reported.
- stderr is scrubbed (absolute paths become `[path]`, truncated to 200
  chars) before reaching MCP clients (`sanitizeStderr`).

## 6. Prompt-injection stance

Document content is data, never instructions: content flows into
structure-preserving edits (`internal/ooxml/edit.go`), validation
(`internal/validator/validator.go`) and analysis, never into command
construction — there is no shell, `eval`, or template expansion of
document text anywhere in the Go tree. Model-supplied operations are
gated by strict decoding (unknown fields rejected, values never echoed),
registry lookup, risk policy with approval/diff rules
(`internal/operations/*`), and revision checks before atomic write.
UNVERIFIED: no dedicated prompt-injection filter or output-encoding
audit exists; tool *descriptions* returned to the model are static
strings in `internal/server/tools.go`.

## 7. Logging and redaction

- Logged: action/target/outcome JSONL lines to `<storageDir>/audit.jsonl`
  (`0600`, `internal/audit/audit.go`); note entries include resolved
  absolute paths and backup names in `detail`. Startup readiness flags
  (counts only) via `slog`.
- Redacted: bridge token never appears in `/ping` or health output
  (asserted by `internal/server/health_test.go`); `DecodeOperation`
  errors never echo param values (asserted by
  `internal/operations/decode_test.go`); builder stderr paths scrubbed;
  engine parse errors truncate the offending line to 200 chars.

## 8. Verified control / known gap / phase

| Control | Status | Evidence |
|---|---|---|
| Strict operation decoding, no value echo | Verified | `internal/operations/decode.go`, `decode_test.go:TestDecodeNeverEchoesParamValues` |
| Path containment + symlink refusal | Verified | `internal/storage/storage.go`, `local_test.go`, `fs_handlers_test.go` |
| Zip-slip/symlink extraction blocks | Verified | `internal/server/fs_handlers.go:handleExtractZip` |
| OOXML reader caps (64 MiB/part, 2048 entries, 256 MiB total) | Verified (Phase G) | `internal/ooxml/edit.go`, `hostile_test.go` |
| Validator caps (32 MiB/part, 512 MiB total) | Verified (Phase G) | `internal/validator/validator.go`, `validator_test.go:TestValidateOversizedPart` |
| Fuzz seed corpora for parsers/bridge | Verified (Phase G) | `internal/ooxml/fuzz_test.go`, `internal/operations/fuzz_test.go`, `internal/storage/fuzz_test.go`, `internal/plugin/fuzz_test.go` |
| Bridge token + nonce pairing + origin gates | Verified | `internal/plugin/bridge.go`, `bridge_test.go`, `challenge_test.go` |
| TOCTOU / hardlink escapes | Known gap | No `openat2` or hardlink checks in tree |
| Engine inherits parent env | Known gap (by design) | `internal/engine/bridge.go:doStartLocked` |
| Prompt-injection filter | UNVERIFIED: none exists | No matches in tree |
