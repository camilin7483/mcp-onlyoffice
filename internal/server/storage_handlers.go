package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/cam/mcp-onlyoffice/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *ToolHandlers) handleList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	docs, err := h.store.List()
	if err != nil {
		return textError("list failed: " + err.Error()), nil
	}
	return jsonResult(docs), nil
}

func (h *ToolHandlers) handleGet(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return textError("id is required"), nil
	}
	doc, err := h.store.Get(id)
	if err != nil {
		return textError("document not found: " + err.Error()), nil
	}
	data, err := h.store.GetData(id)
	if err != nil {
		return textError("read failed: " + err.Error()), nil
	}
	return jsonResult(map[string]any{
		"id": doc.ID, "filename": doc.Filename, "size": doc.Size,
		"mime": doc.MimeType, "created": doc.CreatedAt, "modified": doc.ModifiedAt,
		"content": string(data),
	}), nil
}

func (h *ToolHandlers) handleDelete(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return textError("id is required"), nil
	}
	if err := h.store.Delete(id); err != nil {
		return textError("delete failed: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"deleted": id}), nil
}

func (h *ToolHandlers) handleRename(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	id, _ := args["id"].(string)
	newName, _ := args["new_filename"].(string)
	if id == "" || newName == "" {
		return textError("id and new_filename are required"), nil
	}
	data, err := h.store.GetData(id)
	if err != nil {
		return textError("source not found: " + err.Error()), nil
	}
	if err := h.store.Delete(id); err != nil {
		return textError("delete original: " + err.Error()), nil
	}
	doc, err := h.store.Create(newName, data)
	if err != nil {
		return textError("create renamed: " + err.Error()), nil
	}
	return jsonResult(doc), nil
}

func (h *ToolHandlers) handleCopy(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	id, _ := args["id"].(string)
	newName, _ := args["new_filename"].(string)
	if id == "" || newName == "" {
		return textError("id and new_filename are required"), nil
	}
	data, err := h.store.GetData(id)
	if err != nil {
		return textError("source not found: " + err.Error()), nil
	}
	doc, err := h.store.Create(newName, data)
	if err != nil {
		return textError("copy failed: " + err.Error()), nil
	}
	return jsonResult(doc), nil
}

func (h *ToolHandlers) handleSearch(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return textError("query is required"), nil
	}
	docs, err := h.store.List()
	if err != nil {
		return textError("list failed: " + err.Error()), nil
	}
	q := strings.ToLower(query)
	var results []storage.Document
	for _, d := range docs {
		if strings.Contains(strings.ToLower(d.Filename), q) {
			results = append(results, d)
		}
	}
	if results == nil {
		results = []storage.Document{}
	}
	return jsonResult(results), nil
}

func (h *ToolHandlers) handleExportDocument(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	id, _ := args["id"].(string)
	path, _ := args["path"].(string)
	if id == "" || path == "" {
		return textError("id and path are required"), nil
	}
	doc, err := h.store.Get(id)
	if err != nil {
		return textError("document not found: " + err.Error()), nil
	}
	data, err := h.store.GetData(id)
	if err != nil {
		return textError("read data: " + err.Error()), nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(data), "filename": doc.Filename}), nil
}

func (h *ToolHandlers) handleImportDocument(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	filename := filepath.Base(path)
	doc, err := h.store.Create(filename, data)
	if err != nil {
		return textError("import failed: " + err.Error()), nil
	}
	return jsonResult(doc), nil
}

func (h *ToolHandlers) handleStorageInfo(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	docs, err := h.store.List()
	if err != nil {
		return textError("list failed: " + err.Error()), nil
	}
	totalSize := int64(0)
	for _, d := range docs {
		totalSize += d.Size
	}
	return jsonResult(map[string]any{"documents": len(docs), "total_size": totalSize}), nil
}

func (h *ToolHandlers) handleAddRef(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	id, _ := args["id"].(string)
	refID, _ := args["ref_id"].(string)
	if id == "" || refID == "" {
		return textError("id and ref_id are required"), nil
	}
	if err := h.store.AddRef(id, refID); err != nil {
		return textError("add ref: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"id": id, "ref_id": refID}), nil
}

func (h *ToolHandlers) handleGetRefs(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return textError("id is required"), nil
	}
	refs, err := h.store.GetRefs(id)
	if err != nil {
		return textError("get refs: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"id": id, "refs": refs}), nil
}
