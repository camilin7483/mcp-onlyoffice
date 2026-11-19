# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

## [2.1.0] — 2026-11-19

### Added

- A `professional` profile exposes 17 document-focused MCP tools. The default `legacy` profile retains all 71 registrations. Both profiles include MCP tool annotations.
- A `health` tool reports `core_ready`, `python_available`, `desktop_available`, `builder_available`, `document_server_available`, version, toolset and counts. No paths or secrets are included.
- Cross-platform CI (Linux, Windows, macOS) with race detector, gofmt gate, Python tests, `govulncheck`, secret scan, license check, SBOM and per-platform artifacts.
- `internal/version` is now the single source of truth for the product version; `-version` prints it.
- `docs/audit-baseline.md` records the verified state of the repository before these changes.

### Changed

- The core starts with only storage available. A missing editor, a missing Python engine, or an engine that fails to start now degrades the affected tools instead of aborting startup; handlers return a clear error instead of touching an unavailable backend.
- MCP SDK upgraded from v1.6.1 to v1.8.0, which supports MCP revision `2026-07-28` while still negotiating with older clients. `golang.org/x/sys` bumped to v0.44.0 (GO-2026-5024, Windows-only). Wire changes: `readOnlyHint`/`idempotentHint` are always serialized and `-32601` is included in stdio errors.
- Repository is gofmt-clean; the format gate is part of `make lint` and CI.

### Fixed

- `delete_file` and `move_file` refuse to operate on an allowed root, so `recursive=true` can no longer remove the root itself.
- Overwrite backups use one `O_EXCL` streaming writer: a symlink planted at the predictable `.bak-` name is no longer followed, and large files are no longer read fully into memory.
- `append_text` enforces the write-size cap and writes an audit entry like every other writer.
- The desktop plugin discovers and reconnects to the bridge independently of the editor's initialization hook. Authenticated pairing has been verified under Xvfb. Live selection and replacement still need interactive verification.
- Report packs sanitize output filenames, require explicit permission to overwrite existing files, and validate DOCX image widths.
- Fresh checkouts include the Go command entry point. The ignore rule now matches only the compiled binaries at the repository root.

### Documentation

- Updated the source build and OpenCode setup, documented report images, and added a chart generated and embedded in a sample DOCX by the Python engine.

## [0.4.2] — 2026-11-17

### Reports
- `documents.create_report` accepts `type: "image"` sections with `path`/`image`
  and optional `width_inches`; the image is embedded and the path is checked by
  the Python output-dir confinement. Added Python regression tests.

## [0.4.1] — 2026-11-17

### ONLYOFFICE plugin live pairing
- Fixed plugin manifest GUID/folder layout: Desktop Editors loads
  `sdkjs-plugins/<guid-without-asc.>/index.html`. The plugin now installs as
  `asc.{UUID}` config inside `{UUID}` folder.
- `/ping` and `/pair` send CORS headers; WebSocket accepts the privileged
  `onlyoffice://plugin` (and its bare `plugin` Origin) after token/origin checks.
- Plugin captures the `Asc.plugin` API at init so document commands keep working
  if the host removes the global afterwards.
- Verified live: Desktop Editors 8.x under Xvfb loads the plugin, pairs and opens
  the authenticated WebSocket on port 2021.

## [0.4.0] — 2026-11-17

### Security / Hardening
- Plugin bridge: per-launch token (`~/.config/mcp-onlyoffice/bridge-token`, 0600), origin
  allowlist (no website origins), `/pair` bootstrap and `/ping` discovery, single active
  client, 1 MiB read limit, request cap, honest send failures, random-port fallback logged.
- `replace_text`: real `SearchAndReplace` with match count; no full-document paste; Go handler
  propagates `{ok,matches}`.
- Filesystem: all fs/document writers resolve through allowed roots with symlink checks;
  overwrite requires `overwrite=true` and creates `.bak-YYYYMMDDHHMMSS`; recursive delete
  requires `recursive=true`; zip-slip/absolute/symlink entries rejected; 32 MiB read /
  64 MiB write / 1000-entry / 256 MiB caps; JSONL audit.
- Python engine: SIGKILL + lazy restart on timeout/cancel, echoed request ids, no automatic
  retry of unknown outcomes, output-dir confinement, 8 MiB request cap.
- Config: allowed roots derive from the final storage path (env/config no longer leave stale
  default roots enabled).

### Tests
- New bridge, filesystem, engine-bridge, plugin and python confinement suites; `go test ./...`
  and `python3 -m unittest test_confinement` green.

## [0.3.0] — UNRELEASED

### Security / Hardening
- ZIP handlers: partial extract on corrupt archives, ZipSlip guard, resource caps (10MB per-file read, 100 files max, 200MB total uncompressed budget).
- Storage: `MaxDocBytes` (10MB) cap on document payloads in Create/Update.
- Engine bridge: 120s timeout, capability allowlist enforcement, no traceback leaked to the client.

### OOXML (`internal/ooxml`)
- Native generator produces repair-free output: ordered ZIP entries, docProps, `sectPr`, `t=b` preservation, workbook dimension, presentation `clrMap`.

### Validator (`internal/validator`)
- Checks for ZIP integrity, XML well-formedness, `[Content_Types]`, relationships, and metadata; 0–100 quality score.

### Python engine (`python_engine`)
- Pinned dependencies with `==`: openpyxl 3.1.5, python-docx 1.2.0, python-pptx 1.0.2, matplotlib 3.11.0, numpy 2.5.2, Pillow 12.3.0.
- New Excel capabilities: formulas, conditional formatting, chart embed into workbooks.
- Charts: DPI 150 default with seaborn style.
- Presentations: 8-slide blue 16:9 theme with speaker notes.
- PDF: cover page, pagination, and title/author metadata.

### Planner (`internal/planner`)
- `expandReport` builds per-step params instead of copying one blob.
- Persistent plan cache with deep-copy on store/lookup.
- `ExecutePlan` retries each failed step once with an alternative capability.

### Workflow / Agent (`internal/workflow`, `internal/agent`)
- New workflow: `batch_create`.
- New agent task: `mail_merge`, returning all record IDs.

### Observability (`internal/server`)
- Consistent `jsonError` envelope, JSONL audit log (`mcp-onlyoffice-audit.jsonl`), `MCP_ONLYOFFICE_LOG_LEVEL` env support.

### Validation
- `go test ./internal/ooxml/ ./internal/validator/ ./internal/planner/ ./internal/workflow/ ./internal/storage/` — all pass.
- The Q3 sample pack passed the structural validator with a score of 100.
