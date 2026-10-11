package server

import (
	"archive/zip"
	"bufio"
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

// Resource limits for filesystem handlers.
const (
	maxFileReadBytes = 10 << 20  // 10MB cap for single-file reads
	maxHeadTailLines = 1000      // clamp for head/tail line counts
	maxZipFiles      = 100       // max entries extracted from a zip
	maxZipTotalBytes = 200 << 20 // 200MB total uncompressed extraction budget
)

// statWithinLimit stats path and rejects missing dirs and files over maxFileReadBytes.
func statWithinLimit(path string) (fs.FileInfo, *mcp.CallToolResult) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, textError("stat file: " + err.Error())
	}
	if info.IsDir() {
		return nil, textError(fmt.Sprintf("path %q is a directory, not a file", path))
	}
	if info.Size() > maxFileReadBytes {
		return nil, textError(fmt.Sprintf("file %q is %d bytes, over the %d byte limit", path, info.Size(), maxFileReadBytes))
	}
	return info, nil
}

// isBinary reports whether sample looks like binary data (contains NUL).
func isBinary(sample []byte) bool {
	for _, b := range sample {
		if b == 0 {
			return true
		}
	}
	return false
}

// clampLines coerces n into [1, maxHeadTailLines].
func clampLines(n int) int {
	if n < 1 {
		return 1
	}
	if n > maxHeadTailLines {
		return maxHeadTailLines
	}
	return n
}

func toLines(n float64) int {
	if n == 0 {
		return 10
	}
	return clampLines(int(n))
}

// withinRoot reports whether target equals root or stays under it.
func withinRoot(target, root string) bool {
	if target == root {
		return true
	}
	return strings.HasPrefix(target, root+string(os.PathSeparator))
}

func (h *ToolHandlers) handleReadFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	if _, errRes := statWithinLimit(path); errRes != nil {
		return errRes, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	if isBinary(data) {
		return textError(fmt.Sprintf("file %q looks binary; use checksum_file for integrity or read it as bytes", path)), nil
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
	seen := make(map[string]string, len(sources))
	for _, src := range sources {
		base := filepath.Base(src)
		if prev, dup := seen[base]; dup {
			return textError(fmt.Sprintf("duplicate zip entry %q from %q and %q: rename one source file so basenames are unique", base, prev, src)), nil
		}
		seen[base] = src
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	f, err := os.Create(path)
	if err != nil {
		return textError("create zip: " + err.Error()), nil
	}
	w := zip.NewWriter(f)
	fail := func(format string, a ...any) (*mcp.CallToolResult, error) {
		wCloseErr := w.Close()
		fCloseErr := f.Close()
		_ = os.Remove(path)
		msg := fmt.Sprintf(format, a...)
		if wCloseErr != nil {
			msg += fmt.Sprintf(" (zip close: %v)", wCloseErr)
		}
		if fCloseErr != nil {
			msg += fmt.Sprintf(" (file close: %v)", fCloseErr)
		}
		return textError(msg), nil
	}
	var written int
	for _, src := range sources {
		data, err := os.ReadFile(src)
		if err != nil {
			return fail("read %s: %v", src, err)
		}
		zf, err := w.Create(filepath.Base(src))
		if err != nil {
			return fail("create entry %s: %v", filepath.Base(src), err)
		}
		if _, err := zf.Write(data); err != nil {
			return fail("write %s: %v", filepath.Base(src), err)
		}
		written++
	}
	if err := w.Close(); err != nil {
		f.Close()
		return textError(fmt.Sprintf("close zip: %v", err)), nil
	}
	if err := f.Close(); err != nil {
		return textError(fmt.Sprintf("close zip file: %v", err)), nil
	}
	return jsonResult(map[string]any{"path": path, "files": written, "sources": len(sources)}), nil
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
	absDest, err := filepath.Abs(filepath.Clean(dest))
	if err != nil {
		return textError("resolve dest: " + err.Error()), nil
	}
	cwd, _ := os.Getwd()
	absCwd, _ := filepath.Abs(cwd)
	allowed := withinRoot(absDest, absCwd)
	if h.store != nil {
		if absStore, err := filepath.Abs(h.store.BaseDir()); err == nil && withinRoot(absDest, absStore) {
			allowed = true
		}
	}
	if !allowed {
		return textError(fmt.Sprintf("dest %q escapes the sandbox: extract inside the storage directory or the project working directory %q", dest, absCwd)), nil
	}
	if err := os.MkdirAll(absDest, 0755); err != nil {
		return textError(fmt.Sprintf("mkdir %s: %v", absDest, err)), nil
	}
	var extracted []string
	var totalBytes int64
	var fileCount int
	for _, f := range r.File {
		if f.FileInfo().Mode()&fs.ModeSymlink != 0 {
			return textError(fmt.Sprintf("symlink entry blocked: %s", f.Name)), nil
		}
		target := filepath.Join(absDest, filepath.Clean("/"+f.Name))
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return textError(fmt.Sprintf("resolve %s: %v", f.Name, err)), nil
		}
		if !withinRoot(absTarget, absDest) {
			return textError(fmt.Sprintf("path traversal blocked: %s", f.Name)), nil
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(absTarget, 0755); err != nil {
				return textError(fmt.Sprintf("mkdir %s: %v", absTarget, err)), nil
			}
			continue
		}
		fileCount++
		if fileCount > maxZipFiles {
			return textError(fmt.Sprintf("zip holds more than %d files: refusing to extract", maxZipFiles)), nil
		}
		if err := os.MkdirAll(filepath.Dir(absTarget), 0755); err != nil {
			return textError(fmt.Sprintf("mkdir %s: %v", filepath.Dir(absTarget), err)), nil
		}
		rc, err := f.Open()
		if err != nil {
			return textError(fmt.Sprintf("open %s: %v", f.Name, err)), nil
		}
		out, err := os.OpenFile(absTarget, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			rc.Close()
			return textError(fmt.Sprintf("create %s: %v", absTarget, err)), nil
		}
		// Cap this entry so the running total never exceeds the budget.
		n, err := io.Copy(out, io.LimitReader(rc, maxZipTotalBytes-totalBytes+1))
		cerr := rc.Close()
		werr := out.Close()
		if err != nil {
			return textError(fmt.Sprintf("write %s: %v", absTarget, err)), nil
		}
		if cerr != nil {
			return textError(fmt.Sprintf("close %s: %v", f.Name, cerr)), nil
		}
		if werr != nil {
			return textError(fmt.Sprintf("close %s: %v", absTarget, werr)), nil
		}
		totalBytes += n
		if totalBytes > maxZipTotalBytes {
			return textError(fmt.Sprintf("zip extracts more than %d bytes total: refusing to extract", maxZipTotalBytes)), nil
		}
		extracted = append(extracted, absTarget)
	}
	return jsonResult(map[string]any{"dest": dest, "extracted": len(extracted), "files": extracted}), nil
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
	if _, errRes := statWithinLimit(path); errRes != nil {
		return errRes, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	if isBinary(data) {
		return textError(fmt.Sprintf("file %q looks binary; use checksum_file for integrity instead", path)), nil
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
		n = toLines(nv)
	}
	if path == "" {
		return textError("path is required"), nil
	}
	if _, errRes := statWithinLimit(path); errRes != nil {
		return errRes, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	defer f.Close()
	// Reject large binary files before scanning text lines.
	head := make([]byte, 512)
	m, _ := f.Read(head)
	if isBinary(head[:m]) {
		return textError(fmt.Sprintf("file %q looks binary; use checksum_file for integrity instead", path)), nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return textError("rewind file: " + err.Error()), nil
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) >= n {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return textError("scan file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "lines": lines}), nil
}

func (h *ToolHandlers) handleTailFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	n := 10
	if nv, ok := args["lines"].(float64); ok {
		n = toLines(nv)
	}
	if path == "" {
		return textError("path is required"), nil
	}
	if _, errRes := statWithinLimit(path); errRes != nil {
		return errRes, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	defer f.Close()
	head := make([]byte, 512)
	m, _ := f.Read(head)
	if isBinary(head[:m]) {
		return textError(fmt.Sprintf("file %q looks binary; use checksum_file for integrity instead", path)), nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return textError("rewind file: " + err.Error()), nil
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	ring := make([]string, 0, n)
	for sc.Scan() {
		if len(ring) < n {
			ring = append(ring, sc.Text())
		} else {
			copy(ring, ring[1:])
			ring[n-1] = sc.Text()
		}
	}
	if err := sc.Err(); err != nil {
		return textError("scan file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "lines": ring}), nil
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
	info, errRes := statWithinLimit(path)
	if errRes != nil {
		return errRes, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	defer f.Close()
	hsh := sha256.New()
	if _, err := io.Copy(hsh, io.LimitReader(f, maxFileReadBytes+1)); err != nil {
		return textError("hash file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "sha256": hex.EncodeToString(hsh.Sum(nil)), "size": info.Size()}), nil
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
