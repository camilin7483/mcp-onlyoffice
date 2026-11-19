package validator

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/cam/mcp-onlyoffice/internal/storage"
)

func newTestStore(t *testing.T) *storage.LocalStore {
	t.Helper()
	dir, err := os.MkdirTemp("", "mcp-validator-test-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	s, err := storage.NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return s
}

func TestValidateDOCX(t *testing.T) {
	s := newTestStore(t)
	v := New(s)

	// minimal valid DOCX-like content (just a zip with required files)
	data := createMinimalDocx()
	doc, err := s.Create("test.docx", data)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	result, err := v.Validate(doc.ID)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected valid, got checks: %+v", result.Checks)
	}
}

func TestValidateOOXMLMissingParts(t *testing.T) {
	s := newTestStore(t)
	v := New(s)

	// create a zip without required OOXML parts
	doc, err := s.Create("broken.docx", []byte("not a real docx"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	result, err := v.Validate(doc.ID)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid for broken docx")
	}
}

func TestValidateEmptyFile(t *testing.T) {
	s := newTestStore(t)
	v := New(s)

	doc, err := s.Create("empty.docx", []byte{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	result, err := v.Validate(doc.ID)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	hasSizeCheck := false
	for _, c := range result.Checks {
		if c.Name == "file_size" && c.Status == "fail" {
			hasSizeCheck = true
		}
	}
	if !hasSizeCheck {
		t.Error("expected file_size check to fail for empty file")
	}
}

func TestValidateTextFile(t *testing.T) {
	s := newTestStore(t)
	v := New(s)

	doc, err := s.Create("notes.txt", []byte("hello world"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	result, err := v.Validate(doc.ID)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected valid, got checks: %+v", result.Checks)
	}
}

func createMinimalDocx() []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:r>
        <w:t>Hello</w:t>
      </w:r>
    </w:p>
  </w:body>
</w:document>`,
	}

	for name, content := range files {
		f, _ := w.Create(name)
		f.Write([]byte(content))
	}
	w.Close()
	return buf.Bytes()
}

// TestValidateOversizedPart builds a docx whose word/document.xml
// decompresses past MaxValidatorPartBytes (stored compressed, so the file
// itself is small): validation must fail clearly on the per-part cap,
// never buffer unbounded.
func TestValidateOversizedPart(t *testing.T) {
	s := newTestStore(t)
	v := New(s)

	big := bytes.Repeat([]byte("a"), int(MaxValidatorPartBytes)+1<<20)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`,
	}
	for name, content := range files {
		f, _ := w.Create(name)
		f.Write([]byte(content))
	}
	f, _ := w.Create("word/document.xml")
	f.Write(big)
	w.Close()
	t.Logf("oversized file size: %d bytes", buf.Len())

	doc, err := s.Create("big.docx", buf.Bytes())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	res, err := v.Validate(doc.ID)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Valid {
		t.Fatal("oversized part: want invalid, got valid")
	}
	found := false
	for _, c := range res.Checks {
		if strings.Contains(c.Detail, "byte limit") {
			found = true
		}
	}
	if !found {
		t.Errorf("oversized part: no check names the byte limit: %+v", res.Checks)
	}
}
