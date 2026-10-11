package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
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
		{"true", cellBool, "true"},
		{"FALSE", cellBool, "false"},
		{"NaN", cellString, "NaN"},
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

// validatePackage mimics the validator: ZIP opens, required parts exist,
// every XML part is well-formed, and rel targets resolve.
func validatePackage(t *testing.T, data []byte, required []string) (bool, string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false, "zip open: " + err.Error()
	}
	files := map[string]bool{}
	for _, f := range zr.File {
		files[f.Name] = true
	}
	for _, name := range required {
		if !files[name] {
			return false, "missing part: " + name
		}
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".xml") && !strings.HasSuffix(f.Name, ".rels") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return false, "open " + f.Name + ": " + err.Error()
		}
		var buf bytes.Buffer
		buf.ReadFrom(rc)
		rc.Close()
		if buf.Len() == 0 {
			return false, "empty part: " + f.Name
		}
		var v any
		if err := xml.Unmarshal(buf.Bytes(), &v); err != nil {
			return false, "malformed " + f.Name + ": " + err.Error()
		}
	}
	if ct, ok := files["[Content_Types].xml"]; !ok || !ct {
		return false, "missing [Content_Types].xml"
	}
	return true, "ok"
}

func TestRoundTripDOCX(t *testing.T) {
	data, err := CreateDOCXFromParagraphs([]DocxParagraph{
		{Text: "Hello & <world>", Size: 24, Bold: true, Align: "center"},
		{Text: "Second line", Size: 22},
	})
	if err != nil {
		t.Fatalf("CreateDOCXFromParagraphs: %v", err)
	}
	ok, detail := validatePackage(t, data, []string{
		"[Content_Types].xml", "_rels/.rels", "word/document.xml",
		"word/styles.xml", "docProps/core.xml", "docProps/app.xml",
	})
	if !ok {
		t.Fatalf("round-trip docx invalid: %s", detail)
	}
}

func TestRoundTripDOCXTable(t *testing.T) {
	data, err := CreateDOCXWithTable("Report", TableData{
		Headers: []string{"Name", "Value"},
		Rows:    [][]string{{"A", "1"}, {"B"}}, // ragged row must be padded
	})
	if err != nil {
		t.Fatalf("CreateDOCXWithTable: %v", err)
	}
	ok, detail := validatePackage(t, data, []string{
		"[Content_Types].xml", "word/document.xml", "docProps/core.xml",
	})
	if !ok {
		t.Fatalf("round-trip docx table invalid: %s", detail)
	}
}

func TestRoundTripXLSX(t *testing.T) {
	data, err := CreateXLSXFromData("Data", []string{"Name", "Active", "Score"},
		[][]string{{"A", "true", "9.5"}, {"B", "FALSE", "=SUM(C1:C1)"}})
	if err != nil {
		t.Fatalf("CreateXLSXFromData: %v", err)
	}
	ok, detail := validatePackage(t, data, []string{
		"[Content_Types].xml", "xl/workbook.xml", "xl/worksheets/sheet1.xml",
		"xl/styles.xml", "xl/sharedStrings.xml", "docProps/core.xml",
	})
	if !ok {
		t.Fatalf("round-trip xlsx invalid: %s", detail)
	}
	// booleans must use t="b", never a numeric <v>true</v>
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	for _, f := range zr.File {
		if f.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		rc, _ := f.Open()
		var buf bytes.Buffer
		buf.ReadFrom(rc)
		rc.Close()
		content := buf.String()
		if strings.Contains(content, "<v>true</v>") || strings.Contains(content, "<v>false</v>") {
			t.Error("boolean written as numeric value; want t=\"b\"")
		}
		if !strings.Contains(content, `t="b"`) {
			t.Error("sheet missing boolean cells with t=\"b\"")
		}
	}
}

func TestRoundTripPPTX(t *testing.T) {
	data, err := CreatePPTXFromSlides([]SlideData{
		{Title: "Slide 1", Content: "A & B\n<C>"},
		{Title: "Slide 2"},
	})
	if err != nil {
		t.Fatalf("CreatePPTXFromSlides: %v", err)
	}
	ok, detail := validatePackage(t, data, []string{
		"[Content_Types].xml", "ppt/presentation.xml", "ppt/slides/slide1.xml",
		"ppt/slides/slide2.xml", "docProps/core.xml", "docProps/app.xml",
	})
	if !ok {
		t.Fatalf("round-trip pptx invalid: %s", detail)
	}
}

// docxPart reads one part of a generated DOCX for content assertions.
func docxPart(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s: %v", name, err)
			}
			var buf bytes.Buffer
			buf.ReadFrom(rc)
			rc.Close()
			return buf.String()
		}
	}
	t.Fatalf("part not found: %s", name)
	return ""
}

func TestDocxCorporateStyles(t *testing.T) {
	data, err := CreateDOCXFromParagraphs([]DocxParagraph{
		{Text: "Title", Size: 28, Bold: true},
		{Text: "Body", Size: 22},
	})
	if err != nil {
		t.Fatalf("CreateDOCXFromParagraphs: %v", err)
	}
	ok, detail := validatePackage(t, data, []string{
		"[Content_Types].xml", "_rels/.rels", "word/document.xml",
		"word/styles.xml", "docProps/core.xml", "docProps/app.xml",
	})
	if !ok {
		t.Fatalf("round-trip docx invalid: %s", detail)
	}
	styles := docxPart(t, data, "word/styles.xml")
	for _, want := range []string{"Heading1", "Heading2", "Heading3", "Normal", "Quote", "1F4E79", "2E75B6"} {
		if !strings.Contains(styles, want) {
			t.Errorf("styles.xml missing %q", want)
		}
	}
}

func TestDocxCorporateTable(t *testing.T) {
	data, err := CreateDOCXWithTable("Report", TableData{
		Headers: []string{"Name", "Value"},
		Rows:    [][]string{{"A", "1"}, {"B"}}, // ragged row must be padded
	})
	if err != nil {
		t.Fatalf("CreateDOCXWithTable: %v", err)
	}
	ok, detail := validatePackage(t, data, []string{
		"[Content_Types].xml", "word/document.xml", "docProps/core.xml",
	})
	if !ok {
		t.Fatalf("round-trip docx table invalid: %s", detail)
	}
	doc := docxPart(t, data, "word/document.xml")
	if !strings.Contains(doc, "1F4E79") {
		t.Error("table header missing corporate blue fill 1F4E79")
	}
	if !strings.Contains(doc, "D9E2F3") {
		t.Error("table body missing banded fill D9E2F3")
	}
	// 1 header + 2 body rows, 2 columns each.
	if got := strings.Count(doc, "<w:tc>"); got != 6 {
		t.Errorf("table cells = %d, want 6 (ragged row padded)", got)
	}
	if !strings.Contains(doc, `w:w="12240"`) {
		t.Error("sectPr missing letter page size")
	}
}

func TestDocxCorporateReport(t *testing.T) {
	ragged := TableData{Headers: []string{"K", "V"}, Rows: [][]string{{"a", "b"}, {"only"}}}
	data, err := CreateCorporateReport("Annual Report", CorporateOptions{
		Subtitle: "FY2026", Date: "October 11, 2026",
		Header: "ACME", Footer: "Confidential", PageSize: "A4",
	}, []CorporateSection{
		{Heading: "Summary", Level: 1, Body: []string{"Growth & <gains>."}, Quote: "Stay the course."},
		{Heading: "Detail", Level: 2, Body: []string{"Numbers."}, Table: &ragged},
	})
	if err != nil {
		t.Fatalf("CreateCorporateReport: %v", err)
	}
	ok, detail := validatePackage(t, data, []string{
		"[Content_Types].xml", "_rels/.rels", "word/document.xml",
		"word/styles.xml", "word/header1.xml", "word/footer1.xml",
		"docProps/core.xml", "docProps/app.xml",
	})
	if !ok {
		t.Fatalf("round-trip corporate report invalid: %s", detail)
	}
	doc := docxPart(t, data, "word/document.xml")
	for _, want := range []string{"Annual Report", "FY2026", "Table of Contents", "TOC ", "Stay the course.", `w:w="11906"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("document.xml missing %q", want)
		}
	}
	footer := docxPart(t, data, "word/footer1.xml")
	if !strings.Contains(footer, "PAGE") {
		t.Error("footer missing PAGE numbering field")
	}
	header := docxPart(t, data, "word/header1.xml")
	if !strings.Contains(header, "ACME") {
		t.Error("header missing custom text")
	}
	// Letter is the default page size.
	data, err = CreateCorporateReport("", CorporateOptions{}, nil)
	if err != nil {
		t.Fatalf("CreateCorporateReport defaults: %v", err)
	}
	if ok, detail := validatePackage(t, data, []string{"word/document.xml"}); !ok {
		t.Fatalf("round-trip default corporate report invalid: %s", detail)
	}
	if doc := docxPart(t, data, "word/document.xml"); !strings.Contains(doc, `w:w="12240"`) {
		t.Error("default corporate report missing letter page size")
	}
}
// TestCorruptFiles ensures truncated or non-ZIP bytes never validate ok.
func TestCorruptFiles(t *testing.T) {
	valid := map[string][]byte{}
	if d, err := CreateDOCX("T"); err == nil {
		valid["docx"] = d
	}
	if d, err := CreateXLSX("S"); err == nil {
		valid["xlsx"] = d
	}
	if d, err := CreatePPTX("P"); err == nil {
		valid["pptx"] = d
	}
	for ext, data := range valid {
		truncated := data[:len(data)/2]
		if ok, _ := validatePackage(t, truncated, []string{"[Content_Types].xml"}); ok {
			t.Errorf("%s: truncated file validated ok, want invalid", ext)
		}
		if ok, _ := validatePackage(t, []byte("not a real "+ext), []string{"[Content_Types].xml"}); ok {
			t.Errorf("%s: garbage bytes validated ok, want invalid", ext)
		}
	}
}

// TestMalformedXMLDetected breaks one part of a valid DOCX and requires
// the round-trip check to fail.
func TestMalformedXMLDetected(t *testing.T) {
	data, err := CreateDOCX("T")
	if err != nil {
		t.Fatalf("CreateDOCX: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}
	parts := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		var buf bytes.Buffer
		buf.ReadFrom(rc)
		rc.Close()
		parts[f.Name] = buf.String()
	}
	parts["word/document.xml"] = `<?xml version="1.0" encoding="UTF-8"?><w:document><unclosed>`
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range parts {
		f, _ := w.Create(name)
		f.Write([]byte(content))
	}
	w.Close()
	if ok, _ := validatePackage(t, buf.Bytes(), []string{"word/document.xml"}); ok {
		t.Error("malformed document.xml validated ok, want invalid")
	}
}
