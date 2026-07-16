package server

import (
	"context"
	"path/filepath"

	"github.com/cam/mcp-onlyoffice/internal/agent"
	"github.com/cam/mcp-onlyoffice/internal/templates"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *ToolHandlers) handleDocumentAgent(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	task, _ := args["task"].(string)
	if task == "" {
		return textError("task is required"), nil
	}
	title, _ := args["title"].(string)
	if title == "" {
		return textError("title is required"), nil
	}

	templateName, _ := args["template"].(string)
	if templateName != "" {
		tmplParams, _ := args["template_params"].(map[string]any)
		if tmplParams == nil {
			tmplParams = make(map[string]any)
		}
		tmplParams["title"] = title
		rendered, err := templates.Render(templateName, tmplParams)
		if err != nil {
			return textError("template error: " + err.Error()), nil
		}
		res, err := h.agent.Execute(ctx, rendered)
		if err != nil {
			return textError("agent error: " + err.Error()), nil
		}
		return jsonResult(res), nil
	}

	agentTask := &agent.AgentTask{
		Task:  task,
		Title: title,
	}

	if sectionsRaw, ok := args["sections"].([]any); ok {
		for _, sr := range sectionsRaw {
			if sm, ok := sr.(map[string]any); ok {
				s := agent.Section{}
				if t, ok := sm["type"].(string); ok {
					s.Type = agent.SectionType(t)
				}
				s.Heading, _ = sm["heading"].(string)
				s.Content, _ = sm["content"].(string)
				s.Headers = toStrSlice(sm["headers"])
				s.Rows = toStrMatrix(sm["rows"])
				agentTask.Sections = append(agentTask.Sections, s)
			}
		}
	}

	res, err := h.agent.Execute(ctx, agentTask)
	if err != nil {
		return textError("agent error: " + err.Error()), nil
	}
	return jsonResult(res), nil
}

func (h *ToolHandlers) handleRunWorkflow(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	name, _ := args["workflow"].(string)
	if name == "" {
		return textError("workflow is required"), nil
	}
	params, _ := args["params"].(map[string]any)
	if params == nil {
		params = make(map[string]any)
	}
	res, err := h.wf.Execute(ctx, name, params)
	if err != nil {
		return textError("workflow error: " + err.Error()), nil
	}
	return jsonResult(res), nil
}

func (h *ToolHandlers) handleListWorkflows(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	return jsonResult(h.wf.List()), nil
}

func (h *ToolHandlers) handleListTemplates(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	return jsonResult(templates.List()), nil
}

func (h *ToolHandlers) handleRenderTemplate(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	tmpl, _ := args["template"].(string)
	if tmpl == "" {
		return textError("template is required"), nil
	}
	params, _ := args["params"].(map[string]any)
	if params == nil {
		params = make(map[string]any)
	}
	rendered, err := templates.Render(tmpl, params)
	if err != nil {
		return textError("template error: " + err.Error()), nil
	}
	res, err := h.agent.Execute(ctx, rendered)
	if err != nil {
		return textError("agent error: " + err.Error()), nil
	}
	return jsonResult(res), nil
}

func (h *ToolHandlers) handleAnalyzeDocument(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return textError("id is required"), nil
	}
	analysis, err := h.analyzer.Analyze(id)
	if err != nil {
		return textError("analyze failed: " + err.Error()), nil
	}
	return jsonResult(analysis), nil
}

func (h *ToolHandlers) handleValidateDocument(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return textError("id is required"), nil
	}
	valResult, err := h.validator.Validate(id)
	if err != nil {
		return textError("validate failed: " + err.Error()), nil
	}
	return jsonResult(valResult), nil
}

func (h *ToolHandlers) handleDiscoverTools(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return textError("query is required"), nil
	}
	found := h.registry.Discover(query)
	if len(found) == 0 {
		return textResult(`{"tools":[],"message":"no matching tools found"}`), nil
	}
	names := make([]string, len(found))
	for i, t := range found {
		names[i] = t.Name + ": " + t.Description
	}
	return jsonResult(map[string]any{"tools": names}), nil
}

func (h *ToolHandlers) handleLoadTier(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	tierNum := 2
	if t, ok := args["tier"].(float64); ok {
		tierNum = int(t)
	}
	if tierNum < 2 || tierNum > 3 {
		return textError("tier must be 2 or 3"), nil
	}
	tools := h.registry.Tier(tierNum)
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name + ": " + t.Description
	}
	return jsonResult(map[string]any{"tools": names, "tier": tierNum}), nil
}

func (h *ToolHandlers) handleCreateReport(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	title, _ := args["title"].(string)
	if title == "" {
		return textError("title is required"), nil
	}
	outputDir, _ := args["output_dir"].(string)
	if outputDir == "" {
		return textError("output_dir is required"), nil
	}
	author, _ := args["author"].(string)
	if author == "" {
		author = "Author"
	}
	date, _ := args["date"].(string)
	if date == "" {
		date = "2026-01-01"
	}
	sectionsRaw, _ := args["sections"].([]any)

	safeDir := filepath.Join(outputDir, sanitizeFilename(title))
	docxPath := filepath.Join(safeDir, title+".docx")
	xlsxPath := filepath.Join(safeDir, title+".xlsx")
	pptxPath := filepath.Join(safeDir, title+".pptx")

	results := make(map[string]any)

	docxArgs := map[string]any{"path": docxPath, "title": title, "author": author, "date": date}
	if len(sectionsRaw) > 0 {
		var sections []map[string]any
		for _, s := range sectionsRaw {
			if sm, ok := s.(map[string]any); ok {
				sections = append(sections, sm)
			}
		}
		docxArgs["sections"] = sections
	}
	docxResp, err := h.pyBridge.Execute(ctx, "documents.create_report", docxArgs)
	if err != nil {
		results["docx"] = map[string]any{"error": err.Error()}
	} else {
		results["docx"] = map[string]any{"path": docxPath, "status": docxResp.Status}
	}

	_, xlsxErr := h.pyBridge.Execute(ctx, "excel.create_workbook", map[string]any{"path": xlsxPath, "title": title})
	if xlsxErr != nil {
		results["xlsx"] = map[string]any{"error": xlsxErr.Error()}
	} else {
		results["xlsx"] = map[string]any{"path": xlsxPath}
	}

	pptxSlides := []map[string]any{
		{"type": "title", "title": title, "subtitle": "Generated Report"},
		{"type": "text", "title": "Summary", "content": "Report: " + title + "\nAuthor: " + author + "\nDate: " + date},
	}
	for _, s := range sectionsRaw {
		if sm, ok := s.(map[string]any); ok {
			stype, _ := sm["type"].(string)
			heading, _ := sm["heading"].(string)
			if stype == "table" {
				pptxSlides = append(pptxSlides, map[string]any{
					"type": "table", "title": heading,
					"headers": toStrSlice(sm["headers"]),
					"rows":    toStrMatrix(sm["rows"]),
				})
			} else {
				pptxSlides = append(pptxSlides, map[string]any{
					"type": "text", "title": heading, "content": sm["content"],
				})
			}
		}
	}
	pptxResp, err := h.pyBridge.Execute(ctx, "presentations.create_from_slides", map[string]any{
		"path": pptxPath, "theme": h.cache.GetPrefs().Theme, "slides": pptxSlides,
	})
	if err != nil {
		results["pptx"] = map[string]any{"error": err.Error()}
	} else {
		results["pptx"] = map[string]any{"path": pptxPath, "status": pptxResp.Status}
	}

	h.cache.SetOp("create_report", title+" in "+safeDir)
	return jsonResult(map[string]any{"output": safeDir, "results": results}), nil
}
