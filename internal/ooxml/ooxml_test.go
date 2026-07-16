package ooxml

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestCreateDOCX(t *testing.T) {
	data, err := CreateDOCX("Test Title")
	if err != nil {
		t.Fatalf("CreateDOCX: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	files := make(map[string]bool)
	for _, f := range zr.File {
		files[f.Name] = true
	}

	expected := []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml"}
	for _, name := range expected {
		if !files[name] {
			t.Errorf("missing file: %s", name)
		}
	}

	if data[0] != 'P' || data[1] != 'K' {
		t.Error("data does not start with PK zip signature")
	}
}

func TestCreateDOCXEmpty(t *testing.T) {
	// should create a default paragraph
	data, err := CreateDOCXFromParagraphs([]DocxParagraph{})
	if err != nil {
		t.Fatalf("CreateDOCXFromParagraphs empty: %v", err)
	}
	if len(data) < 100 {
		t.Errorf("docx too small: %d bytes", len(data))
	}
}

func TestCreateDOCXFromText(t *testing.T) {
	data, err := CreateDOCXFromText("Title", "Line1\nLine2\nLine3")
	if err != nil {
		t.Fatalf("CreateDOCXFromText: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	var docContent string
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			buf := new(bytes.Buffer)
			buf.ReadFrom(rc)
			rc.Close()
			docContent = buf.String()
		}
	}

	if docContent == "" {
		t.Fatal("document.xml not found or empty")
	}

	if !strings.Contains(docContent, "Title") {
		t.Error("document.xml missing title")
	}
	if !strings.Contains(docContent, "Line1") {
		t.Error("document.xml missing Line1")
	}
	if !strings.Contains(docContent, "Line2") {
		t.Error("document.xml missing Line2")
	}
}

func TestCreateXLSX(t *testing.T) {
	data, err := CreateXLSX("Sheet1")
	if err != nil {
		t.Fatalf("CreateXLSX: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	hasWorkbook := false
	hasSheet := false
	for _, f := range zr.File {
		if f.Name == "xl/workbook.xml" {
			hasWorkbook = true
		}
		if strings.HasPrefix(f.Name, "xl/worksheets/") {
			hasSheet = true
		}
	}

	if !hasWorkbook {
		t.Error("missing xl/workbook.xml")
	}
	if !hasSheet {
		t.Error("missing worksheet")
	}
}

func TestCreateXLSXWithData(t *testing.T) {
	headers := []string{"Name", "Value"}
	rows := [][]string{{"A", "1"}, {"B", "2"}}
	data, err := CreateXLSXFromData("Data", headers, rows)
	if err != nil {
		t.Fatalf("CreateXLSXFromData: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	for _, f := range zr.File {
		if f.Name == "xl/sharedStrings.xml" {
			rc, _ := f.Open()
			buf := new(bytes.Buffer)
			buf.ReadFrom(rc)
			rc.Close()
			content := buf.String()
			if !strings.Contains(content, "Name") || !strings.Contains(content, "Value") {
				t.Error("sharedStrings.xml missing headers")
			}
			return
		}
	}
	t.Error("missing sharedStrings.xml")
}

func TestCreatePPTX(t *testing.T) {
	data, err := CreatePPTX("Test Pres")
	if err != nil {
		t.Fatalf("CreatePPTX: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	hasPres := false
	hasSlide := false
	for _, f := range zr.File {
		if f.Name == "ppt/presentation.xml" {
			hasPres = true
		}
		if strings.HasPrefix(f.Name, "ppt/slides/slide") {
			hasSlide = true
		}
	}

	if !hasPres {
		t.Error("missing ppt/presentation.xml")
	}
	if !hasSlide {
		t.Error("missing slide file")
	}
}

func TestCreatePPTXFromSlides(t *testing.T) {
	slides := []SlideData{
		{Title: "Slide 1", Content: "Content 1"},
		{Title: "Slide 2", Content: "Content 2\nLine 2"},
	}
	data, err := CreatePPTXFromSlides(slides)
	if err != nil {
		t.Fatalf("CreatePPTXFromSlides: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	slideCount := 0
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slideCount++
		}
	}
	if slideCount != 2 {
		t.Errorf("slide count = %d, want 2", slideCount)
	}
}

func TestCreatePPTXEmptySlides(t *testing.T) {
	// should create a default slide
	data, err := CreatePPTXFromSlides([]SlideData{})
	if err != nil {
		t.Fatalf("CreatePPTXFromSlides empty: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	slideCount := 0
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slideCount++
		}
	}
	if slideCount != 1 {
		t.Errorf("empty slides: got %d, want 1", slideCount)
	}
}

func TestParseCSV(t *testing.T) {
	csv := "Name,Value\nA,1\nB,2\n"
	headers, rows, err := ParseCSV(csv)
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	if len(headers) != 2 || headers[0] != "Name" {
		t.Errorf("headers = %v, want [Name Value]", headers)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d, want 2", len(rows))
	}
}

func TestParseCSVEmpty(t *testing.T) {
	_, rows, err := ParseCSV("")
	if err != nil {
		t.Fatalf("ParseCSV empty: %v", err)
	}
	if rows == nil {
		t.Error("rows should be non-nil empty slice")
	}
}

func TestFormatCSV(t *testing.T) {
	headers := []string{"A", "B"}
	rows := [][]string{{"1", "2"}}
	out, err := FormatCSV(headers, rows)
	if err != nil {
		t.Fatalf("FormatCSV: %v", err)
	}
	if !strings.Contains(out, "A,B") {
		t.Errorf("output missing header: %s", out)
	}
	if !strings.Contains(out, "1,2") {
		t.Errorf("output missing data: %s", out)
	}
}

func TestXMLEncode(t *testing.T) {
	input := "a & b < c > d \"e\" 'f'"
	got := xmlEscape(input)
	want := "a &amp; b &lt; c &gt; d &quot;e&quot; &apos;f&apos;"
	if got != want {
		t.Errorf("xmlEscape = %q, want %q", got, want)
	}
}

func TestCellTypeOf(t *testing.T) {
	tests := []struct {
		val     string
		wantTyp cellType
		wantVal string
	}{
		{"hello", cellString, "hello"},
		{"=SUM(A1:A10)", cellFormula, "SUM(A1:A10)"},
		{"42", cellNumber, "42"},
		{"3.14", cellNumber, "3.14"},
		{"true", cellNumber, "true"},
	}
	for _, tt := range tests {
		typ, val := cellTypeOf(tt.val)
		if typ != tt.wantTyp || val != tt.wantVal {
			t.Errorf("cellTypeOf(%q) = (%d, %q), want (%d, %q)", tt.val, typ, val, tt.wantTyp, tt.wantVal)
		}
	}
}

func TestColName(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "A"},
		{1, "B"},
		{25, "Z"},
		{26, "AA"},
		{51, "AZ"},
	}
	for _, tt := range tests {
		got := colName(tt.n)
		if got != tt.want {
			t.Errorf("colName(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
