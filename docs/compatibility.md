# Compatibility: what runs where, and what was actually tested

## MCP revisions

- SDK: `github.com/modelcontextprotocol/go-sdk v1.8.0` (`go.mod`).
- The server negotiates MCP revision `2026-07-28` while still accepting
  older clients (see `CHANGELOG.md` 2.1.0; wire notes: `readOnlyHint` /
  `idempotentHint` always serialized, `-32601` included in stdio errors).

## Clients tested

- Only the go-sdk wiring in this repo was run: `Definitions()` contract
  tests (`internal/server/tool_contract_test.go` — pins 74 legacy tools),
  registration in `server.Run` (`internal/server/server.go`), and the stdio
  shutdown test (`internal/server/shutdown_test.go`), which boots the real
  server and closes stdin.
- No Claude / OpenCode / Cursor / desktop MCP client run is recorded in
  this repository. Any claim of working in a specific assistant is
  UNVERIFIED here.

## ONLYOFFICE versions

- On this machine: ONLYOFFICE **9.0.4.50** (recorded in
  `benchmark/results/bench-d77900e-20261119T211009Z.json`).
- 8.x was never tested here. Statements elsewhere suggesting 8.x
  verification refer to an earlier checkout state and are UNVERIFIED for
  this tree.

## Platforms

- **Linux: verified.** Full `go test ./...`, race detector, benchmark, and
  the live bridge pairing path run on Linux.
- **Windows / macOS: cross-compiled and `go vet` only.** CI builds all
  three OSes and runs the short test suite there
  (`.github/workflows/ci.yml`), but no interactive Desktop Editors session
  was run on them here. The plugin installer only knows the Linux plugins
  path (`~/.local/share/onlyoffice/desktopeditors/sdkjs-plugins/<GUID>`,
  `internal/plugin/index.go:PluginDir`); on any other OS it returns an
  error naming the `-plugin-dir` override instead of inventing a path, and
  `server.New` (`internal/server/server.go`) logs a warning and serves
  without the plugin.
- The builder backend's process-group kill uses Unix-only `Setpgid`
  (`internal/builder/builder.go`); a Windows port needs a build-tagged
  alternative.

## Document Builder

Optional backend (`internal/builder/builder.go`). `builder_version` was
null in the recorded benchmark run: no builder binary was present here, so
`builder_render` convert/render against a real binary is UNVERIFIED.

## UNVERIFIED list

- Live editor commands (`get_selected_text`, `replace_text`, full-content
  round-trips) in an interactive Desktop Editors session.
- Any specific MCP client app (Claude Desktop, OpenCode, Cursor, …).
- ONLYOFFICE 8.x on this tree.
- Windows / macOS interactive use (compile + vet only).
- Real Document Builder CLI against `builder_render` (argv convention is
  this repo's contract, not a verified server behavior).
- Real office open of generated files (validator is structural; see
  `docs/benchmark.md`).
- Network/multi-user deployment (bridge binds loopback only; pairing
  assumes a single-user machine — see `docs/threat-model.md`).
