package server

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *ToolHandlers) waitForPlugin() (bool, string) {
	if h.bridge.IsConnected() {
		return true, ""
	}
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		if h.bridge.IsConnected() {
			return true, ""
		}
	}
	return false, "plugin not connected. Use open_document first and make sure ONLYOFFICE Desktop Editors is running with the document open."
}

func (h *ToolHandlers) handleOpen(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	id, _ := args["id"].(string)
	mode, _ := args["mode"].(string)
	if id == "" {
		return textError("id is required"), nil
	}
	if mode == "" {
		mode = "edit"
	}
	if h.desktopPath == "" {
		return textError("Desktop Editors not found. Install it or configure the path."), nil
	}
	if err := h.openInEditor(id, mode); err != nil {
		return textError("open failed: " + err.Error()), nil
	}
	for i := 0; i < 30; i++ {
		if h.bridge.IsConnected() {
			return jsonResult(map[string]any{"opened": id, "mode": mode, "plugin": "connected"}), nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return jsonResult(map[string]any{"opened": id, "mode": mode, "plugin": "waiting"}), nil
}

func (h *ToolHandlers) handleGetSelectedText(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	ok, msg := h.waitForPlugin()
	if !ok {
		return textError(msg), nil
	}
	result, err := h.bridge.SendCommand(ctx, "get_selected_text", nil, 10*time.Second)
	if err != nil {
		return textError("plugin error: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"selected_text": result}), nil
}

func (h *ToolHandlers) handleInsertText(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	text, _ := args["text"].(string)
	if text == "" {
		return textError("text is required"), nil
	}
	ok, msg := h.waitForPlugin()
	if !ok {
		return textError(msg), nil
	}
	_, err := h.bridge.SendCommand(ctx, "insert_text", map[string]any{"text": text}, 10*time.Second)
	if err != nil {
		return textError("plugin error: " + err.Error()), nil
	}
	return okResult(), nil
}

func (h *ToolHandlers) handleReplaceText(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	oldT, _ := args["old"].(string)
	newT, _ := args["new"].(string)
	if oldT == "" {
		return textError("old text is required"), nil
	}
	ok, msg := h.waitForPlugin()
	if !ok {
		return textError(msg), nil
	}
	_, err := h.bridge.SendCommand(ctx, "replace_text", map[string]any{"old": oldT, "new": newT}, 10*time.Second)
	if err != nil {
		return textError("plugin error: " + err.Error()), nil
	}
	return okResult(), nil
}

func (h *ToolHandlers) handleCreateTable(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	rows := 3
	cols := 3
	if r, ok := args["rows"].(float64); ok {
		rows = int(r)
	}
	if c, ok := args["cols"].(float64); ok {
		cols = int(c)
	}
	ok, msg := h.waitForPlugin()
	if !ok {
		return textError(msg), nil
	}
	_, err := h.bridge.SendCommand(ctx, "create_table", map[string]any{"rows": rows, "cols": cols}, 10*time.Second)
	if err != nil {
		return textError("plugin error: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"table_created": map[string]any{"rows": rows, "cols": cols}}), nil
}

func (h *ToolHandlers) handleCreateList(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	listType, _ := args["type"].(string)
	if listType == "" {
		listType = "bullet"
	}
	items := toStrSlice(args["items"])
	if len(items) == 0 {
		return textError("items array is required"), nil
	}
	pOk, pMsg := h.waitForPlugin()
	if !pOk {
		return textError(pMsg), nil
	}
	_, err := h.bridge.SendCommand(ctx, "create_list", map[string]any{"type": listType, "items": items}, 10*time.Second)
	if err != nil {
		return textError("plugin error: " + err.Error()), nil
	}
	return okResult(), nil
}

func (h *ToolHandlers) handleGetAllContent(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	ok, msg := h.waitForPlugin()
	if !ok {
		return textError(msg), nil
	}
	result, err := h.bridge.SendCommand(ctx, "get_all_content", nil, 10*time.Second)
	if err != nil {
		return textError("plugin error: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"content": result}), nil
}
