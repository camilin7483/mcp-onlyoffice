package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/cam/mcp-onlyoffice/internal/ooxml"
	"github.com/cam/mcp-onlyoffice/internal/plugin"
	"github.com/cam/mcp-onlyoffice/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// prepareWrite resolves a document output path against the allowed roots and
// applies the same overwrite/backup policy as the generic filesystem tools.
func (h *ToolHandlers) prepareWrite(path string, overwrite bool) (string, *mcp.CallToolResult) {
	resolved, err := storage.ResolveDest(path, h.effectiveRoots())
	if err != nil {
		return "", textError(err.Error())
	}
	if fi, statErr := os.Lstat(resolved); statErr == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", textError("refusing to write through symlink: " + path)
		}
		if !overwrite {
			return "", textError("file exists: " + path + " (pass overwrite=true)")
		}
		if !fi.Mode().IsRegular() {
			return "", textError("refusing to back up non-regular file: " + path)
		}
		bak := resolved + ".bak-" + storage.BackupTimestamp()
		if err := writeBackup(resolved, bak); err != nil {
			return "", textError("backup write: " + err.Error())
		}
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return "", textError("create directory: " + err.Error())
	}
	return resolved, nil
}

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

	resolved, bad := h.prepareWrite(path, args["overwrite"] == true)
	if bad != nil {
		return bad, nil
	}
	if err := os.WriteFile(resolved, data, 0o644); err != nil {
		return textError("write file: " + err.Error()), nil
	}

	return jsonResult(map[string]any{"path": resolved, "size": len(data), "format": ext}), nil
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
	resolved, bad := h.prepareWrite(path, args["overwrite"] == true)
	if bad != nil {
		return bad, nil
	}
	if err := os.WriteFile(resolved, data, 0o644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": resolved, "size": len(data)}), nil
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
	resolved, bad := h.prepareWrite(path, args["overwrite"] == true)
	if bad != nil {
		return bad, nil
	}
	if err := os.WriteFile(resolved, data, 0o644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": resolved, "size": len(data)}), nil
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
	resolved, bad := h.prepareWrite(path, args["overwrite"] == true)
	if bad != nil {
		return bad, nil
	}
	if err := os.WriteFile(resolved, data, 0o644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": resolved, "size": len(data)}), nil
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

	if bad := h.requirePythonEngine(); bad != nil {
		return bad, nil
	}
	resolved, bad := h.prepareWrite(path, args["overwrite"] == true)
	if bad != nil {
		return bad, nil
	}
	path = resolved

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

// replaceTextResult maps a replace_text bridge outcome to its MCP result:
// {"ok":true,"matches":N} on success, an explicit error otherwise. It never
// reports success without a real match count.
//
// NOTE: handleReplaceText (plugin_handlers.go, outside this change's file
// scope) must call it instead of discarding the SendCommand result:
//
//	result, err := h.bridge.SendCommand(ctx, "replace_text", map[string]any{"old": oldT, "new": newT}, 10*time.Second)
//	return replaceTextResult(result, err), nil
func replaceTextResult(result any, err error) *mcp.CallToolResult {
	if err != nil {
		return textError("plugin error: " + err.Error())
	}
	matches, mErr := plugin.ReplaceMatches(result)
	if mErr != nil {
		return textError("plugin error: " + mErr.Error())
	}
	return jsonResult(map[string]any{"ok": true, "matches": matches})
}
