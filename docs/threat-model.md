# Threat model (as implemented)

Scope: the code as committed. UNVERIFIED marks claims no code confirms.

## 1. Assets

- User documents and their bytes (`storage.LocalStore` dir,
  `internal/storage/local.go`; default `~/mcp-onlyoffice/documents`).
- Metadata sidecar `.metadata.json` (filenames, sizes, refs).
- Bridge token file `~/.config/mcp-onlyoffice/bridge-token` (`0600`).
- Audit log `<storageDir>/audit.jsonl` (`0600`).
- Host filesystem reachable through allowed roots (the storage dir by
  default; config `AllowedRoots` otherwise — the cwd fallback in
  `effectiveRoots` is unreachable in production).

## 2. Actors

- **A1 malicious document author**: crafts `.docx/.xlsx/.pptx` (or plain
  zips) to crash, exhaust memory, or escape the reader.
- **A2 malicious local process**: runs as the same user, plants symlinks,
  races path checks, probes loopback ports, reads the token file.
- **A3 malicious model output**: sends hostile tool args / operations to
  exfiltrate, overwrite, or inject instructions via tool results.
- **A4 compromised dependency**: malicious `runner.py` capabilities,
  trojaned Document Builder binary, or poisoned Go module.

## 3. Threats (STRIDE-ish)

| # | Threat | Mitigation (code) | Residual risk |
|---|---|---|---|
| T1 | A1 zip bomb / decompression DoS | Per-part 64 MiB + 256 MiB total + 2048-entry caps in `internal/ooxml/edit.go`; 32 MiB/part + 512 MiB total in `internal/validator/validator.go`; 256 MiB total + 1000 entries on extract (`fs_handlers.go`) | A 64 MiB transient buffer per hostile part is accepted by design |
| T2 | A1 zip-slip / absolute entry escape | In-memory OOXML reader never writes entry names to disk (`hostile_test.go:TestHostileZipSlipNames`); `handleExtractZip` refuses abs/`..`/symlink entries and re-resolves parents | None known for extraction; writer paths unchanged |
| T3 | A1 entity-expansion / DTD attack | `encoding/xml` rejects custom entities; `wellFormed` fails and edits abort (`hostile_test.go:TestHostileDTDEntity`) | External DTD fetch never attempted (no resolver configured) |
| T4 | A1 malformed `.rels` / truncation crash | Parsers return errors, never panic; fuzzed (`internal/ooxml/fuzz_test.go`, `hostile_test.go`) | Fuzzing is seed+10 s runs, not continuous |
| T5 | A2 symlink / traversal escape | `ResolvePath`/`ResolveDest` + extraction re-resolution; covered by `local_test.go`, `fs_handlers_test.go` | **TOCTOU + hardlinks**: no `openat2`, no hardlink checks (see `docs/security.md`) |
| T6 | A2 token theft / rogue pairing | Token `0600`, loopback-only pairing, single-use IP-bound nonces, `Origin: null` and browser-page origin gates (`internal/plugin/bridge.go`) | Same-host, any-local-UID process can reach loopback pairing: HTTP cannot tell local users apart, so this is the accepted boundary, not just same-user (unix-socket peer credentials are future work); a same-user process can also read the token file directly |
| T7 | A2 port hijack before bridge binds | Bridge scans 50 ports from base then random fallback with a warning; plugin only discovers the scanned range | Fallback port is undiscoverable by design; hijack of the chosen port pre-bind is UNVERIFIED (no pre-bind check) |
| T8 | A3 hostile operation args | `DecodeOperation` allowlist (name pattern, non-zero target, param key pattern, 64 KiB cap), registry + risk policy (`internal/operations/*`) | Approved-sensitive mutations rely on the operator reading diffs |
| T9 | A3 exfiltration via tool results | Health/ping expose counts only; errors never echo params; builder stderr scrubbed | Audit log contains absolute paths; tool results legitimately contain document bytes the model asked to read |
| T10 | A3 prompt injection via document text | Content stays data (no shell/eval/template of document bytes); no dedicated filter — UNVERIFIED as a control | Malicious text can still reach the model as tool output and influence it |
| T11 | A4 malicious engine capability | Capability name allowlist `module.method`; argv-only exec; response id correlation; kill-on-timeout with `OutcomeUnknownError` | Full parent env inherited; a malicious `runner.py` runs with user privileges |
| T12 | A4 trojaned builder binary | Optional backend, argv-only, minimal env, throwaway workdir, path + size containment | Explicitly trusted binary; compromise equals user compromise |

## 4. Out of scope

- OS-level attacker (root, kernel, container escape).
- Editor binary compromise (ONLYOFFICE Desktop Editors itself); the
  plugin only sends closed-switch method names and validates results
  (`ReplaceMatches` rejects result shapes that are not `{"matches": N}`).
- Network deployment: the bridge binds loopback only and pairing assumes
  a single-user machine; multi-user or remote exposure is not defended.
- Side channels (timing, cache) and audit-log tampering by a same-user
  attacker.
