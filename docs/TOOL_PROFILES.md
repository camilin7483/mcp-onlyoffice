# Tool profiles and contracts

`mcp-onlyoffice` exposes two tool profiles. Select with:

```bash
MCP_ONLYOFFICE_TOOLSET=legacy        # default in 0.5: all 70 tools
MCP_ONLYOFFICE_TOOLSET=professional  # curated 16 document tools
```

The profile changes only `tools/list`; handlers, storage and the plugin are
unchanged. Legacy clients keep working until 1.0.

## Professional profile (16 tools)

| Tool | Permission | Idempotent | Destructive | World |
|---|---|---|---|---|
| `create_word_document` | write | no | no | closed |
| `create_spreadsheet` | write | no | no | closed |
| `create_presentation` | write | no | no | closed |
| `write_docx` | write | no | no | closed |
| `write_spreadsheet` | write | no | no | closed |
| `write_presentation` | write | no | no | closed |
| `create_report` | write | no | no | closed |
| `list_documents` | read | yes | no | closed |
| `get_document` | read | yes | no | closed |
| `search_documents` | read | yes | no | closed |
| `analyze_document` | read | yes | no | closed |
| `validate_document` | read | yes | no | closed |
| `replace_text` | write | no | no | closed |
| `insert_text` | write | no | no | closed |
| `get_selected_text` | read | yes | no | closed |
| `open_document` | write | no | no | closed |

Filesystem, archive, Python engine, planner, agent, workflow and self-improvement
tools stay in the legacy profile and are considered administrative.

## Annotations

Every registered tool carries MCP annotations:

- `readOnlyHint`: read/list/get/search/analyze/validate/storage/capability tools.
- `destructiveHint`: `delete_*`, `extract_zip`.
- `idempotentHint`: reads plus copy/export/render/validate/analyze.
- `openWorldHint`: network, downloads, planner/agent/workflow/Python.

Clients must still enforce their own allowlists: annotations are hints, not
authorization.

## Compatibility and deprecation

- Tool names are not renamed in 0.5.
- A future canonical naming (`doc_*`, `fs_*`, `admin_*`) must ship as aliases for
  at least one minor release and be removed only in 1.0.
- Output schemas and structured error codes are contract work planned for 0.6;
  until then errors remain human-readable text with `isError=true`.

## Known gaps

- No `outputSchema` yet.
- No RBAC/multi-tenant layer.
- Plugin document commands still need a final interactive desktop run.
- `validate_document` is structural (ZIP/XML), not a renderer conformance test.
