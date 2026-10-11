# Changelog

All notable changes to this project will be documented in this file.

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
- E2E pack v2 (`/tmp/onlyoffice_e2e_v2`) — score 100.
