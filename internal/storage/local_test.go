package storage

import (
	"os"
	"path/filepath"
	"testing"
)

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
