package ooxml

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

// repackWith returns base plus extra parts (extra wins on collision).
func repackWith(t *testing.T, base []byte, extra map[string]string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}
	parts := map[string][]byte{}
	var order []string
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		parts[f.Name] = data
		order = append(order, f.Name)
	}
	for name, content := range extra {
		if _, ok := parts[name]; !ok {
			order = append(order, name)
		}
		parts[name] = []byte(content)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range order {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := f.Write(parts[name]); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// partDigests maps part name to sha256 of its decompressed content.
func partDigests(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		sum := sha256.Sum256(content)
		out[f.Name] = hex.EncodeToString(sum[:])
	}
	return out
}

func partOrder(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}
	var order []string
	for _, f := range zr.File {
		order = append(order, f.Name)
	}
	return order
}

func threeParaFixture(t *testing.T) []byte {
	t.Helper()
	data, err := CreateDOCXFromParagraphs([]DocxParagraph{
		{Text: "Alpha", Size: 22},
		{Text: "Beta", Size: 22},
		{Text: "Gamma", Size: 22},
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return data
}

func richFixture(t *testing.T) []byte {
	t.Helper()
	return repackWith(t, threeParaFixture(t), map[string]string{
		"customXml/item1.xml":   `<item xmlns="urn:x"><v>keep me</v></item>`,
		"word/theme/theme1.xml": `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Tinted"><a:themeElements><a:fmtScheme name="F"/></a:themeElements></a:theme>`,
	})
}

func TestEditReplaceKeepsOtherParagraphs(t *testing.T) {
	src := threeParaFixture(t)
	before := partDigests(t, src)
	out, err := ReplaceParagraphText(src, 1, "Beta2")
	if err != nil {
		t.Fatalf("ReplaceParagraphText: %v", err)
	}
	for i, want := range []string{"Alpha", "Beta2", "Gamma"} {
		got, err := ParagraphText(out, i)
		if err != nil {
			t.Fatalf("ParagraphText %d: %v", i, err)
		}
		if got != want {
			t.Errorf("paragraph %d = %q, want %q", i, got, want)
		}
	}
	after := partDigests(t, out)
	for name, sum := range before {
		if name == "word/document.xml" {
			continue
		}
		if after[name] != sum {
			t.Errorf("part %s changed, want untouched", name)
		}
	}
	if beforeOrder, afterOrder := partOrder(t, src), partOrder(t, out); strings.Join(beforeOrder, ",") != strings.Join(afterOrder, ",") {
		t.Errorf("entry order changed: %v -> %v", beforeOrder, afterOrder)
	}
}

func TestEditUnknownPartSurvives(t *testing.T) {
	src := richFixture(t)
	out, err := ReplaceParagraphText(src, 0, "Alpha2")
	if err != nil {
		t.Fatalf("ReplaceParagraphText: %v", err)
	}
	if partDigests(t, src)["customXml/item1.xml"] != partDigests(t, out)["customXml/item1.xml"] {
		t.Error("customXml/item1.xml changed, want byte-for-byte survival")
	}
}

func TestEditPreservesStylesThemeRelsDocProps(t *testing.T) {
	src := richFixture(t)
	before := partDigests(t, src)
	out, err := ReplaceParagraphText(src, 2, "Gamma2")
	if err != nil {
		t.Fatalf("ReplaceParagraphText: %v", err)
	}
	out, err = InsertParagraphAfter(out, 0, "New", "Heading1")
	if err != nil {
		t.Fatalf("InsertParagraphAfter: %v", err)
	}
	out, err = SetParagraphStyle(out, 0, "Title")
	if err != nil {
		t.Fatalf("SetParagraphStyle: %v", err)
	}
	after := partDigests(t, out)
	for _, name := range []string{
		"word/styles.xml", "word/theme/theme1.xml", "customXml/item1.xml",
		"word/_rels/document.xml.rels", "_rels/.rels",
		"docProps/core.xml", "docProps/app.xml", "[Content_Types].xml",
	} {
		if after[name] != before[name] {
			t.Errorf("part %s changed, want untouched", name)
		}
	}
}

func TestEditInsertParagraphAfter(t *testing.T) {
	src := threeParaFixture(t)
	out, err := InsertParagraphAfter(src, 0, "Inserted", "")
	if err != nil {
		t.Fatalf("InsertParagraphAfter: %v", err)
	}
	for i, want := range []string{"Alpha", "Inserted", "Beta", "Gamma"} {
		got, err := ParagraphText(out, i)
		if err != nil {
			t.Fatalf("ParagraphText %d: %v", i, err)
		}
		if got != want {
			t.Errorf("paragraph %d = %q, want %q", i, got, want)
		}
	}
	if _, err := ParagraphText(out, 4); err == nil {
		t.Error("expected out-of-range error for paragraph 4")
	}
}

func TestEditSetParagraphStyleOnly(t *testing.T) {
	src := threeParaFixture(t)
	out, err := SetParagraphStyle(src, 1, "Heading1")
	if err != nil {
		t.Fatalf("SetParagraphStyle: %v", err)
	}
	doc := ""
	zr, _ := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			raw, _ := io.ReadAll(rc)
			rc.Close()
			doc = string(raw)
		}
	}
	paras := paraRe.FindAllString(doc, -1)
	if len(paras) != 3 {
		t.Fatalf("paragraphs = %d, want 3", len(paras))
	}
	if !strings.Contains(paras[1], `<w:pStyle w:val="Heading1"/>`) {
		t.Errorf("paragraph 1 missing new pStyle: %s", paras[1])
	}
	for _, i := range []int{0, 2} {
		if strings.Contains(paras[i], "w:pStyle") {
			t.Errorf("paragraph %d gained a pStyle, want untouched", i)
		}
	}
	if got, _ := ParagraphText(out, 1); got != "Beta" {
		t.Errorf("text changed to %q, want Beta", got)
	}
}

func xlsxFixture(t *testing.T) []byte {
	t.Helper()
	data, err := CreateXLSXFromData("Data",
		[]string{"Name", "Score"},
		[][]string{{"Ann", "9"}, {"Bob", "7"}})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return data
}

// twoSheetFixture clones sheet1 into a second sheet wired through the
// workbook, rels and content types.
func twoSheetFixture(t *testing.T, base []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}
	parts := map[string][]byte{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		parts[f.Name] = data
	}
	sheet1 := string(parts["xl/worksheets/sheet1.xml"])
	parts["xl/worksheets/sheet2.xml"] = []byte(strings.Replace(sheet1, "<v>9</v>", "<v>5</v>", 1))
	wb := string(parts["xl/workbook.xml"])
	wb = strings.Replace(wb, "</sheets>", `<sheet name="Second" sheetId="2" r:id="rId4" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"/></sheets>`, 1)
	parts["xl/workbook.xml"] = []byte(wb)
	rels := string(parts["xl/_rels/workbook.xml.rels"])
	rels = strings.Replace(rels, "</Relationships>", `<Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/></Relationships>`, 1)
	parts["xl/_rels/workbook.xml.rels"] = []byte(rels)
	ct := string(parts["[Content_Types].xml"])
	ct = strings.Replace(ct, "</Types>", `<Override PartName="/xl/worksheets/sheet2.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`, 1)
	parts["[Content_Types].xml"] = []byte(ct)
	extra := map[string]string{}
	for k, v := range parts {
		extra[k] = string(v)
	}
	return repackWith(t, base, extra)
}

func sheetXML(t *testing.T, data []byte, part string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == part {
			rc, _ := f.Open()
			raw, _ := io.ReadAll(rc)
			rc.Close()
			return string(raw)
		}
	}
	t.Fatalf("part not found: %s", part)
	return ""
}

func TestEditSetCellValue(t *testing.T) {
	src := twoSheetFixture(t, xlsxFixture(t))
	before := partDigests(t, src)
	out, err := SetCellValue(src, "Data", "B3", "10", false, "")
	if err != nil {
		t.Fatalf("SetCellValue: %v", err)
	}
	sheet := sheetXML(t, out, "xl/worksheets/sheet1.xml")
	if !strings.Contains(sheet, `<c r="B3" t="str"><v>10</v></c>`) {
		t.Errorf("B3 not set as inline str: %s", sheet)
	}
	if !strings.Contains(sheet, "Ann") && !strings.Contains(sheet, ">9<") {
		t.Errorf("other cells disturbed: %s", sheet)
	}
	after := partDigests(t, out)
	for name, sum := range before {
		if name == "xl/worksheets/sheet1.xml" {
			continue
		}
		if after[name] != sum {
			t.Errorf("part %s changed, want untouched", name)
		}
	}
}

func TestEditSetCellFormula(t *testing.T) {
	src := xlsxFixture(t)
	out, err := SetCellValue(src, "Data", "C1", "=SUM(B2:B3)", true, "")
	if err != nil {
		t.Fatalf("SetCellValue formula: %v", err)
	}
	sheet := sheetXML(t, out, "xl/worksheets/sheet1.xml")
	if !strings.Contains(sheet, `<c r="C1"><f>SUM(B2:B3)</f></c>`) {
		t.Errorf("formula cell wrong (want no cached <v>0</v>): %s", sheet)
	}
	if strings.Contains(sheet, `<f>SUM(B2:B3)</f><v>`) {
		t.Errorf("formula cell must not carry a cached <v>: %s", sheet)
	}
	if wb := sheetXML(t, out, "xl/workbook.xml"); !strings.Contains(wb, "<calcPr") || !strings.Contains(wb, `fullCalcOnLoad="1"`) {
		t.Errorf("workbook.xml missing calcPr fullCalcOnLoad: %s", wb)
	}
	// sharedStrings must be untouched: plain value cells use t="str".
	if before, after := partDigests(t, src)["xl/sharedStrings.xml"], partDigests(t, out)["xl/sharedStrings.xml"]; before != after {
		t.Error("sharedStrings.xml changed, must stay untouched")
	}
}

func TestEditSetCellNumberFormat(t *testing.T) {
	src := xlsxFixture(t)
	out, err := SetCellValue(src, "Data", "B2", "9.5", false, "0.00")
	if err != nil {
		t.Fatalf("SetCellValue number format: %v", err)
	}
	sheet := sheetXML(t, out, "xl/worksheets/sheet1.xml")
	if !strings.Contains(sheet, `r="B2" t="str" s="`) {
		t.Errorf("B2 missing style index: %s", sheet)
	}
	styles := sheetXML(t, out, "xl/styles.xml")
	if !strings.Contains(styles, `formatCode="0.00"`) {
		t.Errorf("styles.xml missing new numFmt: %s", styles)
	}
	// second call with the same format must reuse the numFmt, not duplicate it.
	out2, err := SetCellValue(out, "Data", "B3", "7.5", false, "0.00")
	if err != nil {
		t.Fatalf("SetCellValue reuse format: %v", err)
	}
	if got := strings.Count(sheetXML(t, out2, "xl/styles.xml"), `formatCode="0.00"`); got != 1 {
		t.Errorf("numFmt duplicated %d times, want 1", got)
	}
}

func TestEditCellErrors(t *testing.T) {
	src := xlsxFixture(t)
	for _, cell := range []string{"", "A", "1", "A0", "AAAA1", "B-2"} {
		if _, err := SetCellValue(src, "Data", cell, "x", false, ""); err == nil {
			t.Errorf("cell %q: expected error", cell)
		}
	}
	if _, err := SetCellValue(src, "Nope", "A1", "x", false, ""); err == nil {
		t.Error("missing sheet: expected error")
	} else if !strings.Contains(err.Error(), "Nope") {
		t.Errorf("missing sheet error must name the sheet: %v", err)
	}
}

func TestEditParagraphOutOfRange(t *testing.T) {
	src := threeParaFixture(t)
	if _, err := ReplaceParagraphText(src, 9, "x"); err == nil {
		t.Error("replace out of range: expected error")
	}
	if _, err := InsertParagraphAfter(src, 9, "x", ""); err == nil {
		t.Error("insert out of range: expected error")
	}
	if _, err := SetParagraphStyle(src, -1, "Title"); err == nil {
		t.Error("style negative index: expected error")
	}
	if _, err := ParagraphText(src, 3); err == nil {
		t.Error("text out of range: expected error")
	}
}

func TestEditMissingDocument(t *testing.T) {
	src := repackWith(t, threeParaFixture(t), nil)
	zr, _ := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	parts := map[string]string{}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			continue
		}
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		parts[f.Name] = string(data)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range parts {
		f, _ := w.Create(name)
		f.Write([]byte(content))
	}
	w.Close()
	stripped := buf.Bytes()
	for _, fn := range []func([]byte, int, string) ([]byte, error){
		func(b []byte, i int, s string) ([]byte, error) { return ReplaceParagraphText(b, i, s) },
		func(b []byte, i int, s string) ([]byte, error) { return InsertParagraphAfter(b, i, s, "") },
		func(b []byte, i int, s string) ([]byte, error) { return SetParagraphStyle(b, i, s) },
	} {
		if _, err := fn(stripped, 0, "x"); err == nil {
			t.Error("missing document.xml: expected error")
		} else if !strings.Contains(err.Error(), "word/document.xml") {
			t.Errorf("error must name the missing part: %v", err)
		}
	}
	if _, err := ParagraphText(stripped, 0); err == nil {
		t.Error("missing document.xml text: expected error")
	}
}

func TestEditTruncatedZip(t *testing.T) {
	src := threeParaFixture(t)
	truncated := src[:len(src)/2]
	for name, fn := range map[string]func() error{
		"replace": func() error { _, err := ReplaceParagraphText(truncated, 0, "x"); return err },
		"insert":  func() error { _, err := InsertParagraphAfter(truncated, 0, "x", ""); return err },
		"style":   func() error { _, err := SetParagraphStyle(truncated, 0, "T"); return err },
		"text":    func() error { _, err := ParagraphText(truncated, 0); return err },
		"cell":    func() error { _, err := SetCellValue(truncated, "S", "A1", "x", false, ""); return err },
	} {
		if err := fn(); err == nil {
			t.Errorf("%s truncated: expected error", name)
		}
	}
	if _, err := SetCellValue([]byte("garbage"), "S", "A1", "x", false, ""); err == nil {
		t.Error("garbage bytes: expected error")
	}
}

func TestEditSpecialCharsRoundTrip(t *testing.T) {
	src := threeParaFixture(t)
	want := `a < b & "q" 's' > c — ünïcodé ✓`
	out, err := ReplaceParagraphText(src, 1, want)
	if err != nil {
		t.Fatalf("ReplaceParagraphText: %v", err)
	}
	got, err := ParagraphText(out, 1)
	if err != nil {
		t.Fatalf("ParagraphText: %v", err)
	}
	if got != want {
		t.Errorf("round-trip = %q, want %q", got, want)
	}
	// style value with XML metacharacters must not break the part.
	out, err = SetParagraphStyle(src, 0, `A&B"<>`)
	if err != nil {
		t.Fatalf("SetParagraphStyle special: %v", err)
	}
	if ok, detail := validatePackage(t, out, []string{"word/document.xml"}); !ok {
		t.Fatalf("special style broke package: %s", detail)
	}
}

func TestEditTableParagraphCounting(t *testing.T) {
	// Documented model: w:p counted in document order, table cells included.
	src, err := CreateDOCXWithTable("Report", TableData{
		Headers: []string{"H1", "H2"},
		Rows:    [][]string{{"a", "b"}, {"c", "d"}},
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	for i, want := range []string{"Report", "H1", "H2", "a", "b", "c", "d"} {
		got, err := ParagraphText(src, i)
		if err != nil {
			t.Fatalf("ParagraphText %d: %v", i, err)
		}
		if got != want {
			t.Errorf("paragraph %d = %q, want %q", i, got, want)
		}
	}
	out, err := ReplaceParagraphText(src, 4, "B2")
	if err != nil {
		t.Fatalf("ReplaceParagraphText table cell: %v", err)
	}
	if got, _ := ParagraphText(out, 4); got != "B2" {
		t.Errorf("table cell = %q, want B2", got)
	}
	if got, _ := ParagraphText(out, 3); got != "a" {
		t.Errorf("neighbor cell = %q, want a", got)
	}
}

func TestEditMultiRunParagraph(t *testing.T) {
	src := threeParaFixture(t)
	zr, _ := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	parts := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		parts[f.Name] = string(data)
	}
	doc := parts["word/document.xml"]
	paras := paraRe.FindAllStringIndex(doc, -1)
	multi := `<w:p><w:pPr></w:pPr>` +
		`<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">Be</w:t></w:r>` +
		`<w:r><w:t xml:space="preserve">ta</w:t></w:r>` +
		`<w:r><w:br w:type="page"/></w:r></w:p>`
	doc = doc[:paras[1][0]] + multi + doc[paras[1][1]:]
	parts["word/document.xml"] = doc
	multiSrc := repackWith(t, src, parts)

	out, err := ReplaceParagraphText(multiSrc, 1, "New")
	if err != nil {
		t.Fatalf("ReplaceParagraphText multi-run: %v", err)
	}
	got, err := ParagraphText(out, 1)
	if err != nil {
		t.Fatalf("ParagraphText: %v", err)
	}
	if got != "New" {
		t.Errorf("multi-run text = %q, want New", got)
	}
	raw := sheetOrDoc(t, out)
	if strings.Count(raw, "<w:t") != 3 {
		// Alpha + New + Gamma each hold exactly one w:t.
		t.Errorf("want 3 w:t total after merge, got: %d", strings.Count(raw, "<w:t"))
	}
	if !strings.Contains(raw, "<w:b/>") {
		t.Error("first run rPr (bold) lost")
	}
	if !strings.Contains(raw, `<w:br w:type="page"/>`) {
		t.Error("non-text page-break run lost")
	}
}

func sheetOrDoc(t *testing.T, data []byte) string {
	t.Helper()
	return sheetXML(t, data, "word/document.xml")
}

func TestDigest(t *testing.T) {
	src := threeParaFixture(t)
	d1 := Digest(src)
	if len(d1) != 16 {
		t.Errorf("digest len = %d, want 16", len(d1))
	}
	out, err := ReplaceParagraphText(src, 0, "changed")
	if err != nil {
		t.Fatalf("ReplaceParagraphText: %v", err)
	}
	if d2 := Digest(out); d2 == d1 {
		t.Error("digest unchanged after edit")
	}
	if d3 := Digest(src); d3 != d1 {
		t.Error("digest not stable")
	}
}
