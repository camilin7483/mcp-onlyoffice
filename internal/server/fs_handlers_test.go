package server

import (
	"archive/zip"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	auditlog "github.com/cam/mcp-onlyoffice/internal/audit"
	"github.com/cam/mcp-onlyoffice/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// testFS wires a ToolHandlers scoped to a temp root and restores globals.
func testFS(t *testing.T) (*ToolHandlers, string) {
	t.Helper()
	root := t.TempDir()
	storeDir := t.TempDir()
	s, err := storage.NewLocalStore(storeDir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	h := &ToolHandlers{store: s}

	oldRoots, oldAudit := fsRoots, fsAudit
	oldWrite, oldRead, oldWalk := fsMaxWrite, fsMaxRead, fsMaxWalk
	oldZip, oldZipB, oldSrcs := fsMaxZip, fsMaxZipB, fsMaxSrcs
	t.Cleanup(func() {
		fsRoots, fsAudit = oldRoots, oldAudit
		fsMaxWrite, fsMaxRead, fsMaxWalk = oldWrite, oldRead, oldWalk
		fsMaxZip, fsMaxZipB, fsMaxSrcs = oldZip, oldZipB, oldSrcs
	})
	h.SetAllowedRoots(root)
	return h, root
}

func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", r.Content[0])
	}
	return tc.Text
}

func callOK(t *testing.T, fn func(context.Context, map[string]any) (*mcp.CallToolResult, error), args map[string]any) string {
	t.Helper()
	r, err := fn(context.Background(), args)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if r.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(t, r))
	}
	return resultText(t, r)
}

func TestFSTraversalDenied(t *testing.T) {
	h, root := testFS(t)
	outside := filepath.Join(root, "..", "escape.txt")
	cases := []struct {
		name    string
		handler func(context.Context, map[string]any) (*mcp.CallToolResult, error)
		args    map[string]any
	}{
		{"read-traversal", h.handleReadFile, map[string]any{"path": outside}},
		{"read-abs", h.handleReadFile, map[string]any{"path": "/etc/passwd"}},
		{"read-nul", h.handleReadFile, map[string]any{"path": filepath.Join(root, "a\x00b")}},
		{"write-abs", h.handleWriteText, map[string]any{"path": "/etc/passwd", "content": "x"}},
		{"info-traversal", h.handleFileInfo, map[string]any{"path": outside}},
		{"list-abs", h.handleListDirectory, map[string]any{"path": "/etc"}},
		{"find-abs", h.handleFindFiles, map[string]any{"root": "/etc"}},
		{"tree-abs", h.handleTreeDirectory, map[string]any{"path": "/etc"}},
		{"delete-abs", h.handleDeleteFile, map[string]any{"path": "/etc/passwd"}},
		{"touch-traversal", h.handleTouchFile, map[string]any{"path": outside}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := tc.handler(context.Background(), tc.args)
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if !r.IsError {
				t.Fatalf("expected deny, got success: %s", resultText(t, r))
			}
			msg := resultText(t, r)
			if !strings.Contains(msg, "path outside allowed roots") && !strings.Contains(msg, "NUL") {
				t.Errorf("unexpected deny message: %s", msg)
			}
		})
	}
}

func TestFSSymlinkEscape(t *testing.T) {
	h, root := testFS(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("topsecret"), 0644); err != nil {
		t.Fatal(err)
	}
	// File symlink pointing outside.
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	r, _ := h.handleReadFile(context.Background(), map[string]any{"path": link})
	if !r.IsError || !strings.Contains(resultText(t, r), "path outside allowed roots") {
		t.Errorf("file symlink escape not denied: %s", resultText(t, r))
	}
	// Dir symlink pointing outside.
	dlink := filepath.Join(root, "dlink")
	if err := os.Symlink(outside, dlink); err != nil {
		t.Fatal(err)
	}
	r, _ = h.handleReadFile(context.Background(), map[string]any{"path": filepath.Join(dlink, "secret.txt")})
	if !r.IsError || !strings.Contains(resultText(t, r), "path outside allowed roots") {
		t.Errorf("dir symlink escape not denied: %s", resultText(t, r))
	}
	// Symlinked write/delete targets are refused outright.
	r, _ = h.handleWriteText(context.Background(), map[string]any{"path": link, "content": "pwn"})
	if !r.IsError || !strings.Contains(resultText(t, r), "refusing to follow symlink") {
		t.Errorf("symlink write not refused: %s", resultText(t, r))
	}
	r, _ = h.handleDeleteFile(context.Background(), map[string]any{"path": link})
	if !r.IsError || !strings.Contains(resultText(t, r), "refusing to follow symlink") {
		t.Errorf("symlink delete not refused: %s", resultText(t, r))
	}
}

func TestFSAllowedRootAccept(t *testing.T) {
	h, root := testFS(t)
	p := filepath.Join(root, "sub", "ok.txt")
	callOK(t, h.handleWriteText, map[string]any{"path": p, "content": "hello"})
	out := callOK(t, h.handleReadFile, map[string]any{"path": p})
	if !strings.Contains(out, "hello") {
		t.Errorf("roundtrip content missing: %s", out)
	}
}

func TestFSOverwriteRefusalAndBackup(t *testing.T) {
	h, root := testFS(t)
	auditDir := t.TempDir()
	al, err := auditlog.New(auditDir)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()
	h.SetAuditLogger(al)

	p := filepath.Join(root, "doc.txt")
	callOK(t, h.handleWriteText, map[string]any{"path": p, "content": "v1"})

	// Refusal without the flag; original untouched; no backup.
	r, _ := h.handleWriteText(context.Background(), map[string]any{"path": p, "content": "v2"})
	if !r.IsError || !strings.Contains(resultText(t, r), "file exists:") {
		t.Fatalf("expected overwrite refusal, got: %s", resultText(t, r))
	}
	if !strings.Contains(resultText(t, r), "overwrite=true") {
		t.Errorf("refusal must name the flag: %s", resultText(t, r))
	}
	data, _ := os.ReadFile(p)
	if string(data) != "v1" {
		t.Errorf("content clobbered: %q", data)
	}
	matches, _ := filepath.Glob(p + ".bak-*")
	if len(matches) != 0 {
		t.Errorf("unexpected backup files: %v", matches)
	}

	// Overwrite with flag: backup created, audit logged.
	callOK(t, h.handleWriteText, map[string]any{"path": p, "content": "v2", "overwrite": true})
	matches, _ = filepath.Glob(p + ".bak-*")
	if len(matches) != 1 {
		t.Fatalf("want 1 backup, got %v", matches)
	}
	bak, _ := os.ReadFile(matches[0])
	if string(bak) != "v1" {
		t.Errorf("backup content = %q, want v1", bak)
	}
	auditData, _ := os.ReadFile(al.Path())
	if !strings.Contains(string(auditData), "backup") {
		t.Errorf("audit missing backup entry: %s", auditData)
	}
}

func TestFSDeleteDirRequiresRecursive(t *testing.T) {
	h, root := testFS(t)
	auditDir := t.TempDir()
	al, err := auditlog.New(auditDir)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()
	h.SetAuditLogger(al)

	d := filepath.Join(root, "wipe")
	callOK(t, h.handleCreateDirectory, map[string]any{"path": d})
	callOK(t, h.handleWriteText, map[string]any{"path": filepath.Join(d, "f.txt"), "content": "x"})

	r, _ := h.handleDeleteFile(context.Background(), map[string]any{"path": d})
	if !r.IsError || !strings.Contains(resultText(t, r), "recursive=true") {
		t.Fatalf("expected recursive refusal, got: %s", resultText(t, r))
	}
	if _, err := os.Stat(d); err != nil {
		t.Fatalf("dir removed without recursive: %v", err)
	}
	callOK(t, h.handleDeleteFile, map[string]any{"path": d, "recursive": true})
	if _, err := os.Stat(d); !os.IsNotExist(err) {
		t.Errorf("dir still present after recursive delete")
	}
	auditData, _ := os.ReadFile(al.Path())
	for _, want := range []string{"attempt", `"outcome":"ok"`} {
		if !strings.Contains(string(auditData), want) {
			t.Errorf("audit missing %q: %s", want, auditData)
		}
	}
}

func writeTestZip(t *testing.T, path string, entries map[string]string, symlinks []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, content := range entries {
		zf, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(zf, content); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range symlinks {
		fh := &zip.FileHeader{Name: name, Method: zip.Store}
		fh.SetMode(os.ModeSymlink | 0777)
		zf, err := w.CreateHeader(fh)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(zf, "target"); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFSZipSlipBlocked(t *testing.T) {
	h, root := testFS(t)
	zp := filepath.Join(root, "evil.zip")
	writeTestZip(t, zp, map[string]string{"../evil.txt": "x"}, nil)
	r, _ := h.handleExtractZip(context.Background(), map[string]any{"path": zp, "dest": filepath.Join(root, "out")})
	if !r.IsError || !strings.Contains(resultText(t, r), "traversal blocked") {
		t.Errorf("zip-slip not blocked: %s", resultText(t, r))
	}
}

func TestFSZipAbsoluteEntryBlocked(t *testing.T) {
	h, root := testFS(t)
	zp := filepath.Join(root, "abs.zip")
	writeTestZip(t, zp, map[string]string{"/abs.txt": "x"}, nil)
	r, _ := h.handleExtractZip(context.Background(), map[string]any{"path": zp, "dest": filepath.Join(root, "out")})
	if !r.IsError || !strings.Contains(resultText(t, r), "absolute path blocked") {
		t.Errorf("absolute entry not blocked: %s", resultText(t, r))
	}
}

func TestFSZipSymlinkEntryBlocked(t *testing.T) {
	h, root := testFS(t)
	zp := filepath.Join(root, "link.zip")
	writeTestZip(t, zp, map[string]string{"ok.txt": "x"}, []string{"evil-link"})
	r, _ := h.handleExtractZip(context.Background(), map[string]any{"path": zp, "dest": filepath.Join(root, "out")})
	if !r.IsError || !strings.Contains(resultText(t, r), "symlink entry blocked") {
		t.Errorf("symlink entry not blocked: %s", resultText(t, r))
	}
}

func TestFSZipLimitsHook(t *testing.T) {
	h, root := testFS(t)
	zp := filepath.Join(root, "many.zip")
	writeTestZip(t, zp, map[string]string{"a.txt": "a", "b.txt": "b", "c.txt": "c"}, nil)

	fsMaxZip = 2
	r, _ := h.handleExtractZip(context.Background(), map[string]any{"path": zp, "dest": filepath.Join(root, "o1")})
	if !r.IsError || !strings.Contains(resultText(t, r), "more than 2 files") {
		t.Errorf("entry-count limit not enforced: %s", resultText(t, r))
	}

	fsMaxZip = 1000
	fsMaxZipB = 2
	r, _ = h.handleExtractZip(context.Background(), map[string]any{"path": zp, "dest": filepath.Join(root, "o2")})
	if !r.IsError || !strings.Contains(resultText(t, r), "more than 2 bytes") {
		t.Errorf("uncompressed-size limit not enforced: %s", resultText(t, r))
	}
}

func TestFSZipSymlinkedDestBlocked(t *testing.T) {
	h, root := testFS(t)
	outside := t.TempDir()
	destLink := filepath.Join(root, "destlink")
	if err := os.Symlink(outside, destLink); err != nil {
		t.Fatal(err)
	}
	zp := filepath.Join(root, "z.zip")
	writeTestZip(t, zp, map[string]string{"ok.txt": "x"}, nil)
	r, _ := h.handleExtractZip(context.Background(), map[string]any{"path": zp, "dest": destLink})
	if !r.IsError {
		t.Errorf("symlinked dest not blocked: %s", resultText(t, r))
	}
	if _, err := os.Stat(filepath.Join(outside, "ok.txt")); !os.IsNotExist(err) {
		t.Errorf("zip escaped through symlinked dest")
	}
}

func TestFSDeleteRootRefused(t *testing.T) {
	h, root := testFS(t)
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	// Nested roots: the parent resolves, so only the root guard can stop this.
	h.SetAllowedRoots(root, sub)
	r, _ := h.handleDeleteFile(context.Background(), map[string]any{"path": sub, "recursive": true})
	if !r.IsError || !strings.Contains(resultText(t, r), "allowed root") {
		t.Fatalf("root delete not refused: %s", resultText(t, r))
	}
	if _, err := os.Stat(sub); err != nil {
		t.Errorf("allowed root removed: %v", err)
	}
}

func TestFSMoveRootRefused(t *testing.T) {
	h, root := testFS(t)
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(root, "f.txt")
	callOK(t, h.handleWriteText, map[string]any{"path": f, "content": "x"})
	h.SetAllowedRoots(root, sub)

	// Source is a root: refused, still present.
	r, _ := h.handleMoveFile(context.Background(), map[string]any{"source": sub, "dest": filepath.Join(root, "moved")})
	if !r.IsError || !strings.Contains(resultText(t, r), "allowed root") {
		t.Fatalf("root move-source not refused: %s", resultText(t, r))
	}
	if _, err := os.Stat(sub); err != nil {
		t.Errorf("allowed root moved away: %v", err)
	}

	// Destination is a root: refused, both sides untouched.
	r, _ = h.handleMoveFile(context.Background(), map[string]any{"source": f, "dest": sub})
	if !r.IsError || !strings.Contains(resultText(t, r), "allowed root") {
		t.Fatalf("root move-dest not refused: %s", resultText(t, r))
	}
	if _, err := os.Stat(f); err != nil {
		t.Errorf("move source removed: %v", err)
	}
	if fi, err := os.Stat(sub); err != nil || !fi.IsDir() {
		t.Errorf("allowed root clobbered: %v %v", fi, err)
	}
}

func TestWriteBackupRefusesExistingAndSymlink(t *testing.T) {
	_, root := testFS(t)
	src := filepath.Join(root, "src.txt")
	if err := os.WriteFile(src, []byte("orig"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := src + ".bak-test"
	if err := writeBackup(src, dst); err != nil {
		t.Fatalf("writeBackup: %v", err)
	}
	if bak, _ := os.ReadFile(dst); string(bak) != "orig" {
		t.Errorf("backup content = %q, want orig", bak)
	}
	if fi, _ := os.Stat(dst); fi.Mode().Perm() != 0600 {
		t.Errorf("backup mode = %o, want 600", fi.Mode().Perm())
	}
	// Existing destination (regular file) is refused, source untouched.
	if err := writeBackup(src, dst); err == nil || !strings.Contains(err.Error(), "backup exists") {
		t.Errorf("existing backup not refused: %v", err)
	}

	// Symlink at the destination is refused and never followed: nothing
	// lands outside the root.
	outside := t.TempDir()
	planted := filepath.Join(outside, "evil.txt")
	link := filepath.Join(root, "link.bak")
	if err := os.Symlink(planted, link); err != nil {
		t.Fatal(err)
	}
	if err := writeBackup(src, link); err == nil {
		t.Errorf("symlink backup not refused")
	}
	if _, err := os.Stat(planted); !os.IsNotExist(err) {
		t.Errorf("backup escaped through symlink: %v", err)
	}
}

func TestEnsureWritableRefusesNonRegular(t *testing.T) {
	_, root := testFS(t)
	sock := filepath.Join(root, "sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix socket unavailable: %v", err)
	}
	defer l.Close()
	_, errRes := ensureWritable(sock, "write_text", map[string]any{"overwrite": true})
	if errRes == nil || !strings.Contains(resultText(t, errRes), "non-regular") {
		t.Fatalf("expected non-regular refusal, got: %+v", errRes)
	}
}

func TestWriteBackupFailureLeavesNoBak(t *testing.T) {
	_, root := testFS(t)
	// A directory as copy source: os.Open succeeds but io.Copy fails,
	// so the partial destination must be removed.
	srcDir := filepath.Join(root, "srcdir")
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "srcdir.bak-test")
	if err := writeBackup(srcDir, dst); err == nil {
		t.Fatal("expected backup of a directory to fail")
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatalf("partial backup left behind: %s", dst)
	}

	// A pre-created destination directory is refused without side effects.
	src := filepath.Join(root, "src.txt")
	if err := os.WriteFile(src, []byte("orig"), 0644); err != nil {
		t.Fatal(err)
	}
	dstDir := filepath.Join(root, "taken.bak")
	if err := os.Mkdir(dstDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeBackup(src, dstDir); err == nil {
		t.Fatal("expected existing destination to be refused")
	}
	if fi, err := os.Lstat(dstDir); err != nil || !fi.IsDir() {
		t.Fatalf("destination directory disturbed: %v %v", fi, err)
	}
}

func TestFSAppendTextLimitsAndAudit(t *testing.T) {
	h, root := testFS(t)
	auditDir := t.TempDir()
	al, err := auditlog.New(auditDir)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()
	h.SetAuditLogger(al)

	p := filepath.Join(root, "log.txt")
	callOK(t, h.handleWriteText, map[string]any{"path": p, "content": "abcd"})

	// Total resulting size over the cap is refused; file unchanged.
	fsMaxWrite = 4
	r, _ := h.handleAppendText(context.Background(), map[string]any{"path": p, "content": "e", "overwrite": true})
	if !r.IsError || !strings.Contains(resultText(t, r), "over the 4 byte limit") {
		t.Fatalf("append over cap not refused: %s", resultText(t, r))
	}
	if data, _ := os.ReadFile(p); string(data) != "abcd" {
		t.Errorf("content changed on refused append: %q", data)
	}
	fsMaxWrite = storage.MaxWriteBytes

	// Normal append works and audits.
	callOK(t, h.handleAppendText, map[string]any{"path": p, "content": "e", "overwrite": true})
	if data, _ := os.ReadFile(p); string(data) != "abcde" {
		t.Errorf("append content = %q, want abcde", data)
	}
	auditData, _ := os.ReadFile(al.Path())
	if !strings.Contains(string(auditData), "append_text") {
		t.Errorf("audit missing append_text entry: %s", auditData)
	}
}

func TestFSWalkAndWriteLimits(t *testing.T) {
	h, root := testFS(t)
	for i := 0; i < 5; i++ {
		p := filepath.Join(root, "f"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	fsMaxWalk = 2
	r, _ := h.handleTreeDirectory(context.Background(), map[string]any{"path": root})
	if !r.IsError || !strings.Contains(resultText(t, r), "exceeds 2 entries") {
		t.Errorf("walk limit not enforced: %s", resultText(t, r))
	}
	fsMaxWalk = 10000

	fsMaxWrite = 4
	r, _ = h.handleWriteText(context.Background(), map[string]any{"path": filepath.Join(root, "big.txt"), "content": "12345"})
	if !r.IsError || !strings.Contains(resultText(t, r), "over the 4 byte limit") {
		t.Errorf("write limit not enforced: %s", resultText(t, r))
	}
}
