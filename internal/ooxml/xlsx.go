package ooxml

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

type cellType int

const (
	cellString cellType = iota
	cellNumber
	cellFormula
)

func cellTypeOf(val string) (cellType, string) {
	if strings.HasPrefix(val, "=") {
		return cellFormula, val[1:]
	}
	if _, err := strconv.ParseFloat(val, 64); err == nil {
		return cellNumber, val
	}
	if val == "true" || val == "false" || val == "TRUE" || val == "FALSE" {
		return cellNumber, val
	}
	return cellString, val
}

func CreateXLSX(title string) ([]byte, error) {
	return CreateXLSXFromData(title, nil, nil)
}

func CreateXLSXFromData(title string, headers []string, rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	sharedStrings := collectSharedStrings(headers, rows)
	sheetXML := buildSheetXML(headers, rows, sharedStrings)
	sharedStringsXML := buildSharedStringsXML(sharedStrings)

	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
  <Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
  <Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/>
</Relationships>`,
		"xl/workbook.xml": fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheets>
    <sheet name="%s" sheetId="1" r:id="rId1" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"/>
  </sheets>
</workbook>`, xmlEscape(title)),
		"xl/worksheets/sheet1.xml": sheetXML,
		"xl/styles.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <numFmts count="1">
    <numFmt numFmtId="164" formatCode="#,##0.00"/>
  </numFmts>
  <fonts count="2">
    <font><sz val="11"/><name val="Calibri"/></font>
    <font><b/><sz val="11"/><name val="Calibri"/></font>
  </fonts>
  <fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>
  <borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>
  <cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
  <cellXfs count="3">
    <xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
    <xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>
    <xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
  </cellXfs>
</styleSheet>`,
		"xl/sharedStrings.xml": sharedStringsXML,
	}

	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}

	return buf.Bytes(), nil
}

func collectSharedStrings(headers []string, rows [][]string) []string {
	seen := map[string]int{}
	var list []string
	add := func(s string) {
		if _, ok := seen[s]; !ok && !isNumeric(s) && !strings.HasPrefix(s, "=") {
			seen[s] = len(list)
			list = append(list, s)
		}
	}
	for _, h := range headers {
		add(h)
	}
	for _, row := range rows {
		for _, cell := range row {
			add(cell)
		}
	}
	return list
}

func isNumeric(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil || s == "true" || s == "false" || s == "TRUE" || s == "FALSE"
}

func buildSheetXML(headers []string, rows [][]string, sharedStrings []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
`)
	ssIndex := func(s string) int {
		for i, v := range sharedStrings {
			if v == s {
				return i
			}
		}
		return 0
	}

	writeCell := func(ref, val string, isHeader bool) {
		if val == "" {
			return
		}
		style := ""
		if isHeader {
			style = ` s="1"`
		}
		if strings.HasPrefix(val, "=") {
			formula := val[1:]
			if isHeader {
				b.WriteString(fmt.Sprintf("      <c r=\"%s\"%s><f>%s</f></c>\n", ref, style, xmlEscape(formula)))
			} else {
				b.WriteString(fmt.Sprintf("      <c r=\"%s\"><f>%s</f></c>\n", ref, xmlEscape(formula)))
			}
		} else if isNumeric(val) {
			if !isHeader {
				b.WriteString(fmt.Sprintf("      <c r=\"%s\" s=\"2\"><v>%s</v></c>\n", ref, val))
			} else {
				b.WriteString(fmt.Sprintf("      <c r=\"%s\" t=\"s\"%s><v>%d</v></c>\n", ref, style, ssIndex(val)))
			}
		} else {
			si := ssIndex(val)
			b.WriteString(fmt.Sprintf("      <c r=\"%s\" t=\"s\"%s><v>%d</v></c>\n", ref, style, si))
		}
	}

	writeRow := func(rowIdx int, cells []string, isHeader bool) {
		b.WriteString(fmt.Sprintf("    <row r=\"%d\">\n", rowIdx))
		for ci, val := range cells {
			ref := fmt.Sprintf("%s%d", colName(ci), rowIdx)
			writeCell(ref, val, isHeader)
		}
		b.WriteString("    </row>\n")
	}

	if len(headers) > 0 {
		writeRow(1, headers, true)
	}
	startRow := 2
	if len(headers) == 0 {
		startRow = 1
	}
	for ri, row := range rows {
		writeRow(startRow+ri, row, false)
	}

	b.WriteString(`  </sheetData>
</worksheet>`)
	return b.String()
}

func buildSharedStringsXML(ss []string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="%d" uniqueCount="%d">
`, len(ss), len(ss)))
	for _, s := range ss {
		b.WriteString(fmt.Sprintf("  <si><t>%s</t></si>\n", xmlEscape(s)))
	}
	b.WriteString("</sst>")
	return b.String()
}
