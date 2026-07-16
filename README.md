# MCP-OnlyOffice

**MCP server for ONLYOFFICE** — Create, edit, analyze, and manage office documents (DOCX, XLSX, PPTX, PDF) through natural language using any MCP-compatible AI assistant.

> Built for [OpenCode](https://opencode.ai), Claude, Cursor, and any MCP client.

---

## Features

- **42 MCP tools** organized in 3 progressive tiers
- **Document creation** — Word, spreadsheets, presentations from natural language descriptions
- **ONLYOFFICE Desktop integration** — Open, edit, insert text, create tables and lists in the editor via WebSocket plugin
- **Python engine** — Charts, analytics, Excel manipulation, PDF generation, image processing
- **Document storage** — UUID-based local storage with metadata indexing and cross-references
- **Smart planner** — Intent analysis → capability chain → execution with error recovery
- **Planner system** — Complex multi-step tasks via natural language objectives
- **Templates** — Business reports, technical docs, meeting minutes, proposals
- **Document analyzer** — Structure introspection for DOCX/XLSX/PPTX
- **OOXML validator** — Structure, metadata, and quality checks
- **OOXML native generator** — Create documents via raw XML+ZIP (Go, no dependencies)
- **Cross-document references** — Link related documents together
- **Self-improvement** — Performance tracking, failure analysis, and improvement suggestions

---

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                   AI Assistant (MCP Client)               │
│              OpenCode / Claude / Cursor                   │
└────────────────────────┬─────────────────────────────────┘
                         │ stdin/stdout (JSON-RPC over MCP)
                         ▼
┌──────────────────────────────────────────────────────────┐
│                  mcp-onlyoffice (Go binary)               │
│                                                          │
│  ┌─────────────────┐   ┌──────────────────────────┐     │
│  │  cmd/           │   │  internal/server/         │     │
│  │  main.go        │──▶│  - server.go (MCP)       │     │
│  │  (entry point)  │   │  - tools.go (42 tools)   │     │
│  └─────────────────┘   │  - *handlers.go          │     │
│                        │  - discovery.go (tiers)  │     │
│                        │  - cache.go              │     │
│                        └──────────┬──────────────-┘     │
│                                   │                     │
│  ┌────────────────────────────────┼──────────────────┐  │
│  │  internal/                      │                  │  │
│  │  ├── agent/     (document agent)│                 │  │
│  │  ├── planner/   (intent+plan)  │                 │  │
│  │  ├── workflow/  (workflows)    │                 │  │
│  │  ├── templates/ (templates)    │                 │  │
│  │  ├── analyzer/  (doc analysis) │                 │  │
│  │  ├── validator/ (doc validation│                 │  │
│  │  ├── ooxml/     (docx/xlsx/pptx│                 │  │
│  │  ├── storage/   (local storage)│                 │  │
│  │  ├── config/    (config loader)│                 │  │
│  │  └── desktoped/ (binary finder)│                 │  │
│  └────────────────────────────────┼──────────────────┘  │
│                                   │                     │
│  ┌──────────────────────────────┐ │                     │
│  │  internal/plugin/            │ │ WebSocket           │
│  │  bridge.go (client)          │◀┼────127.0.0.1:2021  │
│  │  index.go  (plugin files)   │ │                     │
│  └──────────────────────────────┘ │                     │
│                                   │                     │
│  ┌──────────────────────────────┐ │                     │
│  │  internal/engine/            │ │ stdin/stdout JSON   │
│  │  bridge.go (Go↔Python IPC)   │◀┼─────────────────┐  │
│  └──────────────────────────────┘ │                 │  │
│                                   │                 │  │
└───────────────────────────────────┼─────────────────┘  │
                                    │                     │
┌───────────────────────────────────┼─────────────────┐  │
│  python_engine/                   │ IPC              │  │
│  ├── runner.py   (IPC dispatcher) ◀──────────────────┘  │
│  ├── __init__.py (PythonEngine)  │                     │
│  ├── excel/       (spreadsheets) │                     │
│  ├── charts/      (matplotlib)   │                     │
│  ├── documents/   (python-docx)  │                     │
│  ├── presentations/ (pptx)       │                     │
│  ├── analytics/   (statistics)   │                     │
│  ├── pdf/         (fpdf2/PyPDF2) │                     │
│  └── images/      (Pillow)       │                     │
└──────────────────────────────────┘                     │
                                                         │
┌─────────────────────────────────────────────────────┐  │
│  ONLYOFFICE Desktop Editors (native app)             │  │
│  WebSocket plugin at ws://127.0.0.1:2021            │  │
└────────────────────────────────────────────────────-┘  │
                                                        │
┌─────────────────────────────────────────────────────┐  │
│  Document Storage (local filesystem)                 │  │
│  ~/.local/share/mcp-onlyoffice/documents/            │  │
│  └── UUID.data + .metadata.json                     │  │
└─────────────────────────────────────────────────────┘  │
```

### Data Flow

1. **User sends natural language request** → AI model interprets and calls an MCP tool
2. **MCP protocol over stdin/stdout** → Go server receives the JSON-RPC request
3. **Go handlers** route to the appropriate subsystem:
   - Filesystem operations → direct Go I/O
   - Document creation → `internal/ooxml/` (raw XML+ZIP) OR `python_engine/` (via Go↔Python IPC)
   - Document editing → `internal/plugin/` WebSocket bridge to ONLYOFFICE Desktop
   - Complex tasks → `internal/planner/` → intent analysis → capability chain → execution
   - Document analysis/validation → `internal/analyzer/` + `internal/validator/`
4. **Response** → JSON-RPC result back to AI assistant

---

## Installation

### Prerequisites

| Dependency | Version | Purpose |
|-----------|---------|---------|
| Go | 1.25+ | Build core binary |
| Python | 3.10+ | Document engine (charts, Excel, PDF) |
| ONLYOFFICE Desktop Editors | Latest | Live document editing (optional) |

### From Source

```bash
# Clone
git clone https://github.com/camilin7483/mcp-onlyoffice.git
cd mcp-onlyoffice

# Build
make build

# Install
make install
```

This will:
- Compile the Go binary
- Install it to `~/.local/bin/mcp-onlyoffice`
- Create config directory at `~/.config/mcp-onlyoffice/`
- Create storage directory at `~/mcp-onlyoffice/documents/`
- Copy default config if not present

### Python Dependencies

```bash
pip install -r python_engine/requirements.txt
```

Or install individually:
```bash
pip install openpyxl python-docx python-pptx matplotlib numpy fpdf2 PyPDF2 Pillow
```

### Configuration

Create `~/.config/mcp-onlyoffice/config.json`:

```json
{
  "plugin_port": 2021,
  "storage_path": "/home/user/mcp-onlyoffice/documents",
  "desktop_path": "/opt/onlyoffice/desktopeditors/DesktopEditors",
  "log_level": "info"
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `plugin_port` | `2021` | WebSocket port for ONLYOFFICE Desktop plugin |
| `storage_path` | `~/mcp-onlyoffice/documents` | Document storage directory |
| `desktop_path` | (auto-detect) | Path to DesktopEditors binary |
| `log_level` | `info` | Log level: debug/info/warn/error |

### OpenCode Configuration

Add to your `opencode.jsonc` or `~/.config/opencode/opencode.jsonc`:

```jsonc
{
  "mcpServers": {
    "onlyoffice": {
      "command": "/home/user/.local/bin/mcp-onlyoffice",
      "args": ["-config", "/home/user/.config/mcp-onlyoffice/config.json"],
      "env": {
        "MCP_ONLYOFFICE_PLUGIN_PORT": "2021",
        "MCP_ONLYOFFICE_STORAGE_PATH": "/home/user/mcp-onlyoffice/documents"
      }
    }
  }
}
```

---

## Usage

### Starting the Server

```bash
# From project directory
./mcp-onlyoffice

# Or with custom config
./mcp-onlyoffice -config /path/to/config.json
```

The server starts and listens on **stdin/stdout** (MCP stdio transport). Your AI assistant communicates with it automatically once configured.

### Quick Start Examples

Once connected, try these with your AI assistant:

| Request | What happens |
|---------|-------------|
| "Create a Word document with meeting minutes" | `create_word_document` → generated DOCX |
| "Make a spreadsheet with sales data" | `create_spreadsheet` → XLSX with headers+rows |
| "Create a presentation about Q3 results" | `write_presentation` → PPTX with slides+tables |
| "Open document abc-123 in ONLYOFFICE" | `open_document` → launches Desktop Editor |
| "Insert a table at the cursor" | `create_table` → table in open document |
| "Generate a bar chart from this data" | `python_engine` → matplotlib chart |
| "Create a business report" | `document_agent` → multi-document report |
| "Analyze document xyz-789" | `analyze_document` → structure breakdown |
| "What tools are available for charts?" | `discover_tools` → capability search |
| "Plan a complex task: create project proposal with budget table and timeline" | `planner` → multi-step execution |

---

## Tool Reference

### Tier 1 — Essential (loaded at startup)

| Tool | Description | Input |
|------|-------------|-------|
| `discover_tools` | Search tools by keyword | `query: string` |
| `load_tool_tier` | Load Tier 2 or Tier 3 | `tier: number (2\|3)` |
| `create_document` | Create doc in storage | `filename`, `content?` |
| `get_document` | Get doc by ID | `id` |
| `list_documents` | List all stored docs | — |
| `delete_document` | Delete doc by ID | `id` |
| `open_document` | Open in ONLYOFFICE Desktop | `id`, `mode? (edit\|view)` |
| `get_selected_text` | Get selected text in editor | — |
| `insert_text` | Insert at cursor | `text` |
| `replace_text` | Find and replace | `old`, `new` |
| `create_table` | Insert table | `rows?`, `cols?` |
| `create_list` | Insert list | `type (bullet\|numbered)`, `items[]` |
| `get_all_content` | Get full doc content | — |
| `write_document` | Write doc to filesystem | `path`, `content?` |
| `read_file` | Read file | `path` |
| `write_text` | Write text file | `path`, `content` |
| `append_text` | Append to file | `path`, `content` |
| `list_directory` | List directory | `path?` |
| `copy_file` | Copy file | `source`, `dest` |
| `move_file` | Move/rename | `source`, `dest` |
| `delete_file` | Delete file/dir | `path` |
| `file_info` | File metadata | `path` |
| `find_files` | Glob search | `pattern`, `root?` |
| `file_exists` | Check existence | `path` |
| `python_engine` | Run Python capability | `capability`, `params` |

### Tier 2 — Common

| Tool | Description |
|------|-------------|
| `create_word_document` | Create DOCX with title |
| `create_spreadsheet` | Create XLSX with sheet name |
| `create_presentation` | Create PPTX with title |
| `write_spreadsheet` | XLSX with headers+rows |
| `write_presentation` | PPTX with slides, tables, KPIs, themes |
| `write_docx` | DOCX with title+body |
| `write_table_docx` | DOCX with formatted table |
| `read_csv` | Parse CSV file |
| `write_csv` | Create CSV |
| `read_json` | Read and parse JSON |
| `write_json` | Write JSON object |
| `write_markdown` | Write MD file |
| `create_zip` | Archive files |
| `extract_zip` | Extract archive |
| `head_file` | First N lines |
| `tail_file` | Last N lines |
| `word_count` | Words/chars/lines |
| `checksum_file` | SHA-256 hash |
| `disk_usage` | Size/file/dir counts |
| `tree_directory` | Recursive listing |
| `csv_to_json` | Convert CSV to JSON |
| `json_to_csv` | Convert JSON to CSV |
| `touch_file` | Create empty file |
| `rename_document` | Rename stored doc |
| `copy_document` | Copy stored doc |
| `search_documents` | Search by filename |
| `export_document` | Export to filesystem |
| `import_document` | Import from filesystem |
| `storage_info` | Doc count + total size |
| `document_agent` | Create report with templates |
| `planner` | Plan+execute complex task |
| `list_capabilities` | List Python engine capabilities |

### Tier 3 — Advanced

| Tool | Description |
|------|-------------|
| `run_workflow` | Execute workflow |
| `list_workflows` | List available workflows |
| `list_templates` | List document templates |
| `render_template` | Render template with params |
| `analyze_document` | Analyze doc structure |
| `validate_document` | Validate OOXML structure |
| `add_document_reference` | Cross-reference documents |
| `get_document_references` | Get references |
| `create_report` | Create formatted business report (DOCX+XLSX+PPTX) |
| `operational_memory` | Performance stats + history |
| `discover` | Find capabilities by keyword |
| `self_improve` | Analyze + suggest improvements |

---

## Python Engine Capabilities

The Python engine (`python_engine/`) provides these capability paths for the `python_engine` tool:

| Capability Path | Module | Features |
|----------------|--------|----------|
| `excel.create_workbook` | xl | Create workbook with sheets |
| `excel.insert_data` | xl | Insert rows/columns |
| `excel.create_kpi_sheet` | xl | KPI dashboard sheet |
| `charts.create_bar_chart` | charts | Bar chart (matplotlib) |
| `charts.create_line_chart` | charts | Line chart |
| `charts.create_pie_chart` | charts | Pie chart |
| `charts.create_heatmap` | charts | Heatmap |
| `charts.create_financial_chart` | charts | Financial/stock chart |
| `documents.create_report` | docs | Report with python-docx |
| `presentations.create_deck` | pres | Slide deck with python-pptx |
| `analytics.descriptive_stats` | analytics | Mean, median, std dev |
| `analytics.frequency_analysis` | analytics | Frequency distribution |
| `analytics.aggregate_data` | analytics | Group and aggregate |
| `pdf.generate_pdf` | pdf | Generate PDF from text |
| `images.convert_image` | images | Format conversion |
| `images.resize_image` | images | Resize dimensions |

---

## Project Structure

```
mcp-onlyoffice/
├── cmd/
│   └── mcp-onlyoffice/
│       └── main.go              # Entry point
├── internal/
│   ├── agent/
│   │   └── agent.go             # Document agent orchestrator
│   ├── analyzer/
│   │   └── analyzer.go          # DOCX/XLSX/PPTX structure analyzer
│   ├── config/
│   │   └── config.go            # Configuration loader
│   ├── desktoped/
│   │   └── desktoped.go         # ONLYOFFICE Desktop binary finder
│   ├── engine/
│   │   └── bridge.go            # Go↔Python subprocess IPC bridge
│   ├── ooxml/
│   │   ├── ooxml.go             # OOXML shared helpers
│   │   ├── docx.go              # DOCX generator
│   │   ├── xlsx.go              # XLSX generator
│   │   ├── pptx.go              # PPTX generator
│   │   ├── csv.go               # CSV utilities
│   │   └── ooxml_test.go        # OOXML tests
│   ├── planner/
│   │   ├── planner.go           # Intent analysis + plan execution
│   │   ├── intent.go            # Intent classification (11 types)
│   │   ├── discovery.go         # Capability registry
│   │   ├── memory.go            # Operational memory + error recovery
│   │   ├── cache.go             # Plan cache (24h TTL)
│   │   ├── improvement.go       # Self-improvement suggestions
│   │   └── planner_test.go      # Planner tests
│   ├── plugin/
│   │   ├── bridge.go            # WebSocket client to ONLYOFFICE
│   │   └── index.go             # Plugin files installer
│   ├── server/
│   │   ├── server.go            # MCP server core
│   │   ├── tools.go              # 42 tool definitions
│   │   ├── doc_handlers.go      # Document creation handlers
│   │   ├── plugin_handlers.go   # Plugin interaction handlers
│   │   ├── engine_handlers.go   # Python engine + planner handlers
│   │   ├── fs_handlers.go       # Filesystem operation handlers
│   │   ├── storage_handlers.go  # Storage CRUD handlers
│   │   ├── agent_handlers.go    # Agent/workflow/template handlers
│   │   ├── discovery.go         # Tiered tool registry
│   │   ├── cache.go             # Session cache
│   │   └── result.go            # Response helpers
│   ├── storage/
│   │   ├── storage.go           # Storage interface
│   │   ├── local.go             # Local filesystem implementation
│   │   └── local_test.go        # Storage tests
│   ├── templates/
│   │   └── templates.go         # Document templates (4 types)
│   ├── validator/
│   │   └── validator.go         # OOXML structure validator
│   └── workflow/
│       └── workflow.go          # Workflow engine
├── python_engine/
│   ├── __init__.py              # PythonEngine class + dispatcher
│   ├── runner.py                # IPC stdin/stdout runner
│   ├── validator.py             # Result validation
│   ├── requirements.txt         # Python dependencies
│   ├── analytics/               # Statistics and analysis
│   ├── charts/                  # Chart generation (matplotlib)
│   ├── documents/               # Advanced document creation
│   ├── excel/                   # Spreadsheet operations
│   ├── images/                  # Image processing
│   ├── pdf/                     # PDF generation
│   └── presentations/           # Slide deck creation
├── documents/                   # Local document storage
├── config.example.json          # Example configuration
├── go.mod                       # Go module
├── go.sum                       # Go module checksums
├── Makefile                     # Build targets
├── install.sh                   # Installation script
├── uninstall.sh                 # Uninstallation script
├── update.sh                    # Update script
└── README.md                    # This file
```

---

## Development

### Building

```bash
make build        # Build binary
make install      # Build + install to ~/.local/bin
make clean        # Remove binary
make run          # Build + run
make dev          # go run (no build)
```

### Adding a New Tool

1. **Add handler** in `internal/server/` — create a `handleMyTool` method on `ToolHandlers`
2. **Register definition** in `internal/server/tools.go` — add a `toolDef` entry in `Definitions()`
3. **Add to tier** in `internal/server/discovery.go` — register in the appropriate tier (1/2/3)
4. **Restart server** — the tool is now available

Example minimal tool handler:

```go
func (h *ToolHandlers) handleMyTool(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
    name := args["name"].(string)
    result := fmt.Sprintf("Hello, %s!", name)
    return jsonResult(map[string]any{"message": result}), nil
}
```

### Adding Python Capabilities

1. Create a new directory `python_engine/your_module/`
2. Add a handler function that takes `params: dict` and returns a result dict
3. Register it in `python_engine/__init__.py` in the `CAPABILITY_MAP`

### Running Tests

```bash
go test ./...           # All Go tests
go test ./internal/ooxml/   # OOXML tests
go test ./internal/storage/ # Storage tests
go test ./internal/planner/ # Planner tests
```

---

## Roadmap

Based on the development plan in `system_prompt_mcp_onlyoffice_v2.md`:

| Phase | Status | Description |
|-------|--------|-------------|
| 1. Audit & Refactor | Pending | Code quality audit, fix ignored errors |
| 2. Architecture Design | Pending | Clean architecture boundaries |
| 3. Bug Fixes | Pending | Fix ZIP, storage, and dependency issues |
| 4. Document Agent | ✅ Done | `internal/agent/` |
| 5. Workflow Engine | ✅ Done | `internal/workflow/` |
| 6. Template System | ✅ Done | `internal/templates/` |
| 7. Document Analyzer | ✅ Done | `internal/analyzer/` |
| 8. Validator | ✅ Done | `internal/validator/` |
| 9. Cross-Doc References | ✅ Done | `add_document_reference` |
| 10. Observability | Pending | Metrics, logging, tracing |
| 11. Testing | Partial | Basic tests exist, needs expansion |
| 12. Performance Refactor | Pending | tools.go > 800 lines refactor |

---

## Known Issues

- **ZIP operations**: Some error returns are silently ignored in `create_zip`/`extract_zip`
- **Storage delete**: Error from `os.Remove` may be ignored in some paths
- **Dead dependency**: `golang.org/x/oauth2` is indirect but no longer needed
- **tools.go**: File exceeds 800 lines — needs refactoring into separate files
- **ONLYOFFICE plugin**: Requires Desktop Editors installed and running

## Troubleshooting

| Problem | Solution |
|---------|----------|
| "binary not found" | Run `make install` or build manually |
| "ONLYOFFICE Desktop not found" | Set `desktop_path` in config or install ONLYOFFICE |
| "Python engine not responding" | Ensure `pip install -r python_engine/requirements.txt` ran |
| "WebSocket connection failed" | Check ONLYOFFICE plugin is enabled and port 2021 is free |
| "Document storage corrupted" | Delete `documents/.metadata.json` (files remain) |

---

## Credits

**Author**: CamiloDev  
**Original creator**: Siddharth Ahuja (@ahujasid) — original Blender MCP architecture inspired this project's IPC approach  
**Tech**: Go 1.25 + Python 3 + MCP SDK + ONLYOFFICE  

## License

MIT — See [LICENSE](LICENSE) for details.
