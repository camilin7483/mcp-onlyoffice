# Backends: the four execution paths

Every document mutation or generation in this server runs on exactly one of
four backends. The backend set is declared in
`internal/operations/model.go` (`BackendDesktop`, `BackendOOXML`,
`BackendDocumentBuilder`, `BackendPython`).

## How each backend is selected

| Backend | Selected by | Detection | When absent |
|---|---|---|---|
| `desktop` | `document_operation` with `writer.*` (`internal/server/operations_handler.go:applyDesktopOperation`); the `open_document`, `get_selected_text`, `insert_text`, `replace_text`, `create_table`, `create_list`, `get_all_content`, `list_editor_sessions` tools (`internal/server/plugin_handlers.go`, `internal/server/tools.go`) | `internal/desktoped/desktoped.go` finds the Desktop Editors binary; `internal/plugin/bridge.go` holds the WebSocket session | `server.New` (`internal/server/server.go`) warns and serves without it; `open_document` and editor handlers return a plain error telling the user to open the document first |
| `ooxml` | `document_operation` with `ooxml.*` (`applyOOXMLOperation`); the `write_docx`, `write_table_docx`, `write_spreadsheet`, `write_presentation`, `create_word_document`, `create_spreadsheet`, `create_presentation` generators (`internal/ooxml/*.go`) | Always available (pure Go, no binary) | Never absent; a corrupt input fails the individual call, not startup |
| `document_builder` | `builder_render` with `mode: convert\|render` (`internal/server/builder_handlers.go`) | `builder.Detect` (`internal/builder/builder.go`): explicit `builder_path` wins, else `documentbuilder-mcp-shim` on `PATH` (the real Document Builder CLIs are never auto-detected by name) | `Available()` is false; `builder_render` returns "document builder unavailable" and every other tool keeps working |
| `python` | `python_engine` plus the report/chart paths built on it (`create_report`, planner capabilities via `RegisterPythonCapabilities`) | `server.New` looks for `python_engine/runner.py` next to the binary and calls `Bridge.Start` (`internal/engine/bridge.go`, `python_engine/runner.py`); zero capabilities means unavailable | Startup warns and continues; handlers gated by `requirePythonEngine` (`internal/server/health.go`) return an error naming the missing backend |

`server.New` treats storage as the only fatal dependency: a missing editor,
engine, or builder degrades its tools instead of aborting startup, and
`health` (`internal/server/health.go`) reports each backend as a boolean flag.

## Capability table

| Capability | Backend | Needs a live editor | Writes files |
|---|---|---|---|
| Read selection / full content | desktop | yes | no |
| Insert / replace text, tables, lists in the open document | desktop | yes | no (edits the open document) |
| In-place `ooxml.*` paragraph/cell edits | ooxml | no | yes (with `.bak-` backup) |
| Native DOCX/XLSX/PPTX generation | ooxml | no | yes |
| Convert / render via `builder_render` | document_builder | no | yes |
| Charts, Excel formulas, PDF, images, analytics | python | no | yes (inside the storage dir) |
| Plan, workflow, templates, agent | none (Go only) | no | via the backends above |

## Exact tool-to-backend mapping

- **desktop** (`internal/plugin/bridge.go` sessions): `open_document`,
  `get_selected_text`, `insert_text`, `replace_text`, `create_table`,
  `create_list`, `get_all_content`, `list_editor_sessions`, and
  `document_operation` with `writer.replace_text` / `writer.insert_text`.
- **ooxml** (`internal/ooxml/`): `write_docx`, `write_table_docx`,
  `write_spreadsheet`, `write_presentation`, `create_word_document`,
  `create_spreadsheet`, `create_presentation`, and `document_operation`
  with `ooxml.docx.replace_paragraph_text`,
  `ooxml.docx.insert_paragraph_after`,
  `ooxml.docx.set_paragraph_style`, `ooxml.xlsx.set_cell`.
  `analyze_document` / `validate_document` read through
  `internal/analyzer/` + `internal/validator/` (structural, not rendering).
- **document_builder** (`internal/builder/builder.go`): `builder_render` only.
- **python** (`python_engine/`, `internal/engine/bridge.go`):
  `python_engine`, `list_capabilities`, `create_report`,
  `document_agent` report sections, and planner-discovered capabilities.
- **No backend** (pure Go orchestration): `discover_tools`, `load_tool_tier`,
  `health`, filesystem and storage tools, `run_workflow`,
  `list_workflows`, `list_templates`, `render_template`, `planner`,
  `operational_memory`, `discover`, `self_improve`.

## Builder backend contract

`builder_render` speaks only to a shim binary, never to a real Document
Builder CLI directly:

- Convert: `<binary> <input> <output>`
- Render: `<binary> <input> <format> <output>` (`format` is restricted to
  `^[a-z][a-z0-9]{1,9}$` before spawning, so flag-shaped values are rejected)
- Existing outputs require `overwrite: true` and are backed up as `.bak-*`,
  like every other file writer (`prepareWrite`)

Configure it with `builder_path` (`MCP_ONLYOFFICE_BUILDER_PATH`); empty means
disabled. `builder.Detect` otherwise looks up only the
`documentbuilder-mcp-shim` name on `PATH`.

UNVERIFIED: whether any real Document Builder CLI accepts this positional
argv, which is why the real CLIs are not auto-detected by name; no builder
run was recorded here (see `docs/compatibility.md`).


### XLSX values and concurrent edits

`ooxml.xlsx.set_cell` clones the target cell's existing format when applying
`number_format`. Plain values stay text by default. When a number format is
requested, decimal and scientific notation values with no leading zero padding
are stored as numbers if representable within Excel’s 15 significant digits.
Other strings, such as `0012`, are preserved as text.

MCP instances sharing a storage directory coordinate filesystem mutations through
a lock. Typed OOXML changes also lock the target directory, reject Desktop
Editors' open-for-edit marker, and recheck the source bytes immediately before
replacing it. External tools must coordinate writes or edit a separate copy.
An unrelated process can still write without honoring either lock.
