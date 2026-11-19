package ooxml

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

// selfClosingFixture inserts an empty self-closing paragraph between
// Alpha and Beta of the three-paragraph fixture.
func selfClosingFixture(t *testing.T) []byte {
	t.Helper()
	src := threeParaFixture(t)
	parts := unzipParts(t, src)
	doc := string(parts["word/document.xml"])
	paras := paraRe.FindAllStringIndex(doc, -1)
	if len(paras) != 3 {
		t.Fatalf("setup: paragraphs = %d, want 3", len(paras))
	}
	blank := `<w:p w:rsidR="00ABCDEF"/>`
	doc = doc[:paras[0][1]] + blank + doc[paras[0][1]:]
	parts["word/document.xml"] = []byte(doc)
	var order []string
	zr, _ := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	for _, f := range zr.File {
		order = append(order, f.Name)
	}
	return buildZip(t, parts, order)
}

func TestEditSelfClosingParagraphCount(t *testing.T) {
	src := selfClosingFixture(t)
	for i, want := range []string{"Alpha", "", "Beta", "Gamma"} {
		got, err := ParagraphText(src, i)
		if err != nil {
			t.Fatalf("ParagraphText %d: %v", i, err)
		}
		if got != want {
			t.Errorf("paragraph %d = %q, want %q", i, got, want)
		}
	}
}

func TestEditSelfClosingParagraphReplace(t *testing.T) {
	src := selfClosingFixture(t)
	out, err := ReplaceParagraphText(src, 1, "Blank")
	if err != nil {
		t.Fatalf("ReplaceParagraphText self-closing: %v", err)
	}
	if got, _ := ParagraphText(out, 1); got != "Blank" {
		t.Errorf("paragraph 1 = %q, want Blank", got)
	}
	for i, want := range []string{"Alpha", "Blank", "Beta", "Gamma"} {
		got, _ := ParagraphText(out, i)
		if got != want {
			t.Errorf("after replace paragraph %d = %q, want %q", i, got, want)
		}
	}
	raw := sheetOrDoc(t, out)
	paras := paraRe.FindAllString(raw, -1)
	if len(paras) != 4 {
		t.Fatalf("paragraphs = %d, want 4", len(paras))
	}
	if !strings.Contains(paras[1], `w:rsidR="00ABCDEF"`) {
		t.Errorf("replaced self-closing lost attributes: %s", paras[1])
	}
	if strings.HasSuffix(strings.TrimSpace(paras[1]), "/>") {
		t.Errorf("replaced self-closing still self-closing: %s", paras[1])
	}
}

func TestEditSelfClosingParagraphInsertStyle(t *testing.T) {
	src := selfClosingFixture(t)
	out, err := InsertParagraphAfter(src, 1, "After", "")
	if err != nil {
		t.Fatalf("InsertParagraphAfter self-closing: %v", err)
	}
	for i, want := range []string{"Alpha", "", "After", "Beta", "Gamma"} {
		got, _ := ParagraphText(out, i)
		if got != want {
			t.Errorf("paragraph %d = %q, want %q", i, got, want)
		}
	}
	out, err = SetParagraphStyle(src, 1, "Heading1")
	if err != nil {
		t.Fatalf("SetParagraphStyle self-closing: %v", err)
	}
	raw := sheetOrDoc(t, out)
	paras := paraRe.FindAllString(raw, -1)
	if !strings.Contains(paras[1], `w:pStyle w:val="Heading1"`) {
		t.Errorf("self-closing missing new pStyle: %s", paras[1])
	}
	if strings.HasSuffix(strings.TrimSpace(paras[1]), "/>") {
		t.Errorf("styled self-closing still self-closing: %s", paras[1])
	}
	if got, _ := ParagraphText(out, 1); got != "" {
		t.Errorf("styled text = %q, want empty", got)
	}
}

// textboxFixture appends a paragraph containing w:txbxContent with a
// nested w:p after the three text paragraphs.
func textboxFixture(t *testing.T) []byte {
	t.Helper()
	src := threeParaFixture(t)
	parts := unzipParts(t, src)
	doc := string(parts["word/document.xml"])
	tb := `<w:p><w:r><w:txbxContent><w:p><w:r><w:t>inner</w:t></w:r></w:p></w:txbxContent></w:r></w:p>`
	doc = strings.Replace(doc, "</w:body>", tb+"</w:body>", 1)
	parts["word/document.xml"] = []byte(doc)
	var order []string
	zr, _ := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	for _, f := range zr.File {
		order = append(order, f.Name)
	}
	return buildZip(t, parts, order)
}

func TestEditNestedParagraphError(t *testing.T) {
	src := textboxFixture(t)
	// The text-box paragraph is the last regex match; its match is
	// truncated at the inner </w:p> and must fail closed.
	n := len(paraRe.FindAllString(string(unzipParts(t, src)["word/document.xml"]), -1))
	idx := n - 1
	for name, fn := range map[string]func() error{
		"text":    func() error { _, err := ParagraphText(src, idx); return err },
		"replace": func() error { _, err := ReplaceParagraphText(src, idx, "x"); return err },
		"insert":  func() error { _, err := InsertParagraphAfter(src, idx, "x", ""); return err },
		"style":   func() error { _, err := SetParagraphStyle(src, idx, "Title"); return err },
	} {
		err := fn()
		if err == nil {
			t.Errorf("%s: want ErrNestedParagraph, got nil", name)
			continue
		}
		if !errors.Is(err, ErrNestedParagraph) {
			t.Errorf("%s: error %v does not wrap ErrNestedParagraph", name, err)
		}
		if !strings.Contains(err.Error(), strconv.Itoa(idx)) {
			t.Errorf("%s: error %v must name index %d", name, err, idx)
		}
	}
}

func TestEditSetCellValueKeepsStyle(t *testing.T) {
	src := xlsxFixture(t)
	// A1 is a header cell with s="1"; a value change must keep it.
	out, err := SetCellValue(src, "Data", "A1", "New", false, "")
	if err != nil {
		t.Fatalf("SetCellValue: %v", err)
	}
	sheet := sheetXML(t, out, "xl/worksheets/sheet1.xml")
	loc := strings.Index(sheet, `r="A1"`)
	if loc < 0 {
		t.Fatalf("A1 missing: %s", sheet)
	}
	end := strings.Index(sheet[loc:], ">")
	tag := sheet[loc : loc+end]
	if !strings.Contains(tag, `s="1"`) {
		t.Errorf("styled cell lost s=\"1\": %s", tag)
	}
	// cm/vm survive when present.
	parts := unzipParts(t, src)
	sheetSrc := string(parts["xl/worksheets/sheet1.xml"])
	old := `<c r="B2"><v>9</v></c>`
	if !strings.Contains(sheetSrc, old) {
		t.Fatalf("setup: B2 shape changed: %s", sheetSrc)
	}
	parts["xl/worksheets/sheet1.xml"] = []byte(strings.Replace(sheetSrc, old, `<c r="B2" s="2" cm="1" vm="2"><v>9</v></c>`, 1))
	var order []string
	zr, _ := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	for _, f := range zr.File {
		order = append(order, f.Name)
	}
	src2 := buildZip(t, parts, order)
	out2, err := SetCellValue(src2, "Data", "B2", "10", false, "")
	if err != nil {
		t.Fatalf("SetCellValue cm/vm: %v", err)
	}
	sheet2 := sheetXML(t, out2, "xl/worksheets/sheet1.xml")
	for _, want := range []string{`r="B2"`, `s="2"`, `cm="1"`, `vm="2"`} {
		if !strings.Contains(sheet2, want) {
			t.Errorf("B2 missing %s: %s", want, sheet2)
		}
	}
	// An explicit number format overrides s but keeps cm/vm.
	out3, err := SetCellValue(src2, "Data", "B2", "10", false, "0.00")
	if err != nil {
		t.Fatalf("SetCellValue override: %v", err)
	}
	sheet3 := sheetXML(t, out3, "xl/worksheets/sheet1.xml")
	if strings.Contains(sheet3, `r="B2" t="str" s="2"`) {
		t.Errorf("B2 s not overridden: %s", sheet3)
	}
	for _, want := range []string{`cm="1"`, `vm="2"`} {
		if !strings.Contains(sheet3, want) {
			t.Errorf("B2 override lost %s: %s", want, sheet3)
		}
	}
}

func TestEditSetCellFormulaNoCache(t *testing.T) {
	src := xlsxFixture(t)
	out, err := SetCellValue(src, "Data", "C5", "=SUM(B2:B3)", true, "")
	if err != nil {
		t.Fatalf("SetCellValue formula: %v", err)
	}
	sheet := sheetXML(t, out, "xl/worksheets/sheet1.xml")
	if !strings.Contains(sheet, `<f>SUM(B2:B3)</f>`) {
		t.Errorf("formula missing: %s", sheet)
	}
	if strings.Contains(sheet, `<f>SUM(B2:B3)</f><v>`) {
		t.Errorf("formula must not carry cached <v>: %s", sheet)
	}
	wb := sheetXML(t, out, "xl/workbook.xml")
	if !strings.Contains(wb, `<calcPr calcId="0" fullCalcOnLoad="1"/>`) {
		t.Errorf("workbook missing calcPr: %s", wb)
	}
	// Non-formula edits must not touch the workbook.
	out2, err := SetCellValue(src, "Data", "C5", "plain", false, "")
	if err != nil {
		t.Fatalf("SetCellValue plain: %v", err)
	}
	if a, b := partDigests(t, src)["xl/workbook.xml"], partDigests(t, out2)["xl/workbook.xml"]; a != b {
		t.Error("plain value edit must not touch xl/workbook.xml")
	}
}

// sparseRowFixture builds a sheet with only rows 1 and 5 and cells
// A1/C1 in row 1, for ordered-insertion tests.
func sparseRowFixture(t *testing.T) []byte {
	t.Helper()
	src := xlsxFixture(t)
	parts := unzipParts(t, src)
	parts["xl/worksheets/sheet1.xml"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		`<sheetData><row r="1"><c r="A1" t="str"><v>1</v></c><c r="C1" t="str"><v>3</v></c></row>` +
		`<row r="5"><c r="A5" t="str"><v>5</v></c></row></sheetData></worksheet>`)
	var order []string
	zr, _ := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	for _, f := range zr.File {
		order = append(order, f.Name)
	}
	return buildZip(t, parts, order)
}

func TestEditRowCellOrderedInsert(t *testing.T) {
	src := sparseRowFixture(t)
	out, err := SetCellValue(src, "Data", "A3", "mid", false, "")
	if err != nil {
		t.Fatalf("SetCellValue row insert: %v", err)
	}
	sheet := sheetXML(t, out, "xl/worksheets/sheet1.xml")
	i1, i3, i5 := strings.Index(sheet, `r="1"`), strings.Index(sheet, `r="3"`), strings.Index(sheet, `r="5"`)
	if i1 < 0 || i3 < 0 || i5 < 0 {
		t.Fatalf("rows missing: %s", sheet)
	}
	if !(i1 < i3 && i3 < i5) {
		t.Errorf("rows out of order: 1@%d 3@%d 5@%d: %s", i1, i3, i5, sheet)
	}
	out, err = SetCellValue(src, "Data", "B1", "2", false, "")
	if err != nil {
		t.Fatalf("SetCellValue cell insert: %v", err)
	}
	sheet = sheetXML(t, out, "xl/worksheets/sheet1.xml")
	a, b, c := strings.Index(sheet, `r="A1"`), strings.Index(sheet, `r="B1"`), strings.Index(sheet, `r="C1"`)
	if a < 0 || b < 0 || c < 0 {
		t.Fatalf("cells missing: %s", sheet)
	}
	if !(a < b && b < c) {
		t.Errorf("cells out of order: A1@%d B1@%d C1@%d: %s", a, b, c, sheet)
	}
}

func TestEditSelfClosingSheetData(t *testing.T) {
	src := xlsxFixture(t)
	parts := unzipParts(t, src)
	parts["xl/worksheets/sheet1.xml"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData/></worksheet>`)
	var order []string
	zr, _ := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	for _, f := range zr.File {
		order = append(order, f.Name)
	}
	src2 := buildZip(t, parts, order)
	out, err := SetCellValue(src2, "Data", "B2", "x", false, "")
	if err != nil {
		t.Fatalf("SetCellValue self-closing sheetData: %v", err)
	}
	sheet := sheetXML(t, out, "xl/worksheets/sheet1.xml")
	if !strings.Contains(sheet, `<row r="2">`) || !strings.Contains(sheet, `r="B2"`) {
		t.Errorf("cell not created in expanded sheetData: %s", sheet)
	}
	if ok, detail := validatePackage(t, out, []string{"xl/worksheets/sheet1.xml"}); !ok {
		t.Fatalf("expanded package invalid: %s", detail)
	}
}

func TestEditHeaderPreservation(t *testing.T) {
	src := threeParaFixture(t)
	fixed := time.Date(2020, 1, 2, 3, 4, 4, 0, time.UTC)
	zr, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range zr.File {
		fh := f.FileHeader
		fh.Modified = fixed
		fh.Extra = []byte("XXextraYY")
		fh.Comment = "keepme"
		fw, err := w.CreateHeader(&fh)
		if err != nil {
			t.Fatal(err)
		}
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		_, _ = fw.Write(data)
	}
	_ = w.Close()
	src2 := buf.Bytes()
	out, err := ReplaceParagraphText(src2, 0, "Changed")
	if err != nil {
		t.Fatalf("ReplaceParagraphText: %v", err)
	}
	zr2, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr2.File {
		if f.Name == "word/document.xml" {
			continue
		}
		if !f.Modified.Equal(fixed) {
			t.Errorf("part %s Modified = %v, want %v", f.Name, f.Modified, fixed)
		}
		if !strings.Contains(string(f.Extra), "XXextraYY") {
			t.Errorf("part %s Extra = %q, want it preserved", f.Name, string(f.Extra))
		}
		if f.Comment != "keepme" {
			t.Errorf("part %s Comment = %q, want keepme", f.Name, f.Comment)
		}
	}
}

func TestEditBenchStyles(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0.00"/></numFmts><cellXfs count="2000">`)
	for i := 0; i < 2000; i++ {
		b.WriteString(`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>`)
	}
	b.WriteString(`</cellXfs></styleSheet>`)
	styles := []byte(b.String())
	start := time.Now()
	idx, out, err := ensureNumberFormat(styles, "0.00")
	if err != nil {
		t.Fatalf("ensureNumberFormat: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("ensureNumberFormat took %v, want under 2s", d)
	}
	if idx != 2000 {
		t.Errorf("index = %d, want 2000", idx)
	}
	if !strings.Contains(out, `formatCode="0.00"`) {
		t.Error("new numFmt missing")
	}
	if got := strings.Count(out, "<xf "); got != 2001 {
		t.Errorf("xf count = %d, want 2001", got)
	}
}

func TestEditCellRefBoundaries(t *testing.T) {
	src := xlsxFixture(t)
	for _, cell := range []string{"A1", "XFD1", "A1048576", "XFD1048576", "a1", " b2 "} {
		if _, err := SetCellValue(src, "Data", cell, "x", false, ""); err != nil {
			t.Errorf("cell %q: want ok, got %v", cell, err)
		}
	}
	for _, cell := range []string{"ZZZ1", "XFE1", "A0", "A1048577", "A2000000", "XFD1048577", "AAAA1", ""} {
		if _, err := SetCellValue(src, "Data", cell, "x", false, ""); err == nil {
			t.Errorf("cell %q: want error, got nil", cell)
		}
	}
}

func stylesWithNumFmts(numFmtsBlock, cellXfsBlock string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		numFmtsBlock + cellXfsBlock + `</styleSheet>`
}

func TestEditNumFmtsNoCount(t *testing.T) {
	styles := stylesWithNumFmts(
		`<numFmts><numFmt numFmtId="164" formatCode="#,##0.00"/></numFmts>`,
		`<cellXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/></cellXfs>`)
	idx, out, err := ensureNumberFormat([]byte(styles), "0.000")
	if err != nil {
		t.Fatalf("ensureNumberFormat no-count: %v", err)
	}
	if !strings.Contains(out, `count="2"`) {
		t.Errorf("numFmts count not fixed: %s", out)
	}
	if strings.Count(out, "<numFmts") != 1 {
		t.Errorf("numFmts block duplicated: %s", out)
	}
	if ok := wellFormed([]byte(out)); ok != nil {
		t.Errorf("malformed styles: %v", ok)
	}
	_ = idx
}

func TestEditSelfClosingNumFmtsCellXfs(t *testing.T) {
	styles := stylesWithNumFmts(`<numFmts/>`, `<cellXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/></cellXfs>`)
	_, out, err := ensureNumberFormat([]byte(styles), "0.00")
	if err != nil {
		t.Fatalf("self-closing numFmts: %v", err)
	}
	if strings.Count(out, "<numFmts") != 1 {
		t.Errorf("numFmts duplicated: %s", out)
	}
	if !strings.Contains(out, `formatCode="0.00"`) {
		t.Errorf("new numFmt missing: %s", out)
	}
	if ok := wellFormed([]byte(out)); ok != nil {
		t.Errorf("malformed: %v", ok)
	}
	styles2 := stylesWithNumFmts(
		`<numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0.00"/></numFmts>`,
		`<cellXfs/>`)
	idx, out2, err := ensureNumberFormat([]byte(styles2), "#,##0.00")
	if err != nil {
		t.Fatalf("self-closing cellXfs: %v", err)
	}
	if idx != 0 {
		t.Errorf("index = %d, want 0", idx)
	}
	if strings.Count(out2, "<cellXfs") != 1 {
		t.Errorf("cellXfs duplicated: %s", out2)
	}
	if !strings.Contains(out2, `count="1"`) {
		t.Errorf("cellXfs count missing: %s", out2)
	}
	if ok := wellFormed([]byte(out2)); ok != nil {
		t.Errorf("malformed: %v", ok)
	}
}
