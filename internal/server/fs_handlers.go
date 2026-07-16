package server

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/ooxml"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *ToolHandlers) handleReadFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	info, _ := os.Stat(path)
	return jsonResult(map[string]any{
		"path": path, "size": len(data), "content": string(data),
		"modified": info.ModTime().Format(time.RFC3339),
	}), nil
}

func (h *ToolHandlers) handleWriteText(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(content)}), nil
}

func (h *ToolHandlers) handleAppendText(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return textError("open file: " + err.Error()), nil
	}
	defer f.Close()
	n, err := f.WriteString(content)
	if err != nil {
		return textError("append: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "appended": n}), nil
}

func (h *ToolHandlers) handleReadCSV(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	headers, rows, err := ooxml.ParseCSV(string(data))
	if err != nil {
		return textError("parse csv: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"headers": headers, "rows": rows}), nil
}

func (h *ToolHandlers) handleWriteCSV(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	headers := toStrSlice(args["headers"])
	rows := toStrMatrix(args["rows"])
	content, err := ooxml.FormatCSV(headers, rows)
	if err != nil {
		return textError("format csv: " + err.Error()), nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(content)}), nil
}

func (h *ToolHandlers) handleListDirectory(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return textError("list directory: " + err.Error()), nil
	}
	var files []map[string]any
	for _, e := range entries {
		info, _ := e.Info()
		files = append(files, map[string]any{
			"name": e.Name(), "dir": e.IsDir(),
			"size": info.Size(), "modified": info.ModTime().Format(time.RFC3339),
		})
	}
	return jsonResult(files), nil
}

func (h *ToolHandlers) handleCreateDirectory(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"created": path}), nil
}

func (h *ToolHandlers) handleCopyFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	src, _ := args["source"].(string)
	dst, _ := args["dest"].(string)
	if src == "" || dst == "" {
		return textError("source and dest are required"), nil
	}
	srcData, err := os.ReadFile(src)
	if err != nil {
		return textError("read source: " + err.Error()), nil
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	if err := os.WriteFile(dst, srcData, 0644); err != nil {
		return textError("write dest: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"copied": map[string]any{"from": src, "to": dst, "size": len(srcData)}}), nil
}

func (h *ToolHandlers) handleMoveFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	src, _ := args["source"].(string)
	dst, _ := args["dest"].(string)
	if src == "" || dst == "" {
		return textError("source and dest are required"), nil
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	if err := os.Rename(src, dst); err != nil {
		return textError("move failed: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"moved": map[string]any{"from": src, "to": dst}}), nil
}

func (h *ToolHandlers) handleDeleteFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	if err := os.RemoveAll(path); err != nil {
		return textError("delete failed: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"deleted": path}), nil
}

func (h *ToolHandlers) handleFileInfo(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return textError("stat failed: " + err.Error()), nil
	}
	return jsonResult(map[string]any{
		"name": info.Name(), "size": info.Size(), "dir": info.IsDir(),
		"modified": info.ModTime().Format(time.RFC3339),
		"mode":     info.Mode().String(),
	}), nil
}

func (h *ToolHandlers) handleFindFiles(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	root, _ := args["root"].(string)
	pattern, _ := args["pattern"].(string)
	if root == "" {
		root = "."
	}
	if pattern == "" {
		pattern = "*"
	}
	var matches []string
	filepath.Walk(root, func(p string, info fs.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		matched, _ := filepath.Match(pattern, info.Name())
		if matched {
			matches = append(matches, p)
		}
		return nil
	})
	return jsonResult(matches), nil
}

func (h *ToolHandlers) handleReadJSON(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return textError("invalid json: " + err.Error()), nil
	}
	result, _ := json.Marshal(parsed)
	return textResult(string(result)), nil
}

func (h *ToolHandlers) handleWriteJSON(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	data, ok := args["data"].(map[string]any)
	if !ok {
		return textError("data object is required"), nil
	}
	pretty, _ := json.MarshalIndent(data, "", "  ")
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	if err := os.WriteFile(path, pretty, 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(pretty)}), nil
}

func (h *ToolHandlers) handleCreateZip(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	sourcesRaw, ok := args["sources"].([]any)
	if !ok || len(sourcesRaw) == 0 {
		return textError("path and sources array are required"), nil
	}
	sources := toStrSlice(sourcesRaw)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	f, err := os.Create(path)
	if err != nil {
		return textError("create zip: " + err.Error()), nil
	}
	defer f.Close()
	w := zip.NewWriter(f)
	var written int
	var zipErrors []string
	for _, src := range sources {
		data, err := os.ReadFile(src)
		if err != nil {
			zipErrors = append(zipErrors, fmt.Sprintf("read %s: %v", src, err))
			continue
		}
		zf, err := w.Create(filepath.Base(src))
		if err != nil {
			zipErrors = append(zipErrors, fmt.Sprintf("create entry %s: %v", filepath.Base(src), err))
			continue
		}
		if _, err := zf.Write(data); err != nil {
			zipErrors = append(zipErrors, fmt.Sprintf("write %s: %v", filepath.Base(src), err))
			continue
		}
		written++
	}
	if err := w.Close(); err != nil {
		zipErrors = append(zipErrors, fmt.Sprintf("close zip: %v", err))
	}
	result := map[string]any{"path": path, "files": written, "sources": len(sources)}
	if len(zipErrors) > 0 {
		result["errors"] = zipErrors
	}
	return jsonResult(result), nil
}

func (h *ToolHandlers) handleExtractZip(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	dest, _ := args["dest"].(string)
	if path == "" || dest == "" {
		return textError("path and dest are required"), nil
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		return textError("open zip: " + err.Error()), nil
	}
	defer r.Close()
	var extracted []string
	var extractErrors []string
	for _, f := range r.File {
		target := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			extractErrors = append(extractErrors, fmt.Sprintf("path traversal blocked: %s", f.Name))
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				extractErrors = append(extractErrors, fmt.Sprintf("mkdir %s: %v", target, err))
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			extractErrors = append(extractErrors, fmt.Sprintf("mkdir %s: %v", filepath.Dir(target), err))
			continue
		}
		rc, err := f.Open()
		if err != nil {
			extractErrors = append(extractErrors, fmt.Sprintf("open %s: %v", f.Name, err))
			continue
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			extractErrors = append(extractErrors, fmt.Sprintf("read %s: %v", f.Name, err))
			continue
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			extractErrors = append(extractErrors, fmt.Sprintf("write %s: %v", target, err))
			continue
		}
		extracted = append(extracted, target)
	}
	result := map[string]any{"dest": dest, "extracted": len(extracted), "files": extracted}
	if len(extractErrors) > 0 {
		result["errors"] = extractErrors
	}
	return jsonResult(result), nil
}

func (h *ToolHandlers) handleFileExists(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	_, err := os.Stat(path)
	return jsonResult(map[string]any{"path": path, "exists": err == nil}), nil
}

func (h *ToolHandlers) handleWordCount(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	text := string(data)
	return jsonResult(map[string]any{
		"path": path, "words": len(strings.Fields(text)),
		"characters": len(text), "lines": strings.Count(text, "\n") + 1,
	}), nil
}

func (h *ToolHandlers) handleHeadFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	n := 10
	if nv, ok := args["lines"].(float64); ok {
		n = int(nv)
	}
	if path == "" {
		return textError("path is required"), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	lines := strings.SplitN(string(data), "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return jsonResult(map[string]any{"path": path, "lines": lines}), nil
}

func (h *ToolHandlers) handleTailFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	n := 10
	if nv, ok := args["lines"].(float64); ok {
		n = int(nv)
	}
	if path == "" {
		return textError("path is required"), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	allLines := strings.Split(string(data), "\n")
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}
	start := len(allLines) - n
	if start < 0 {
		start = 0
	}
	return jsonResult(map[string]any{"path": path, "lines": allLines[start:]}), nil
}

func (h *ToolHandlers) handleTreeDirectory(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	root, _ := args["path"].(string)
	if root == "" {
		root = "."
	}
	var tree []string
	filepath.Walk(root, func(p string, info fs.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		tree = append(tree, rel)
		return nil
	})
	return jsonResult(tree), nil
}

func (h *ToolHandlers) handleCSVtoJSON(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	headers, rows, err := ooxml.ParseCSV(string(data))
	if err != nil {
		return textError("parse csv: " + err.Error()), nil
	}
	var result []map[string]string
	for _, row := range rows {
		entry := make(map[string]string)
		for i, h := range headers {
			if i < len(row) {
				entry[h] = row[i]
			}
		}
		result = append(result, entry)
	}
	return jsonResult(result), nil
}

func (h *ToolHandlers) handleJSONtoCSV(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		return textError("invalid json array: " + err.Error()), nil
	}
	if len(records) == 0 {
		return textResult("[]"), nil
	}
	headers := make([]string, 0, len(records[0]))
	for k := range records[0] {
		headers = append(headers, k)
	}
	sort.Strings(headers)
	var rows [][]string
	for _, rec := range records {
		var row []string
		for _, h := range headers {
			row = append(row, fmt.Sprint(rec[h]))
		}
		rows = append(rows, row)
	}
	csvContent, err := ooxml.FormatCSV(headers, rows)
	if err != nil {
		return textError("format csv: " + err.Error()), nil
	}
	csvPath := path + ".csv"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		return textError("write csv: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": csvPath, "size": len(csvContent)}), nil
}

func (h *ToolHandlers) handleTouchFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	f, err := os.OpenFile(path, os.O_CREATE, 0644)
	if err != nil {
		return textError("touch: " + err.Error()), nil
	}
	f.Close()
	return jsonResult(map[string]any{"created": path}), nil
}

func (h *ToolHandlers) handleChecksumFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	hsh := sha256.Sum256(data)
	return jsonResult(map[string]any{"path": path, "sha256": hex.EncodeToString(hsh[:]), "size": len(data)}), nil
}

func (h *ToolHandlers) handleDiskUsage(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}
	var totalSize int64
	var fileCount, dirCount int
	filepath.Walk(path, func(p string, info fs.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			dirCount++
		} else {
			fileCount++
			totalSize += info.Size()
		}
		return nil
	})
	return jsonResult(map[string]any{
		"path": path, "files": fileCount, "directories": dirCount, "total_size": totalSize,
	}), nil
}

func (h *ToolHandlers) handleWriteMarkdown(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	if !strings.HasSuffix(strings.ToLower(path), ".md") {
		path += ".md"
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(content)}), nil
}
