package ooxml

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type cellType int

const (
	cellString cellType = iota
	cellNumber
	cellFormula
	cellBool
)

func cellTypeOf(val string) (cellType, string) {
	if strings.HasPrefix(val, "=") {
		return cellFormula, val[1:]
	}
	if isBool(val) {
		return cellBool, strings.ToLower(val)
	}
	if isNumeric(val) {
		return cellNumber, val
	}
	return cellString, val
}

func isBool(s string) bool {
	switch s {
	case "true", "false", "TRUE", "FALSE":
		return true
	}
	return false
}

func CreateXLSX(title string) ([]byte, error) {
	return CreateXLSXFromData(title, nil, nil)
}

func CreateXLSXFromData(title string, headers []string, rows [][]string) ([]byte, error) {
	title = sanitizeSheetName(title)

	sharedStrings := collectSharedStrings(headers, rows)
	sheetXML := buildSheetXML(headers, rows, sharedStrings)
	sharedStringsXML := buildSharedStringsXML(sharedStrings, countSharedStringUses(headers, rows))

	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
  <Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
  <Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>
` + docPropsContentTypes() + `</Types>`,
		"_rels/.rels":       packageRels("xl/workbook.xml"),
		"docProps/core.xml": docPropsCore(title),
		"docProps/app.xml":  docPropsApp(),
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

	return writeZipOrdered(files)
}

func collectSharedStrings(headers []string, rows [][]string) []string {
	seen := map[string]int{}
	var list []string
	add := func(s string) {
		// Headers are always labels, even when numeric ("2024").
		if _, ok := seen[s]; !ok {
			seen[s] = len(list)
			list = append(list, s)
		}
	}
	for _, h := range headers {
		add(h)
	}
	for _, row := range rows {
		for _, cell := range row {
			if cell == "" || strings.HasPrefix(cell, "=") || isBool(cell) || isNumeric(cell) {
				continue
			}
			add(cell)
		}
	}
	return list
}

// countSharedStringUses returns total string-cell occurrences (duplicates
// counted each time); uniqueCount is len(collectSharedStrings(...)).
func countSharedStringUses(headers []string, rows [][]string) int {
	total := len(headers) // headers are always shared strings
	for _, row := range rows {
		for _, cell := range row {
			if cell == "" || strings.HasPrefix(cell, "=") || isBool(cell) || isNumeric(cell) {
				continue
			}
			total++
		}
	}
	return total
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return false
	}
	// NaN/Inf are not valid spreadsheet numbers; store as text.
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

func buildSheetXML(headers []string, rows [][]string, sharedStrings []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
`)
	b.WriteString("  " + sheetDimension(headers, rows) + "\n")
	b.WriteString(`  <sheetData>
`)
	ssIndex := func(s string) int {
		for i, v := range sharedStrings {
			if v == s {
				return i
			}
		}
		return -1
	}

	writeCell := func(ref, val string, isHeader bool) {
		if val == "" {
			return
		}
		style := ""
		if isHeader {
			style = ` s="1"`
		}
		// Headers are labels: always shared strings.
		if isHeader {
			if si := ssIndex(val); si >= 0 {
				b.WriteString(fmt.Sprintf("      <c r=\"%s\" t=\"s\"%s><v>%d</v></c>\n", ref, style, si))
			} else {
				b.WriteString(fmt.Sprintf("      <c r=\"%s\" t=\"inlineStr\"%s><is><t>%s</t></is></c>\n", ref, style, xmlEscape(val)))
			}
			return
		}
		if strings.HasPrefix(val, "=") {
			b.WriteString(fmt.Sprintf("      <c r=\"%s\"><f>%s</f><v>0</v></c>\n", ref, xmlEscape(val[1:])))
			return
		}
		if isBool(val) {
			v := "0"
			if strings.EqualFold(val, "true") {
				v = "1"
			}
			b.WriteString(fmt.Sprintf("      <c r=\"%s\" t=\"b\"><v>%s</v></c>\n", ref, v))
			return
		}
		if isNumeric(val) {
			b.WriteString(fmt.Sprintf("      <c r=\"%s\"><v>%s</v></c>\n", ref, val))
			return
		}
		if si := ssIndex(val); si >= 0 {
			b.WriteString(fmt.Sprintf("      <c r=\"%s\" t=\"s\"><v>%d</v></c>\n", ref, si))
		} else {
			b.WriteString(fmt.Sprintf("      <c r=\"%s\" t=\"inlineStr\"><is><t>%s</t></is></c>\n", ref, xmlEscape(val)))
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

func sheetDimension(headers []string, rows [][]string) string {
	maxCols := len(headers)
	for _, row := range rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}
	totalRows := len(rows)
	if len(headers) > 0 {
		totalRows++
	}
	if maxCols == 0 || totalRows == 0 {
		return `<dimension ref="A1"/>`
	}
	return fmt.Sprintf(`<dimension ref="A1:%s%d"/>`, colName(maxCols-1), totalRows)
}

func buildSharedStringsXML(ss []string, total int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="%d" uniqueCount="%d">
`, total, len(ss)))
	for _, s := range ss {
		b.WriteString(fmt.Sprintf("  <si><t>%s</t></si>\n", xmlEscape(s)))
	}
	b.WriteString("</sst>")
	return b.String()
}
