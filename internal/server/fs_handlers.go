package server

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	auditlog "github.com/cam/mcp-onlyoffice/internal/audit"
	"github.com/cam/mcp-onlyoffice/internal/ooxml"
	"github.com/cam/mcp-onlyoffice/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Filesystem hardening knobs. Package-level (instead of ToolHandlers fields)
// so no struct change is needed; tests override them as hooks.
var (
	fsRoots    []string
	fsAudit    *auditlog.Logger
	fsMaxWrite = storage.MaxWriteBytes
	fsMaxRead  = storage.MaxReadBytes
	fsMaxWalk  = storage.MaxWalkEntries
	fsMaxZip   = storage.MaxZipEntries
	fsMaxZipB  = storage.MaxZipUncompressed
	fsMaxSrcs  = storage.MaxCreateZipSources
)

// maxHeadTailLines clamps head/tail line counts.
const maxHeadTailLines = 1000

// SetAuditLogger wires the JSONL audit logger (called from server.New).
func (h *ToolHandlers) SetAuditLogger(l *auditlog.Logger) {
	fsAudit = l
}

// SetAllowedRoots overrides the allowed filesystem roots.
func (h *ToolHandlers) SetAllowedRoots(roots ...string) {
	fsRoots = append([]string(nil), roots...)
}

// effectiveRoots returns the configured roots, falling back to the document
// storage dir plus the current working directory.
func (h *ToolHandlers) effectiveRoots() []string {
	if len(fsRoots) > 0 {
		return fsRoots
	}
	var roots []string
	if h.store != nil {
		roots = append(roots, h.store.BaseDir())
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		roots = append(roots, cwd)
	}
	return roots
}

// resolveRead resolves p for reading (a leaf symlink is followed, then the
// target must still sit inside an allowed root).
func (h *ToolHandlers) resolveRead(p string) (string, *mcp.CallToolResult) {
	rp, err := storage.ResolvePath(p, h.effectiveRoots())
	if err != nil {
		return "", textError(err.Error())
	}
	return rp, nil
}

// resolveDest resolves p for creation/write/delete (a leaf symlink is never
// followed; the parent must sit inside an allowed root).
func (h *ToolHandlers) resolveDest(p string) (string, *mcp.CallToolResult) {
	rp, err := storage.ResolveDest(p, h.effectiveRoots())
	if err != nil {
		return "", textError(err.Error())
	}
	return rp, nil
}

// auditLog records an audit entry when a logger is wired (nil-safe).
func auditLog(action, target, outcome string, detail map[string]any) {
	if fsAudit == nil {
		return
	}
	fsAudit.Audit(action, target, outcome, detail)
}

// statWithinLimit stats path with Lstat and rejects dirs and files over fsMaxRead.
func statWithinLimit(path string) (fs.FileInfo, *mcp.CallToolResult) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, textError("stat file: " + err.Error())
	}
	if info.IsDir() {
		return nil, textError(fmt.Sprintf("path %q is a directory, not a file", path))
	}
	if info.Size() > fsMaxRead {
		return nil, textError(fmt.Sprintf("file %q is %d bytes, over the %d byte limit", path, info.Size(), fsMaxRead))
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

// isWithin reports whether target equals root or stays under it.
func isWithin(target, root string) bool {
	if target == root {
		return true
	}
	return strings.HasPrefix(target, root+string(os.PathSeparator))
}

// overwriteOK reports whether args carry an explicit overwrite: true.
func overwriteOK(args map[string]any) bool {
	v, ok := args["overwrite"]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1" || t == "yes"
	case float64:
		return t != 0
	}
	return false
}

// recursiveOK reports whether args carry an explicit recursive: true.
func recursiveOK(args map[string]any) bool {
	v, ok := args["recursive"]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1" || t == "yes"
	case float64:
		return t != 0
	}
	return false
}

// writeBackup streams src into dst, refusing when dst already exists
// (including a symlink planted at that name). The file is created with
// O_EXCL so the existence check has no follow-or-clobber race, memory stays
// bounded via io.Copy, and a symlink at dst is never followed.
func writeBackup(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("backup exists: %s", dst)
	} else if !os.IsNotExist(err) {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(dst)
		if copyErr != nil {
			return copyErr
		}
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
	return nil
}

// refuseRoot rejects an already-resolved path that is exactly one of the
// configured allowed roots, so delete/move can never remove a root itself.
func (h *ToolHandlers) refuseRoot(resolved, verb string) *mcp.CallToolResult {
	if storage.IsAllowedRoot(resolved, h.effectiveRoots()) {
		return textError(fmt.Sprintf("refusing to %s an allowed root: %s", verb, resolved))
	}
	return nil
}

// ensureWritable refuses to clobber an existing file unless overwrite=true;
// on overwrite it backs the previous file up to <name>.bak-YYYYMMDDHHMMSS
// and audits. Returns the backup path ("" when nothing was backed up).
func ensureWritable(resolved, action string, args map[string]any) (string, *mcp.CallToolResult) {
	fi, err := os.Lstat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", textError("stat file: " + err.Error())
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", textError(fmt.Sprintf("refusing to follow symlink: %s", resolved))
	}
	if fi.IsDir() {
		return "", textError(fmt.Sprintf("destination exists: %s (refusing to overwrite a directory)", resolved))
	}
	if !fi.Mode().IsRegular() {
		return "", textError(fmt.Sprintf("refusing to back up non-regular file: %s", resolved))
	}
	if !overwriteOK(args) {
		return "", textError(fmt.Sprintf("file exists: %s (pass overwrite=true)", resolved))
	}
	bak := resolved + ".bak-" + storage.BackupTimestamp()
	if err := writeBackup(resolved, bak); err != nil {
		return "", textError("backup write: " + err.Error())
	}
	auditLog(action, resolved, "ok", map[string]any{"backup": bak})
	return bak, nil
}

// checkWriteSize rejects payloads over the write limit.
func checkWriteSize(n int) *mcp.CallToolResult {
	if int64(n) > fsMaxWrite {
		return textError(fmt.Sprintf("content is %d bytes, over the %d byte limit", n, fsMaxWrite))
	}
	return nil
}

// mkdirParent creates the parent dir of an already-resolved path.
func mkdirParent(resolved string) *mcp.CallToolResult {
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return textError("create directory: " + err.Error())
	}
	return nil
}

var errWalkLimit = errors.New("walk entry limit exceeded")

// walkCapped walks root (already resolved) without following symlinks,
// skipping symlinked entries. Returns the skipped count.
func walkCapped(root string, fn func(path string, d fs.DirEntry) error) (int, error) {
	var count, skipped int
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p == root {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			skipped++
			return nil
		}
		count++
		if count > fsMaxWalk {
			return errWalkLimit
		}
		return fn(p, d)
	})
	if errors.Is(err, errWalkLimit) {
		return skipped, fmt.Errorf("directory walk exceeds %d entries: refusing to list", fsMaxWalk)
	}
	return skipped, err
}

func (h *ToolHandlers) handleReadFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	resolved, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	if _, errRes := statWithinLimit(resolved); errRes != nil {
		return errRes, nil
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	if isBinary(data) {
		return textError(fmt.Sprintf("file %q looks binary; use checksum_file for integrity or read it as bytes", path)), nil
	}
	info, _ := os.Lstat(resolved)
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
	resolved, errRes := h.resolveDest(path)
	if errRes != nil {
		return errRes, nil
	}
	if errRes := checkWriteSize(len(content)); errRes != nil {
		return errRes, nil
	}
	bak, errRes := ensureWritable(resolved, "write_text", args)
	if errRes != nil {
		return errRes, nil
	}
	if errRes := mkdirParent(resolved); errRes != nil {
		return errRes, nil
	}
	if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(content), "backup": bak}), nil
}

func (h *ToolHandlers) handleAppendText(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	resolved, errRes := h.resolveDest(path)
	if errRes != nil {
		return errRes, nil
	}
	var existing int64
	if fi, err := os.Lstat(resolved); err == nil {
		// Existing file: an explicit overwrite flag is required; no backup.
		if fi.IsDir() {
			return textError(fmt.Sprintf("path %q is a directory, not a file", path)), nil
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return textError(fmt.Sprintf("refusing to follow symlink: %s", resolved)), nil
		}
		if !overwriteOK(args) {
			return textError(fmt.Sprintf("file exists: %s (pass overwrite=true)", resolved)), nil
		}
		existing = fi.Size()
	} else if !os.IsNotExist(err) {
		return textError("stat file: " + err.Error()), nil
	}
	if errRes := checkWriteSize(int(existing) + len(content)); errRes != nil {
		return errRes, nil
	}
	if errRes := mkdirParent(resolved); errRes != nil {
		return errRes, nil
	}
	f, err := os.OpenFile(resolved, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return textError("open file: " + err.Error()), nil
	}
	defer f.Close()
	n, err := f.WriteString(content)
	if err != nil {
		return textError("append: " + err.Error()), nil
	}
	auditLog("append_text", resolved, "ok", map[string]any{"appended": n})
	return jsonResult(map[string]any{"path": path, "appended": n}), nil
}

func (h *ToolHandlers) handleReadCSV(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	resolved, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	if _, errRes := statWithinLimit(resolved); errRes != nil {
		return errRes, nil
	}
	data, err := os.ReadFile(resolved)
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
	resolved, errRes := h.resolveDest(path)
	if errRes != nil {
		return errRes, nil
	}
	headers := toStrSlice(args["headers"])
	rows := toStrMatrix(args["rows"])
	content, err := ooxml.FormatCSV(headers, rows)
	if err != nil {
		return textError("format csv: " + err.Error()), nil
	}
	if errRes := checkWriteSize(len(content)); errRes != nil {
		return errRes, nil
	}
	bak, errRes := ensureWritable(resolved, "write_csv", args)
	if errRes != nil {
		return errRes, nil
	}
	if errRes := mkdirParent(resolved); errRes != nil {
		return errRes, nil
	}
	if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(content), "backup": bak}), nil
}

func (h *ToolHandlers) handleListDirectory(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}
	resolved, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return textError("list directory: " + err.Error()), nil
	}
	var files []map[string]any
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink != 0 {
			continue
		}
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
	resolved, errRes := h.resolveDest(path)
	if errRes != nil {
		return errRes, nil
	}
	if fi, err := os.Lstat(resolved); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return textError(fmt.Sprintf("refusing to follow symlink: %s", path)), nil
		}
		if !fi.IsDir() {
			return textError(fmt.Sprintf("path exists and is not a directory: %s", path)), nil
		}
		return jsonResult(map[string]any{"created": path}), nil
	}
	if err := os.MkdirAll(resolved, 0755); err != nil {
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
	resolvedSrc, errRes := h.resolveRead(src)
	if errRes != nil {
		return errRes, nil
	}
	if _, errRes := statWithinLimit(resolvedSrc); errRes != nil {
		return errRes, nil
	}
	resolvedDst, errRes := h.resolveDest(dst)
	if errRes != nil {
		return errRes, nil
	}
	srcData, err := os.ReadFile(resolvedSrc)
	if err != nil {
		return textError("read source: " + err.Error()), nil
	}
	bak, errRes := ensureWritable(resolvedDst, "copy_file", args)
	if errRes != nil {
		return errRes, nil
	}
	if errRes := mkdirParent(resolvedDst); errRes != nil {
		return errRes, nil
	}
	if err := os.WriteFile(resolvedDst, srcData, 0644); err != nil {
		return textError("write dest: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"copied": map[string]any{"from": src, "to": dst, "size": len(srcData)}, "backup": bak}), nil
}

func (h *ToolHandlers) handleMoveFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	src, _ := args["source"].(string)
	dst, _ := args["dest"].(string)
	if src == "" || dst == "" {
		return textError("source and dest are required"), nil
	}
	resolvedSrc, errRes := h.resolveDest(src)
	if errRes != nil {
		return errRes, nil
	}
	if errRes := h.refuseRoot(resolvedSrc, "move"); errRes != nil {
		return errRes, nil
	}
	if _, err := os.Lstat(resolvedSrc); err != nil {
		return textError("stat source: " + err.Error()), nil
	}
	resolvedDst, errRes := h.resolveDest(dst)
	if errRes != nil {
		return errRes, nil
	}
	if errRes := h.refuseRoot(resolvedDst, "move"); errRes != nil {
		return errRes, nil
	}
	bak, errRes := ensureWritable(resolvedDst, "move_file", args)
	if errRes != nil {
		return errRes, nil
	}
	if errRes := mkdirParent(resolvedDst); errRes != nil {
		return errRes, nil
	}
	if err := os.Rename(resolvedSrc, resolvedDst); err != nil {
		return textError("move failed: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"moved": map[string]any{"from": src, "to": dst}, "backup": bak}), nil
}

func (h *ToolHandlers) handleDeleteFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	resolved, errRes := h.resolveDest(path)
	if errRes != nil {
		return errRes, nil
	}
	if errRes := h.refuseRoot(resolved, "delete"); errRes != nil {
		return errRes, nil
	}
	fi, err := os.Lstat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			auditLog("delete_file", resolved, "ok", map[string]any{"note": "already absent"})
			return jsonResult(map[string]any{"deleted": path}), nil
		}
		return textError("stat failed: " + err.Error()), nil
	}
	if fi.IsDir() && !recursiveOK(args) {
		return textError(fmt.Sprintf("refusing to delete directory without recursive=true: %s", path)), nil
	}
	auditLog("delete_file", resolved, "attempt", map[string]any{"recursive": recursiveOK(args)})
	if err := os.RemoveAll(resolved); err != nil {
		auditLog("delete_file", resolved, "error", map[string]any{"error": err.Error()})
		return textError("delete failed: " + err.Error()), nil
	}
	auditLog("delete_file", resolved, "ok", map[string]any{"recursive": recursiveOK(args)})
	return jsonResult(map[string]any{"deleted": path}), nil
}

func (h *ToolHandlers) handleFileInfo(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	resolved, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	info, err := os.Lstat(resolved)
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
	resolved, errRes := h.resolveRead(root)
	if errRes != nil {
		return errRes, nil
	}
	var matches []string
	skipped, err := walkCapped(resolved, func(p string, d fs.DirEntry) error {
		matched, _ := filepath.Match(pattern, d.Name())
		if matched {
			matches = append(matches, p)
		}
		return nil
	})
	if err != nil {
		return textError(err.Error()), nil
	}
	if skipped > 0 {
		auditLog("find_files", resolved, "ok", map[string]any{"skipped_symlinks": skipped})
	}
	return jsonResult(matches), nil
}

func (h *ToolHandlers) handleReadJSON(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	resolved, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	if _, errRes := statWithinLimit(resolved); errRes != nil {
		return errRes, nil
	}
	data, err := os.ReadFile(resolved)
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
	resolved, errRes := h.resolveDest(path)
	if errRes != nil {
		return errRes, nil
	}
	data, ok := args["data"].(map[string]any)
	if !ok {
		return textError("data object is required"), nil
	}
	pretty, _ := json.MarshalIndent(data, "", "  ")
	if errRes := checkWriteSize(len(pretty)); errRes != nil {
		return errRes, nil
	}
	bak, errRes := ensureWritable(resolved, "write_json", args)
	if errRes != nil {
		return errRes, nil
	}
	if errRes := mkdirParent(resolved); errRes != nil {
		return errRes, nil
	}
	if err := os.WriteFile(resolved, pretty, 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(pretty), "backup": bak}), nil
}

func (h *ToolHandlers) handleCreateZip(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	sourcesRaw, ok := args["sources"].([]any)
	if !ok || len(sourcesRaw) == 0 {
		return textError("path and sources array are required"), nil
	}
	if len(sourcesRaw) > fsMaxSrcs {
		return textError(fmt.Sprintf("too many sources: %d over the %d limit", len(sourcesRaw), fsMaxSrcs)), nil
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
	resolved, errRes := h.resolveDest(path)
	if errRes != nil {
		return errRes, nil
	}
	bak, errRes := ensureWritable(resolved, "create_zip", args)
	if errRes != nil {
		return errRes, nil
	}
	if errRes := mkdirParent(resolved); errRes != nil {
		return errRes, nil
	}
	f, err := os.Create(resolved)
	if err != nil {
		return textError("create zip: " + err.Error()), nil
	}
	w := zip.NewWriter(f)
	fail := func(format string, a ...any) (*mcp.CallToolResult, error) {
		wCloseErr := w.Close()
		fCloseErr := f.Close()
		_ = os.Remove(resolved)
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
	var total int64
	for _, src := range sources {
		rsrc, errRes := h.resolveRead(src)
		if errRes != nil {
			return fail("source not allowed: %s", src)
		}
		if _, errRes := statWithinLimit(rsrc); errRes != nil {
			return fail("source over limit: %s", src)
		}
		data, err := os.ReadFile(rsrc)
		if err != nil {
			return fail("read %s: %v", src, err)
		}
		total += int64(len(data))
		if total > fsMaxZipB {
			return fail("sources exceed %d bytes total: refusing to zip", fsMaxZipB)
		}
		zf, err := w.Create(filepath.Base(rsrc))
		if err != nil {
			return fail("create entry %s: %v", filepath.Base(rsrc), err)
		}
		if _, err := zf.Write(data); err != nil {
			return fail("write %s: %v", filepath.Base(rsrc), err)
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
	return jsonResult(map[string]any{"path": path, "files": written, "sources": len(sources), "backup": bak}), nil
}

func (h *ToolHandlers) handleExtractZip(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	dest, _ := args["dest"].(string)
	if path == "" || dest == "" {
		return textError("path and dest are required"), nil
	}
	resolvedZip, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	if _, errRes := statWithinLimit(resolvedZip); errRes != nil {
		return errRes, nil
	}
	absDest, errRes := h.resolveDest(dest)
	if errRes != nil {
		return errRes, nil
	}
	if fi, err := os.Lstat(absDest); err == nil && !fi.IsDir() {
		return textError(fmt.Sprintf("dest exists and is not a directory: %s (pass overwrite=true to replace it)", dest)), nil
	}
	// A symlinked dest is already refused by resolveDest; an existing file
	// dest still needs the overwrite flag.
	if _, err := os.Lstat(absDest); err == nil {
		if !overwriteOK(args) {
			return textError(fmt.Sprintf("dest exists: %s (pass overwrite=true)", dest)), nil
		}
	}
	r, err := zip.OpenReader(resolvedZip)
	if err != nil {
		return textError("open zip: " + err.Error()), nil
	}
	defer r.Close()
	if err := os.MkdirAll(absDest, 0755); err != nil {
		return textError(fmt.Sprintf("mkdir %s: %v", absDest, err)), nil
	}
	var extracted []string
	var totalBytes int64
	var fileCount int
	for _, f := range r.File {
		name := f.Name
		if name == "" || name == "." || name == "/" {
			continue
		}
		if strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
			return textError(fmt.Sprintf("absolute path blocked: %s", name)), nil
		}
		blocked := false
		for _, seg := range strings.Split(filepath.ToSlash(name), "/") {
			if seg == ".." {
				blocked = true
				break
			}
		}
		if blocked {
			return textError(fmt.Sprintf("path traversal blocked: %s", name)), nil
		}
		if f.FileInfo().Mode()&fs.ModeSymlink != 0 {
			return textError(fmt.Sprintf("symlink entry blocked: %s", name)), nil
		}
		target := filepath.Join(absDest, filepath.Clean("/"+name))
		if !isWithin(target, absDest) {
			return textError(fmt.Sprintf("path traversal blocked: %s", name)), nil
		}
		if f.FileInfo().IsDir() {
			if fi, err := os.Lstat(target); err == nil && fi.Mode()&os.ModeSymlink != 0 {
				return textError(fmt.Sprintf("refusing to follow symlink: %s", target)), nil
			}
			if err := os.MkdirAll(target, 0755); err != nil {
				return textError(fmt.Sprintf("mkdir %s: %v", target, err)), nil
			}
			continue
		}
		fileCount++
		if fileCount > fsMaxZip {
			return textError(fmt.Sprintf("zip holds more than %d files: refusing to extract", fsMaxZip)), nil
		}
		parent := filepath.Dir(target)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return textError(fmt.Sprintf("mkdir %s: %v", parent, err)), nil
		}
		// Re-resolve the parent: a pre-existing symlink subdir must not
		// redirect the write outside dest.
		if realParent, err := filepath.EvalSymlinks(parent); err != nil || !isWithin(realParent, absDest) {
			return textError(fmt.Sprintf("symlink escape blocked: %s", name)), nil
		}
		if fi, err := os.Lstat(target); err == nil {
			if fi.Mode()&os.ModeSymlink != 0 {
				return textError(fmt.Sprintf("refusing to follow symlink: %s", target)), nil
			}
			if !overwriteOK(args) {
				return textError(fmt.Sprintf("file exists: %s (pass overwrite=true)", target)), nil
			}
		}
		rc, err := f.Open()
		if err != nil {
			return textError(fmt.Sprintf("open %s: %v", name, err)), nil
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			rc.Close()
			return textError(fmt.Sprintf("create %s: %v", target, err)), nil
		}
		// Cap this entry so the running total never exceeds the budget.
		n, err := io.Copy(out, io.LimitReader(rc, fsMaxZipB-totalBytes+1))
		cerr := rc.Close()
		werr := out.Close()
		if err != nil {
			return textError(fmt.Sprintf("write %s: %v", target, err)), nil
		}
		if cerr != nil {
			return textError(fmt.Sprintf("close %s: %v", name, cerr)), nil
		}
		if werr != nil {
			return textError(fmt.Sprintf("close %s: %v", target, werr)), nil
		}
		totalBytes += n
		if totalBytes > fsMaxZipB {
			return textError(fmt.Sprintf("zip extracts more than %d bytes total: refusing to extract", fsMaxZipB)), nil
		}
		extracted = append(extracted, target)
	}
	return jsonResult(map[string]any{"dest": dest, "extracted": len(extracted), "files": extracted}), nil
}

func (h *ToolHandlers) handleFileExists(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	// Lstat the leaf without following it; only the parent must resolve.
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return textError("resolve path: " + err.Error()), nil
	}
	parent, err := storage.ResolvePath(filepath.Dir(abs), h.effectiveRoots())
	if err != nil {
		return textError(err.Error()), nil
	}
	_, err = os.Lstat(filepath.Join(parent, filepath.Base(abs)))
	return jsonResult(map[string]any{"path": path, "exists": err == nil}), nil
}

func (h *ToolHandlers) handleWordCount(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	resolved, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	if _, errRes := statWithinLimit(resolved); errRes != nil {
		return errRes, nil
	}
	data, err := os.ReadFile(resolved)
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
	resolved, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	if _, errRes := statWithinLimit(resolved); errRes != nil {
		return errRes, nil
	}
	f, err := os.Open(resolved)
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
	resolved, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	if _, errRes := statWithinLimit(resolved); errRes != nil {
		return errRes, nil
	}
	f, err := os.Open(resolved)
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
	resolved, errRes := h.resolveRead(root)
	if errRes != nil {
		return errRes, nil
	}
	var tree []string
	skipped, err := walkCapped(resolved, func(p string, d fs.DirEntry) error {
		rel, err := filepath.Rel(resolved, p)
		if err != nil {
			return nil
		}
		tree = append(tree, rel)
		return nil
	})
	if err != nil {
		return textError(err.Error()), nil
	}
	if skipped > 0 {
		auditLog("tree_directory", resolved, "ok", map[string]any{"skipped_symlinks": skipped})
	}
	return jsonResult(tree), nil
}

func (h *ToolHandlers) handleCSVtoJSON(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	resolved, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	if _, errRes := statWithinLimit(resolved); errRes != nil {
		return errRes, nil
	}
	data, err := os.ReadFile(resolved)
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
		for i, hdr := range headers {
			if i < len(row) {
				entry[hdr] = row[i]
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
	resolved, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	if _, errRes := statWithinLimit(resolved); errRes != nil {
		return errRes, nil
	}
	data, err := os.ReadFile(resolved)
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
		for _, hdr := range headers {
			row = append(row, fmt.Sprint(rec[hdr]))
		}
		rows = append(rows, row)
	}
	csvContent, err := ooxml.FormatCSV(headers, rows)
	if err != nil {
		return textError("format csv: " + err.Error()), nil
	}
	csvPath, errRes := h.resolveDest(resolved + ".csv")
	if errRes != nil {
		return errRes, nil
	}
	if errRes := checkWriteSize(len(csvContent)); errRes != nil {
		return errRes, nil
	}
	bak, errRes := ensureWritable(csvPath, "json_to_csv", args)
	if errRes != nil {
		return errRes, nil
	}
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		return textError("write csv: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": csvPath, "size": len(csvContent), "backup": bak}), nil
}

func (h *ToolHandlers) handleTouchFile(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return textError("path is required"), nil
	}
	resolved, errRes := h.resolveDest(path)
	if errRes != nil {
		return errRes, nil
	}
	if fi, err := os.Lstat(resolved); err == nil && fi.IsDir() {
		return textError(fmt.Sprintf("path %q is a directory", path)), nil
	}
	if errRes := mkdirParent(resolved); errRes != nil {
		return errRes, nil
	}
	f, err := os.OpenFile(resolved, os.O_CREATE, 0644)
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
	resolved, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	info, errRes := statWithinLimit(resolved)
	if errRes != nil {
		return errRes, nil
	}
	f, err := os.Open(resolved)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	defer f.Close()
	hsh := sha256.New()
	if _, err := io.Copy(hsh, io.LimitReader(f, fsMaxRead+1)); err != nil {
		return textError("hash file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "sha256": hex.EncodeToString(hsh.Sum(nil)), "size": info.Size()}), nil
}

func (h *ToolHandlers) handleDiskUsage(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}
	resolved, errRes := h.resolveRead(path)
	if errRes != nil {
		return errRes, nil
	}
	var totalSize int64
	var fileCount, dirCount int
	skipped, err := walkCapped(resolved, func(p string, d fs.DirEntry) error {
		if d.IsDir() {
			dirCount++
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		fileCount++
		totalSize += info.Size()
		return nil
	})
	if err != nil {
		return textError(err.Error()), nil
	}
	out := map[string]any{
		"path": path, "files": fileCount, "directories": dirCount, "total_size": totalSize,
	}
	if skipped > 0 {
		out["skipped_symlinks"] = skipped
	}
	return jsonResult(out), nil
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
	resolved, errRes := h.resolveDest(path)
	if errRes != nil {
		return errRes, nil
	}
	if errRes := checkWriteSize(len(content)); errRes != nil {
		return errRes, nil
	}
	bak, errRes := ensureWritable(resolved, "write_markdown", args)
	if errRes != nil {
		return errRes, nil
	}
	if errRes := mkdirParent(resolved); errRes != nil {
		return errRes, nil
	}
	if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path, "size": len(content), "backup": bak}), nil
}
