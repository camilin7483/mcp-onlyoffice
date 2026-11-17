package analyzer

import (
	"testing"

	"github.com/cam/mcp-onlyoffice/internal/ooxml"
	"github.com/cam/mcp-onlyoffice/internal/storage"
)

func newTestAnalyzer(t *testing.T) (*Analyzer, *storage.LocalStore) {
	t.Helper()
	store, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return New(store), store
}

func TestAnalyzeDocx(t *testing.T) {
	an, store := newTestAnalyzer(t)
	data, err := ooxml.CreateDOCXFromText("Report Title", "hello world from analyzer test")
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	doc, err := store.Create("report.docx", data)
	if err != nil {
		t.Fatalf("store create: %v", err)
	}
	a, err := an.Analyze(doc.ID)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if a.Format != "docx" {
		t.Fatalf("format = %q, want docx", a.Format)
	}
	if a.Docx == nil {
		t.Fatal("expected Docx analysis")
	}
	if a.Docx.Paragraphs == 0 {
		t.Fatal("expected non-zero paragraph count")
	}
	if a.Docx.WordCount == 0 {
		t.Fatal("expected non-zero word count")
	}
}

func TestAnalyzeXlsx(t *testing.T) {
	an, store := newTestAnalyzer(t)
	data, err := ooxml.CreateXLSXFromData("Sheet1",
		[]string{"name", "age"},
		[][]string{{"alice", "30"}, {"bob", "25"}})
	if err != nil {
		t.Fatalf("create xlsx: %v", err)
	}
	doc, err := store.Create("data.xlsx", data)
	if err != nil {
		t.Fatalf("store create: %v", err)
	}
	a, err := an.Analyze(doc.ID)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if a.Format != "xlsx" {
		t.Fatalf("format = %q, want xlsx", a.Format)
	}
	if a.Xlsx == nil {
		t.Fatal("expected Xlsx analysis")
	}
	if a.Xlsx.Sheets == 0 {
		t.Fatal("expected at least one sheet")
	}
	if a.Xlsx.Rows == 0 {
		t.Fatal("expected non-zero row count")
	}
	if a.Xlsx.Cols == 0 {
		t.Fatal("expected non-zero col count")
	}
}

func TestAnalyzeInvalidDocx(t *testing.T) {
	an, store := newTestAnalyzer(t)
	doc, err := store.Create("broken.docx", []byte("not a zip file"))
	if err != nil {
		t.Fatalf("store create: %v", err)
	}
	a, err := an.Analyze(doc.ID)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if a.Valid {
		t.Fatal("expected Valid=false for corrupt docx")
	}
	if len(a.Issues) == 0 {
		t.Fatal("expected at least one issue for corrupt docx")
	}
}

func TestAnalyzeUnknownID(t *testing.T) {
	an, _ := newTestAnalyzer(t)
	if _, err := an.Analyze("does-not-exist"); err == nil {
		t.Fatal("expected error for unknown id")
	}
}
