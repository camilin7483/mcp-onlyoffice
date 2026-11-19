# Tool profiles and contracts

`mcp-onlyoffice` exposes two tool profiles. Select with:

```bash
export MCP_ONLYOFFICE_TOOLSET=professional
```

You can also set `"toolset": "professional"` in the server config. The default
`legacy` profile registers all 74 tools. The selected profile determines which
tools the MCP server lists and accepts calls for.

## Professional profile (20 tools)

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
| `list_editor_sessions` | read | yes | no | closed |
| `builder_render` | write | no | no | closed |
| `health` | read | yes | no | closed |
| `document_operation` | write | no | no | closed |

Filesystem, archive, Python engine, planner, agent, workflow and self-improvement
tools stay in the legacy profile and are considered administrative.

## Annotations

Every registered tool carries MCP annotations:

- `readOnlyHint`: read/list/get/search/analyze/validate/storage/capability tools.
- `destructiveHint`: `delete_*`, `extract_zip`.
- `idempotentHint`: reads plus copy/export/render/validate/analyze.
- `openWorldHint`: network, downloads, planner/agent/workflow/Python.

Annotations describe tools to the MCP client. Clients enforce their own
authorization rules.

## Compatibility and deprecation

- The legacy profile retains its existing tool names. The professional profile
  uses the same names for its 20 tools.
- Tool errors carry human-readable text with `isError=true`. Clients should
  check individual outputs in a report pack for per-file errors.

## Known gaps

- No `outputSchema` yet.
- No RBAC/multi-tenant layer.
- Plugin document commands still need a final interactive desktop run.
- `validate_document` is structural (ZIP/XML), not a renderer conformance test.
