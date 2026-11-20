package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skipSymlinkIfUnsupported skips when the platform or user cannot create
// symlinks. Only permission errors skip; anything else still fails.
func skipSymlinkIfUnsupported(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if os.IsPermission(err) {
		t.Skipf("symlink creation unsupported: %v", err)
	}
	t.Fatalf("symlink creation failed: %v", err)
}

func newTestStore(t *testing.T) *LocalStore {
	t.Helper()
	dir, err := os.MkdirTemp("", "mcp-storage-test-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	s, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return s
}

func TestCreateAndGet(t *testing.T) {
	s := newTestStore(t)

	doc, err := s.Create("test.txt", []byte("hello"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if doc.Filename != "test.txt" {
		t.Errorf("filename = %q, want %q", doc.Filename, "test.txt")
	}
	if doc.Size != 5 {
		t.Errorf("size = %d, want %d", doc.Size, 5)
	}
	if doc.ID == "" {
		t.Error("ID is empty")
	}

	got, err := s.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Filename != doc.Filename {
		t.Errorf("filename = %q, want %q", got.Filename, doc.Filename)
	}
}

func TestGetData(t *testing.T) {
	s := newTestStore(t)
	content := []byte("test data content")
	doc, err := s.Create("data.bin", content)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	data, err := s.GetData(doc.ID)
	if err != nil {
		t.Fatalf("GetData: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("data = %q, want %q", string(data), string(content))
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)
	doc, err := s.Create("delete_me.txt", []byte("bye"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Delete(doc.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := s.Get(doc.ID); err != ErrNotFound {
		t.Errorf("Get after Delete: want ErrNotFound, got %v", err)
	}

	dataPath := filepath.Join(s.base, doc.ID+".data")
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Errorf("data file should be removed, stat: %v", err)
	}
}

func TestDeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.Delete("nonexistent"); err != ErrNotFound {
		t.Errorf("Delete nonexistent: want ErrNotFound, got %v", err)
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)
	s.Create("a.txt", []byte("a"))
	s.Create("b.txt", []byte("b"))

	docs, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(docs) != 2 {
		t.Errorf("len = %d, want 2", len(docs))
	}
}

func TestListEmpty(t *testing.T) {
	s := newTestStore(t)
	docs, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("len = %d, want 0", len(docs))
	}
}

func TestUpdate(t *testing.T) {
	s := newTestStore(t)
	doc, err := s.Create("update.txt", []byte("original"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := s.Update(doc.ID, []byte("modified"))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Size != 8 {
		t.Errorf("size = %d, want 8", updated.Size)
	}

	data, _ := s.GetData(doc.ID)
	if string(data) != "modified" {
		t.Errorf("data = %q, want %q", string(data), "modified")
	}
}

func TestUpdateNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Update("nonexistent", []byte("data")); err != ErrNotFound {
		t.Errorf("Update nonexistent: want ErrNotFound, got %v", err)
	}
}

func TestAddRefAndGetRefs(t *testing.T) {
	s := newTestStore(t)
	doc1, _ := s.Create("main.txt", []byte("main"))
	doc2, _ := s.Create("ref.txt", []byte("ref"))

	if err := s.AddRef(doc1.ID, doc2.ID); err != nil {
		t.Fatalf("AddRef: %v", err)
	}

	refs, err := s.GetRefs(doc1.ID)
	if err != nil {
		t.Fatalf("GetRefs: %v", err)
	}
	if len(refs) != 1 || refs[0] != doc2.ID {
		t.Errorf("refs = %v, want [%s]", refs, doc2.ID)
	}

	doc1ref, _ := s.Get(doc1.ID)
	if len(doc1ref.Refs) != 1 || doc1ref.Refs[0] != doc2.ID {
		t.Errorf("Document.Refs = %v, want [%s]", doc1ref.Refs, doc2.ID)
	}
}

func TestAddRefToNonexistent(t *testing.T) {
	s := newTestStore(t)
	doc, _ := s.Create("source.txt", []byte("src"))

	if err := s.AddRef(doc.ID, "ghost"); err == nil {
		t.Error("AddRef to nonexistent target: want error, got nil")
	}
	if err := s.AddRef("ghost", doc.ID); err == nil {
		t.Error("AddRef from nonexistent source: want error, got nil")
	}
}

func TestMimeType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"doc.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"sheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"deck.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		{"doc.pdf", "application/pdf"},
		{"text.txt", "text/plain"},
		{"data.csv", "text/csv"},
		{"unknown.xyz", "application/octet-stream"},
	}
	for _, tt := range tests {
		got := mimeType(tt.filename)
		if got != tt.want {
			t.Errorf("mimeType(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}

func TestResolvePathInsideRoot(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "sub", "file.txt")
	got, err := ResolvePath(p, []string{root})
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonicalRoot, "sub", "file.txt")
	if got != want {
		t.Errorf("resolved = %q, want %q", got, want)
	}
}

func TestResolvePathTraversalDenied(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{
		filepath.Join(root, "..", "escape.txt"),
		filepath.Join(root, "..", "..", "etc", "passwd"),
		"/etc/passwd",
		filepath.Join(root, "a\x00b"),
	} {
		if _, err := ResolvePath(p, []string{root}); err == nil {
			t.Errorf("ResolvePath(%q): want error, got nil", p)
		} else if msg := err.Error(); !strings.Contains(msg, "path outside allowed roots") && !strings.Contains(msg, "NUL") {
			t.Errorf("ResolvePath(%q) message = %q", p, msg)
		}
	}
}

func TestResolvePathSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("s"), 0644); err != nil {
		t.Fatal(err)
	}
	skipSymlinkIfUnsupported(t, os.Symlink(secret, filepath.Join(root, "link.txt")))
	if _, err := ResolvePath(filepath.Join(root, "link.txt"), []string{root}); err == nil {
		t.Error("file symlink escape: want error, got nil")
	}
	skipSymlinkIfUnsupported(t, os.Symlink(outside, filepath.Join(root, "dlink")))
	if _, err := ResolvePath(filepath.Join(root, "dlink", "secret.txt"), []string{root}); err == nil {
		t.Error("dir symlink escape: want error, got nil")
	}
}

func TestResolveDestRefusesLeafSymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real.txt")
	if err := os.WriteFile(real, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	skipSymlinkIfUnsupported(t, os.Symlink(real, link))
	if _, err := ResolveDest(link, []string{root}); err == nil {
		t.Error("ResolveDest on symlink: want error, got nil")
	}
	if _, err := ResolveDest(filepath.Join(root, "..", "out.txt"), []string{root}); err == nil {
		t.Error("ResolveDest outside parent: want error, got nil")
	}
}

func TestDesktopPathPreservesManagedDocumentAcrossRenames(t *testing.T) {
	s := newTestStore(t)
	doc, err := s.Create("Old.DOCX", []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.Create("Reference.txt", []byte("reference"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddRef(doc.ID, ref.ID); err != nil {
		t.Fatal(err)
	}
	path, err := s.DesktopPath(doc.ID)
	if err != nil || filepath.Ext(path) != ".docx" {
		t.Fatalf("DesktopPath = %q, %v", path, err)
	}
	if _, err := os.Stat(filepath.Join(s.BaseDir(), doc.ID+".data")); !os.IsNotExist(err) {
		t.Fatalf("legacy payload still exists: %v", err)
	}
	if err := os.WriteFile(path, []byte("saved by editor"), 0600); err != nil {
		t.Fatal(err)
	}
	renamed, err := s.Rename(doc.ID, "New.docx")
	if err != nil || renamed.ID != doc.ID || len(renamed.Refs) != 1 || renamed.Refs[0] != ref.ID {
		t.Fatalf("Rename = %+v, %v", renamed, err)
	}
	if current, err := s.DesktopPath(doc.ID); err != nil || current != path {
		t.Fatalf("DesktopPath after rename = %q, %v", current, err)
	}
	if got, err := s.GetData(doc.ID); err != nil || string(got) != "saved by editor" {
		t.Fatalf("GetData = %q, %v", got, err)
	}
}

func TestTwoStoresKeepAllConcurrentCreates(t *testing.T) {
	first := newTestStore(t)
	second, err := NewLocalStore(first.BaseDir())
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 20)
	for i := range 20 {
		store := first
		if i%2 == 1 {
			store = second
		}
		go func() {
			<-start
			_, err := store.Create("sample.txt", []byte("content"))
			results <- err
		}()
	}
	close(start)
	for range 20 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	docs, err := second.List()
	if err != nil || len(docs) != 20 {
		t.Fatalf("List found %d documents: %v", len(docs), err)
	}
}

func TestStoreRefusesUpdateAndDeleteWhileEditorIsOpen(t *testing.T) {
	s := newTestStore(t)
	doc, err := s.Create("Example.docx", []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.DesktopPath(doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(s.BaseDir(), ".~lock."+filepath.Base(path)+"#")
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(doc.ID, []byte("new")); err == nil {
		t.Fatal("updated an open document")
	}
	if err := s.Delete(doc.ID); err == nil {
		t.Fatal("deleted an open document")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if data, err := s.GetData(doc.ID); err != nil || string(data) != "original" {
		t.Fatalf("original = %q, %v", data, err)
	}
	updated, err := s.Update(doc.ID, []byte("new"))
	if err != nil || updated.Size != 3 {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
}
