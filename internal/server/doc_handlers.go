package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/cam/mcp-onlyoffice/internal/ooxml"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *ToolHandlers) handleCreate(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	filename, _ := args["filename"].(string)
	if filename == "" {
		return textError("filename is required"), nil
	}
	var data []byte
	if c, ok := args["content"].(string); ok && c != "" {
		data = []byte(c)
	}
	doc, err := h.store.Create(filename, data)
	if err != nil {
		return textError("create failed: " + err.Error()), nil
	}
	return jsonResult(doc), nil
}

func (h *ToolHandlers) handleWriteDocument(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	content, _ := args["content"].(string)
	if content == "" {
		content = "Document"
	}

	ext := strings.ToLower(filepath.Ext(path))
	var data []byte
	var err error

	switch ext {
	case ".docx":
		data, err = ooxml.CreateDOCX(content)
	case ".xlsx":
		data, err = ooxml.CreateXLSX(content)
	case ".pptx":
		data, err = ooxml.CreatePPTX(content)
	case ".txt", "":
		data = []byte(content)
	default:
		data, err = ooxml.CreateDOCX(content)
	}

	if err != nil {
		return textError("create document: " + err.Error()), nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}

	return jsonResult(map[string]any{"path": path, "size": len(data), "format": ext}), nil
}

func (h *ToolHandlers) handleCreateWord(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	title, _ := args["title"].(string)
	if title == "" {
		title = "New Document"
	}
	data, err := ooxml.CreateDOCX(title)
	if err != nil {
		return textError("create docx: " + err.Error()), nil
	}
	filename := sanitizeFilename(title) + ".docx"
	doc, err := h.store.Create(filename, data)
	if err != nil {
		return textError("store failed: " + err.Error()), nil
	}
	go h.openInEditor(doc.ID, "edit")
	return jsonResult(doc), nil
}

func (h *ToolHandlers) handleCreateSpreadsheet(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	title, _ := args["title"].(string)
	if title == "" {
		title = "Sheet1"
	}
	data, err := ooxml.CreateXLSX(title)
	if err != nil {
		return textError("create xlsx: " + err.Error()), nil
	}
	filename := sanitizeFilename(title) + ".xlsx"
	doc, err := h.store.Create(filename, data)
	if err != nil {
		return textError("store failed: " + err.Error()), nil
	}
	go h.openInEditor(doc.ID, "edit")
	return jsonResult(doc), nil
}

func (h *ToolHandlers) handleCreatePresentation(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	title, _ := args["title"].(string)
	if title == "" {
		title = "New Presentation"
	}
	data, err := ooxml.CreatePPTX(title)
	if err != nil {
		return textError("create pptx: " + err.Error()), nil
	}
	filename := sanitizeFilename(title) + ".pptx"
	doc, err := h.store.Create(filename, data)
	if err != nil {
		return textError("store failed: " + err.Error()), nil
	}
	go h.openInEditor(doc.ID, "edit")
	return jsonResult(doc), nil
}

func (h *ToolHandlers) handleWriteDocxFromText(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	title, _ := args["title"].(string)
	content, _ := args["body"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	if title == "" {
		title = "Document"
	}
	data, err := ooxml.CreateDOCXFromText(title, content)
	if err != nil {
		return textError("create docx: " + err.Error()), nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(data)}), nil
}

func (h *ToolHandlers) handleWriteTableDocx(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	title, _ := args["title"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	if title == "" {
		title = "Table"
	}
	data, err := ooxml.CreateDOCXWithTable(title, ooxml.TableData{
		Headers: toStrSlice(args["headers"]),
		Rows:    toStrMatrix(args["rows"]),
	})
	if err != nil {
		return textError("create docx: " + err.Error()), nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(data)}), nil
}

func (h *ToolHandlers) handleWriteSpreadsheet(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	sheetName, _ := args["sheet_name"].(string)
	if sheetName == "" {
		sheetName = "Sheet1"
	}
	headers := toStrSlice(args["headers"])
	rows := toStrMatrix(args["rows"])
	data, err := ooxml.CreateXLSXFromData(sheetName, headers, rows)
	if err != nil {
		return textError("create spreadsheet: " + err.Error()), nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(data)}), nil
}

func (h *ToolHandlers) handleWritePresentation(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}

	slidesRaw, ok := args["slides"].([]any)
	if !ok || len(slidesRaw) == 0 {
		return textError("slides array is required"), nil
	}

	theme, _ := args["theme"].(string)
	if theme == "" {
		theme = "dark"
	}

	var slides []map[string]any
	for _, si := range slidesRaw {
		if sm, ok := si.(map[string]any); ok {
			slide := make(map[string]any)
			for k, v := range sm {
				slide[k] = v
			}
			if _, has := slide["type"]; !has {
				slide["type"] = "text"
			}
			slides = append(slides, slide)
		}
	}

	if len(slides) == 0 {
		return textError("no valid slides provided"), nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}

	if h.pyBridge != nil {
		resp, err := h.pyBridge.Execute(ctx, "presentations.create_from_slides", map[string]any{
			"path": path, "slides": slides, "theme": theme,
		})
		if err != nil {
			return textError("python engine error: " + err.Error()), nil
		}
		if resp.Status == "error" {
			return textError("presentation creation failed: " + resp.Error), nil
		}
		return jsonResult(resp.Data), nil
	}

	var slidesData []ooxml.SlideData
	for _, si := range slidesRaw {
		if sm, ok := si.(map[string]any); ok {
			title, _ := sm["title"].(string)
			content, _ := sm["content"].(string)
			slidesData = append(slidesData, ooxml.SlideData{Title: title, Content: content})
		}
	}
	data, err := ooxml.CreatePPTXFromSlides(slidesData)
	if err != nil {
		return textError("create presentation: " + err.Error()), nil
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(data)}), nil
}
