# Architecture (as implemented)

## 1. Package graph (one line per package)

- `cmd/mcp-onlyoffice`: CLI entry; flags, config load, `server.New(...).Run()`.
- `internal/server`: MCP tool wiring over stdio, request handlers, readiness.
- `internal/config`: JSON config + env overrides, allowed roots, toolset profile.
- `internal/storage`: `LocalStore` documents + `ResolvePath`/`ResolveDest` sandbox and byte/entry caps.
- `internal/ooxml`: create/edit `.docx/.xlsx/.pptx` in memory (writers build, `edit.go` preserves).
- `internal/operations`: typed mutation model: `DecodeOperation`, registry, risk policy, transactions, receipts.
- `internal/validator`: read-only OOXML checks over the store; capped part reads.
- `internal/analyzer`: read-only document analysis (structure/issues per format).
- `internal/agent`: section-based document agent over store + `ooxml`.
- `internal/workflow`: builtin multi-step workflows over store + agent.
- `internal/planner`: intent/memory/discovery/improvement helpers for planning tools.
- `internal/templates`: corporate report sections shared by the Go agent path.
- `internal/plugin`: loopback WebSocket bridge to Desktop Editors (token, nonce pairing, sessions).
- `internal/engine`: subprocess bridge to `python_engine/runner.py` (NDJSON over stdio).
- `internal/builder`: optional Document Builder binary backend (argv-only, minimal env).
- `internal/desktoped`: Desktop Editors binary locator (`FindBinary`).
- `internal/audit`: append-only JSONL audit log (`0600`).
- `internal/version`: single source of truth for server/plugin/protocol versions.
- `python_engine/`: Python capabilities (`excel`, `documents`, `charts`, …) spoken to via `engine.Bridge`.

## 2. Request path for an MCP call

`server.Run` (stdio) → `addToolToServer` arg unmarshal →
`ToolHandlers.<handleX>` → `resolveRead`/`resolveDest` (roots) →
backend (`store`, `ooxml`, `analyzer`, `validator`, `pyBridge.Execute`,
`bridge.SendCommand`, `builder`) → `textResult`/`jsonResult` back to the
model. Errors are tool errors (`IsError`), never Go panics across the
boundary. Key files: `internal/server/server.go`, `tools.go`,
`fs_handlers.go`, `doc_handlers.go`, `engine_handlers.go`,
`plugin_handlers.go`, `operations_handler.go`.

## 3. Operation path for a mutation

`document_operation` args → `DecodeOperation` (strict, no value echo) →
`operationRegistry` lookup → `operationPolicy.Decide` (risk gate:
read-only/safe/sensitive/destructive) → `CheckRevision`
(expected vs `ooxml.Digest`) → backend apply (OOXML in-place edit or
desktop `SendCommand`) → `ValidateBeforeReturn`/`validateBytes` →
`Transaction` Commit/Rollback receipt with before/after digests.
Key files: `internal/server/operations_handler.go`,
`internal/operations/decode.go`, `registry.go`, `risk.go`,
`transaction.go`, `revision.go`.

## 4. Readiness model

`ToolHandlers.Health()` (`internal/server/health.go`) is infallible and
degrades per backend: `core_ready` always true; `python_available`,
`desktop_available`, `builder_available`, `document_server_available`
(angle: `bridge.IsConnected`), `storage_writable` (temp-file probe),
`tools_registered`, `python_capabilities`, `allowed_roots` (count only —
no paths or secrets). `server.New` logs one line per flag at startup and
serves with degraded backends instead of failing.

## 5. Where each phase's code lives

- Reliability foundation (this branch): capped readers and hostile
  fixtures in `internal/ooxml/edit.go` + `hostile_test.go` + `fuzz_test.go`;
  decoder/storage/bridge fuzz targets in their packages; validator caps
  in `internal/validator/validator.go`; this doc set in `docs/`.
- Operation safety: `internal/operations/*` + `internal/server/operations_handler.go`.
- Bridge pairing/auth: `internal/plugin/bridge.go` (+ `index.go` install).
- Python IPC hardening: `internal/engine/bridge.go` (caps, allowlist, kill-on-timeout).
- Builder containment: `internal/builder/builder.go`.
- Filesystem sandbox: `internal/storage/storage.go` + `internal/server/fs_handlers.go`.
