package server

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cam/mcp-onlyoffice/internal/agent"
	"github.com/cam/mcp-onlyoffice/internal/analyzer"
	"github.com/cam/mcp-onlyoffice/internal/engine"
	"github.com/cam/mcp-onlyoffice/internal/planner"
	"github.com/cam/mcp-onlyoffice/internal/plugin"
	"github.com/cam/mcp-onlyoffice/internal/storage"
	"github.com/cam/mcp-onlyoffice/internal/validator"
	"github.com/cam/mcp-onlyoffice/internal/workflow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ToolHandlers struct {
	store        *storage.LocalStore
	desktopPath  string
	bridge       *plugin.Bridge
	agent        *agent.DocumentAgent
	wf           *workflow.Engine
	analyzer     *analyzer.Analyzer
	validator    *validator.Validator
	pyBridge     *engine.Bridge
	planner      *planner.Planner
	planExec     *planner.ErrorRecovery
	opMemory     *planner.OperationalMemory
	improvement  *planner.Improvement
	cache        *SessionCache
	registry     *ToolRegistry
}

func NewToolHandlers(store *storage.LocalStore, desktopPath string, bridge *plugin.Bridge, pyBridge *engine.Bridge, cache *SessionCache, registry *ToolRegistry) *ToolHandlers {
	agt := agent.New(store)
	pl := planner.New()
	return &ToolHandlers{
		store:        store,
		desktopPath:  desktopPath,
		bridge:       bridge,
		pyBridge:     pyBridge,
		agent:        agt,
		wf:           workflow.NewEngine(store, agt),
		analyzer:     analyzer.New(store),
		validator:    validator.New(store),
		planner:      pl,
		planExec:     planner.NewErrorRecovery(pl.Memory()),
		opMemory:     pl.Memory(),
		improvement:  planner.NewImprovement(pl.Memory(), pl.Discovery()),
		cache:        cache,
		registry:     registry,
	}
}

func inputSchema(props map[string]any, required []string) any {
	s := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

type toolDef struct {
	Name        string
	Description string
	InputSchema any
	Handler     func(context.Context, map[string]any) (*mcp.CallToolResult, error)
}

func (h *ToolHandlers) RegisterPythonCapabilities(caps []engine.CapabilityInfo) {
	capDefs := make([]planner.Capability, len(caps))
	for i, c := range caps {
		capDefs[i] = planner.Capability{Name: c.Name, Description: c.Description}
	}
	h.planner.RegisterCapabilities(capDefs)
}

func (h *ToolHandlers) capabilitiesCount() int {
	return h.planner.Discovery().Count()
}

func (h *ToolHandlers) Definitions() []toolDef {
	return []toolDef{
		{Name: "discover_tools", Description: "Search tools by keyword. Returns top 5 matches.", InputSchema: inputSchema(map[string]any{"query": map[string]any{"type": "string", "desc": "Search keyword"}}, nil), Handler: h.handleDiscoverTools},
		{Name: "load_tool_tier", Description: "Load Tier 2 or Tier 3 tools after start.", InputSchema: inputSchema(map[string]any{"tier": map[string]any{"type": "number", "desc": "2 or 3"}}, []string{"tier"}), Handler: h.handleLoadTier},
		{Name: "create_document", Description: "Create new doc in storage.", InputSchema: inputSchema(map[string]any{"filename": map[string]any{"type": "string", "desc": "Filename w/ extension"}, "content": map[string]any{"type": "string", "desc": "Optional text"}}, []string{"filename"}), Handler: h.handleCreate},
		{Name: "write_document", Description: "Write docx/xlsx/pptx/txt to filesystem.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Full filesystem path"}, "content": map[string]any{"type": "string", "desc": "Text content (optional)"}}, []string{"path"}), Handler: h.handleWriteDocument},
		{Name: "create_word_document", Description: "Create .docx with optional title.", InputSchema: inputSchema(map[string]any{"title": map[string]any{"type": "string", "desc": "Document title"}}, nil), Handler: h.handleCreateWord},
		{Name: "create_spreadsheet", Description: "Create .xlsx with optional sheet name.", InputSchema: inputSchema(map[string]any{"title": map[string]any{"type": "string", "desc": "Sheet name"}}, nil), Handler: h.handleCreateSpreadsheet},
		{Name: "create_presentation", Description: "Create .pptx with optional title.", InputSchema: inputSchema(map[string]any{"title": map[string]any{"type": "string", "desc": "Presentation title"}}, nil), Handler: h.handleCreatePresentation},
		{Name: "list_documents", Description: "List stored documents.", InputSchema: inputSchema(map[string]any{}, nil), Handler: h.handleList},
		{Name: "get_document", Description: "Get doc by ID with content.", InputSchema: inputSchema(map[string]any{"id": map[string]any{"type": "string", "desc": "Document ID"}}, []string{"id"}), Handler: h.handleGet},
		{Name: "delete_document", Description: "Delete doc by ID.", InputSchema: inputSchema(map[string]any{"id": map[string]any{"type": "string", "desc": "Document ID"}}, []string{"id"}), Handler: h.handleDelete},
		{Name: "rename_document", Description: "Rename a stored document.", InputSchema: inputSchema(map[string]any{"id": map[string]any{"type": "string", "desc": "Document ID"}, "new_filename": map[string]any{"type": "string", "desc": "New name"}}, []string{"id", "new_filename"}), Handler: h.handleRename},
		{Name: "copy_document", Description: "Copy a stored document.", InputSchema: inputSchema(map[string]any{"id": map[string]any{"type": "string", "desc": "Document ID"}, "new_filename": map[string]any{"type": "string", "desc": "New filename"}}, []string{"id", "new_filename"}), Handler: h.handleCopy},
		{Name: "search_documents", Description: "Search docs by filename keyword.", InputSchema: inputSchema(map[string]any{"query": map[string]any{"type": "string", "desc": "Search term"}}, []string{"query"}), Handler: h.handleSearch},
		{Name: "open_document", Description: "Open doc in ONLYOFFICE Desktop.", InputSchema: inputSchema(map[string]any{"id": map[string]any{"type": "string", "desc": "Document ID"}, "mode": map[string]any{"type": "string", "desc": "edit/view", "enum": []string{"edit", "view"}}}, []string{"id"}), Handler: h.handleOpen},
		{Name: "get_selected_text", Description: "Get selected text in open doc.", InputSchema: inputSchema(map[string]any{}, nil), Handler: h.handleGetSelectedText},
		{Name: "insert_text", Description: "Insert text at cursor in open doc.", InputSchema: inputSchema(map[string]any{"text": map[string]any{"type": "string", "desc": "Text to insert"}}, []string{"text"}), Handler: h.handleInsertText},
		{Name: "replace_text", Description: "Replace text in open doc.", InputSchema: inputSchema(map[string]any{"old": map[string]any{"type": "string", "desc": "Text to find"}, "new": map[string]any{"type": "string", "desc": "Replacement"}}, []string{"old", "new"}), Handler: h.handleReplaceText},
		{Name: "create_table", Description: "Insert table at cursor in open doc.", InputSchema: inputSchema(map[string]any{"rows": map[string]any{"type": "number", "desc": "Row count (default 3)"}, "cols": map[string]any{"type": "number", "desc": "Column count (default 3)"}}, nil), Handler: h.handleCreateTable},
		{Name: "create_list", Description: "Insert bullet/numbered list in open doc.", InputSchema: inputSchema(map[string]any{"type": map[string]any{"type": "string", "desc": "bullet/numbered", "enum": []string{"bullet", "numbered"}}, "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "desc": "List items"}}, []string{"items"}), Handler: h.handleCreateList},
		{Name: "get_all_content", Description: "Get all content from open doc.", InputSchema: inputSchema(map[string]any{}, nil), Handler: h.handleGetAllContent},
		{Name: "write_spreadsheet", Description: "Create .xlsx with headers/rows.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Full path"}, "sheet_name": map[string]any{"type": "string", "desc": "Sheet name"}, "headers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "desc": "Column headers"}, "rows": map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "desc": "Row data 2D"}}, []string{"path"}), Handler: h.handleWriteSpreadsheet},
		{Name: "write_presentation", Description: "Create .pptx with slides, tables, KPIs, themes.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Full path"}, "theme": map[string]any{"type": "string", "desc": "dark/light/blue", "enum": []string{"dark", "light", "blue"}}, "slides": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"type": map[string]any{"type": "string", "desc": "title/text/table/kpi", "enum": []string{"title", "text", "table", "kpi"}}, "title": map[string]any{"type": "string", "desc": "Slide title"}, "subtitle": map[string]any{"type": "string", "desc": "Subtitle"}, "content": map[string]any{"type": "string", "desc": "Body text"}, "headers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "desc": "Col headers"}, "rows": map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "desc": "Table rows"}, "kpis": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string"}, "value": map[string]any{"type": "string"}}}, "desc": "KPI metrics"}, "background": map[string]any{"type": "string", "desc": "Bg hex color"}}}, "desc": "Slide definitions"}}, []string{"path", "slides"}), Handler: h.handleWritePresentation},
		{Name: "read_file", Description: "Read file from filesystem.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "File path"}}, []string{"path"}), Handler: h.handleReadFile},
		{Name: "export_document", Description: "Export stored doc to filesystem.", InputSchema: inputSchema(map[string]any{"id": map[string]any{"type": "string", "desc": "Document ID"}, "path": map[string]any{"type": "string", "desc": "Dest path"}}, []string{"id", "path"}), Handler: h.handleExportDocument},
		{Name: "import_document", Description: "Import file into doc storage.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "File path"}}, []string{"path"}), Handler: h.handleImportDocument},
		{Name: "write_text", Description: "Write plain text to file.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "File path"}, "content": map[string]any{"type": "string", "desc": "Text content"}}, []string{"path"}), Handler: h.handleWriteText},
		{Name: "append_text", Description: "Append text to file (creates if missing).", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "File path"}, "content": map[string]any{"type": "string", "desc": "Text to append"}}, []string{"path"}), Handler: h.handleAppendText},
		{Name: "write_docx", Description: "Create .docx with title+body.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Full path"}, "title": map[string]any{"type": "string", "desc": "Doc title"}, "body": map[string]any{"type": "string", "desc": "Body text"}}, []string{"path"}), Handler: h.handleWriteDocxFromText},
		{Name: "write_table_docx", Description: "Create .docx with formatted table.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Full path"}, "title": map[string]any{"type": "string", "desc": "Table title"}, "headers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "desc": "Col headers"}, "rows": map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "desc": "Row data 2D"}}, []string{"path"}), Handler: h.handleWriteTableDocx},
		{Name: "read_csv", Description: "Read CSV returns headers+rows.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "CSV file path"}}, []string{"path"}), Handler: h.handleReadCSV},
		{Name: "write_csv", Description: "Write CSV with headers+rows.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "File path"}, "headers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "desc": "Col headers"}, "rows": map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "desc": "Row data 2D"}}, []string{"path"}), Handler: h.handleWriteCSV},
		{Name: "list_directory", Description: "List files in a directory.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Dir path (default: cwd)"}}, nil), Handler: h.handleListDirectory},
		{Name: "create_directory", Description: "mkdir -p.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Dir path"}}, []string{"path"}), Handler: h.handleCreateDirectory},
		{Name: "copy_file", Description: "Copy file.", InputSchema: inputSchema(map[string]any{"source": map[string]any{"type": "string", "desc": "Source"}, "dest": map[string]any{"type": "string", "desc": "Dest"}}, []string{"source", "dest"}), Handler: h.handleCopyFile},
		{Name: "move_file", Description: "Move/rename file.", InputSchema: inputSchema(map[string]any{"source": map[string]any{"type": "string", "desc": "Source"}, "dest": map[string]any{"type": "string", "desc": "Dest"}}, []string{"source", "dest"}), Handler: h.handleMoveFile},
		{Name: "delete_file", Description: "Delete file or directory.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Path to delete"}}, []string{"path"}), Handler: h.handleDeleteFile},
		{Name: "file_info", Description: "Get file metadata (size, time, mode).", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "File path"}}, []string{"path"}), Handler: h.handleFileInfo},
		{Name: "find_files", Description: "Glob search files by pattern.", InputSchema: inputSchema(map[string]any{"root": map[string]any{"type": "string", "desc": "Root dir (default: cwd)"}, "pattern": map[string]any{"type": "string", "desc": "Glob pattern"}}, nil), Handler: h.handleFindFiles},
		{Name: "read_json", Description: "Read and parse JSON file.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "JSON file path"}}, []string{"path"}), Handler: h.handleReadJSON},
		{Name: "write_json", Description: "Write JSON object to file.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "File path"}, "data": map[string]any{"type": "object", "desc": "JSON data"}}, []string{"path", "data"}), Handler: h.handleWriteJSON},
		{Name: "create_zip", Description: "Create ZIP from file list.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Output ZIP path"}, "sources": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "desc": "Files to include"}}, []string{"path", "sources"}), Handler: h.handleCreateZip},
		{Name: "extract_zip", Description: "Extract ZIP to directory.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "ZIP path"}, "dest": map[string]any{"type": "string", "desc": "Dest dir"}}, []string{"path", "dest"}), Handler: h.handleExtractZip},
		{Name: "file_exists", Description: "Check if file/dir exists.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Path"}}, []string{"path"}), Handler: h.handleFileExists},
		{Name: "word_count", Description: "Count words/chars/lines in file.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "File path"}}, []string{"path"}), Handler: h.handleWordCount},
		{Name: "head_file", Description: "Read first N lines of file.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "File path"}, "lines": map[string]any{"type": "number", "desc": "Lines to read (default 10)"}}, []string{"path"}), Handler: h.handleHeadFile},
		{Name: "tail_file", Description: "Read last N lines of file.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "File path"}, "lines": map[string]any{"type": "number", "desc": "Lines to read (default 10)"}}, []string{"path"}), Handler: h.handleTailFile},
		{Name: "tree_directory", Description: "Recursive dir listing.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Root dir (default: cwd)"}}, nil), Handler: h.handleTreeDirectory},
		{Name: "csv_to_json", Description: "Convert CSV to JSON array.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "CSV path"}}, []string{"path"}), Handler: h.handleCSVtoJSON},
		{Name: "json_to_csv", Description: "Convert JSON array to CSV.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "JSON path"}}, []string{"path"}), Handler: h.handleJSONtoCSV},
		{Name: "touch_file", Description: "Create empty file (like touch).", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "File path"}}, []string{"path"}), Handler: h.handleTouchFile},
		{Name: "checksum_file", Description: "SHA-256 checksum of file.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "File path"}}, []string{"path"}), Handler: h.handleChecksumFile},
		{Name: "disk_usage", Description: "Disk usage: size/file count/dir count.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Path (default: cwd)"}}, nil), Handler: h.handleDiskUsage},
		{Name: "write_markdown", Description: "Write .md file.", InputSchema: inputSchema(map[string]any{"path": map[string]any{"type": "string", "desc": "Full path"}, "content": map[string]any{"type": "string", "desc": "Markdown content"}}, []string{"path"}), Handler: h.handleWriteMarkdown},
		{Name: "storage_info", Description: "Doc count and total size.", InputSchema: inputSchema(map[string]any{}, nil), Handler: h.handleStorageInfo},
		{Name: "document_agent", Description: "Create report from description with templates.", InputSchema: inputSchema(map[string]any{"task": map[string]any{"type": "string", "desc": "create_report"}, "title": map[string]any{"type": "string", "desc": "Document title"}, "template": map[string]any{"type": "string", "desc": "business_report/technical_doc/meeting_minutes/proposal"}, "template_params": map[string]any{"type": "object", "desc": "Template params"}, "sections": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"type": map[string]any{"type": "string", "enum": []string{"text", "table"}}, "heading": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}, "headers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "rows": map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}}}, "desc": "Sections"}}, []string{"task", "title"}), Handler: h.handleDocumentAgent},
		{Name: "run_workflow", Description: "Run workflow (create_project_package).", InputSchema: inputSchema(map[string]any{"workflow": map[string]any{"type": "string", "desc": "Workflow name"}, "params": map[string]any{"type": "object", "desc": "Params"}}, []string{"workflow"}), Handler: h.handleRunWorkflow},
		{Name: "list_workflows", Description: "List available workflows.", InputSchema: inputSchema(map[string]any{}, nil), Handler: h.handleListWorkflows},
		{Name: "list_templates", Description: "List document templates.", InputSchema: inputSchema(map[string]any{}, nil), Handler: h.handleListTemplates},
		{Name: "render_template", Description: "Render template with params.", InputSchema: inputSchema(map[string]any{"template": map[string]any{"type": "string", "desc": "Template name"}, "params": map[string]any{"type": "object", "desc": "Params"}}, []string{"template"}), Handler: h.handleRenderTemplate},
		{Name: "analyze_document", Description: "Analyze doc structure and stats.", InputSchema: inputSchema(map[string]any{"id": map[string]any{"type": "string", "desc": "Document ID"}}, []string{"id"}), Handler: h.handleAnalyzeDocument},
		{Name: "validate_document", Description: "Validate OOXML structure + metadata.", InputSchema: inputSchema(map[string]any{"id": map[string]any{"type": "string", "desc": "Document ID"}}, []string{"id"}), Handler: h.handleValidateDocument},
		{Name: "add_document_reference", Description: "Cross-reference doc A → doc B.", InputSchema: inputSchema(map[string]any{"id": map[string]any{"type": "string", "desc": "Source doc ID"}, "ref_id": map[string]any{"type": "string", "desc": "Target doc ID"}}, []string{"id", "ref_id"}), Handler: h.handleAddRef},
		{Name: "get_document_references", Description: "Get all refs for a document.", InputSchema: inputSchema(map[string]any{"id": map[string]any{"type": "string", "desc": "Document ID"}}, []string{"id"}), Handler: h.handleGetRefs},
		{Name: "python_engine", Description: "Run Python capability (charts, analytics, Excel, docs, PDF, images).", InputSchema: inputSchema(map[string]any{"capability": map[string]any{"type": "string", "desc": "Capability path"}, "params": map[string]any{"type": "object", "desc": "Capability params"}}, []string{"capability"}), Handler: h.handlePythonEngine},
		{Name: "planner", Description: "Plan+execute complex task via NL objective.", InputSchema: inputSchema(map[string]any{"objective": map[string]any{"type": "string", "desc": "Task description"}, "params": map[string]any{"type": "object", "desc": "Params"}}, []string{"objective"}), Handler: h.handlePlanner},
		{Name: "list_capabilities", Description: "List Python engine capabilities.", InputSchema: inputSchema(map[string]any{}, nil), Handler: h.handleListCapabilities},
		{Name: "create_report", Description: "High-level: create formatted business report (DOCX + XLSX + PPTX).", InputSchema: inputSchema(map[string]any{"title": map[string]any{"type": "string", "desc": "Report title"}, "author": map[string]any{"type": "string", "desc": "Author name"}, "date": map[string]any{"type": "string", "desc": "Date string"}, "sections": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"type": map[string]any{"type": "string", "enum": []string{"text", "table", "chart"}}, "heading": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}, "headers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "rows": map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}}}, "desc": "Report sections"}, "output_dir": map[string]any{"type": "string", "desc": "Output directory"}}, []string{"title", "output_dir"}), Handler: h.handleCreateReport},
		{Name: "operational_memory", Description: "Performance stats + task history.", InputSchema: inputSchema(map[string]any{}, nil), Handler: h.handleOperationalMemory},
		{Name: "discover", Description: "Find capabilities/modules by keyword.", InputSchema: inputSchema(map[string]any{"query": map[string]any{"type": "string", "desc": "Search keyword"}}, nil), Handler: h.handleDiscover},
		{Name: "self_improve", Description: "Analyze performance and suggest improvements.", InputSchema: inputSchema(map[string]any{}, nil), Handler: h.handleSelfImprove},
	}
}

func (h *ToolHandlers) openInEditor(id, mode string) error {
	if _, err := h.store.Get(id); err != nil {
		return err
	}
	dataPath := h.store.DataPath(id)
	var cmd *exec.Cmd
	switch mode {
	case "view":
		cmd = exec.Command(h.desktopPath, "--view="+dataPath)
	case "review":
		cmd = exec.Command(h.desktopPath, "--review="+dataPath)
	default:
		cmd = exec.Command(h.desktopPath, dataPath)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch editor: %w", err)
	}
	go cmd.Wait()
	return nil
}

func sanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, name)
	if len(name) > 100 {
		name = string([]rune(name)[:100])
	}
	return strings.TrimSpace(name)
}
