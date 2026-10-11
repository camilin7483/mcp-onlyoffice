package ooxml

import (
	"fmt"
	"strings"
)

// Corporate palette shared by the business template pack, the native
// DOCX builder, and the Python engine (keep the hex values in sync).
const (
	CorporateBlueDark  = "1F4E79"
	CorporateBlue      = "2E75B6"
	CorporateBlueLight = "D9E2F3"
	CorporateWhite     = "FFFFFF"
	CorporateBodyText  = "262626"
	CorporateQuoteText = "404040"
)

// CorporateOptions tunes the corporate report builder.
type CorporateOptions struct {
	Subtitle string
	Date     string
	Header   string
	Footer   string
	// PageSize is "letter" (default) or "A4".
	PageSize string
}

// CorporateSection is one headed block of a corporate report.
type CorporateSection struct {
	Heading string
	// Level selects Heading1-3; out-of-range values clamp to Heading1.
	Level int
	Body  []string
	// Quote renders a single Quote-styled paragraph after Body.
	Quote string
	Table *TableData
}

// normalizePageSize maps user input to "letter" or "A4".
func normalizePageSize(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), "a4") {
		return "A4"
	}
	return "letter"
}

// corporateSectPr returns page geometry with 1-inch margins. Letter and A4
// are the only supported sizes; anything else falls back to letter.
func corporateSectPr(pageSize string, withHeaderFooter bool) string {
	w, h := "12240", "15840" // letter
	if normalizePageSize(pageSize) == "A4" {
		w, h = "11906", "16838"
	}
	refs := ""
	if withHeaderFooter {
		refs = `<w:headerReference w:type="default" r:id="rId2"/><w:footerReference w:type="default" r:id="rId3"/>`
	}
	return fmt.Sprintf(`<w:sectPr>%s<w:pgSz w:w="%s" w:h="%s" w:orient="portrait"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720"/></w:sectPr>`, refs, w, h)
}

// corporateStylesXML declares Normal, Title, Heading1-3, Quote, and
// TableGrid in the corporate palette.
func corporateStylesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal">
    <w:name w:val="Normal"/>
    <w:pPr><w:spacing w:after="120" w:line="276" w:lineRule="auto"/></w:pPr>
    <w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:sz w:val="22"/><w:szCs w:val="22"/><w:color w:val="` + CorporateBodyText + `"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Title">
    <w:name w:val="Title"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:pPr><w:jc w:val="center"/><w:spacing w:after="120"/></w:pPr>
    <w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:b/><w:sz w:val="56"/><w:szCs w:val="56"/><w:color w:val="` + CorporateBlueDark + `"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading1">
    <w:name w:val="heading 1"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:pPr><w:keepNext/><w:spacing w:before="240" w:after="60"/></w:pPr>
    <w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:b/><w:sz w:val="32"/><w:szCs w:val="32"/><w:color w:val="` + CorporateBlueDark + `"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading2">
    <w:name w:val="heading 2"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:pPr><w:keepNext/><w:spacing w:before="200" w:after="60"/></w:pPr>
    <w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:b/><w:sz w:val="28"/><w:szCs w:val="28"/><w:color w:val="` + CorporateBlue + `"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading3">
    <w:name w:val="heading 3"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:pPr><w:keepNext/><w:spacing w:before="160" w:after="60"/></w:pPr>
    <w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:b/><w:i/><w:sz w:val="24"/><w:szCs w:val="24"/><w:color w:val="` + CorporateBlue + `"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Quote">
    <w:name w:val="Quote"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:pPr><w:ind w:left="720"/><w:spacing w:before="120" w:after="120"/></w:pPr>
    <w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:i/><w:sz w:val="22"/><w:szCs w:val="22"/><w:color w:val="` + CorporateQuoteText + `"/></w:rPr>
  </w:style>
  <w:style w:type="table" w:styleId="TableGrid">
    <w:name w:val="Table Grid"/>
    <w:pPr><w:spacing w:after="0"/></w:pPr>
    <w:tblPr>
      <w:tblBorders>
        <w:top w:val="single" w:sz="4" w:space="0" w:color="auto"/>
        <w:left w:val="single" w:sz="4" w:space="0" w:color="auto"/>
        <w:bottom w:val="single" w:sz="4" w:space="0" w:color="auto"/>
        <w:right w:val="single" w:sz="4" w:space="0" w:color="auto"/>
        <w:insideH w:val="single" w:sz="4" w:space="0" w:color="auto"/>
        <w:insideV w:val="single" w:sz="4" w:space="0" w:color="auto"/>
      </w:tblBorders>
    </w:tblPr>
  </w:style>
</w:styles>`
}

func corporateContentTypes(withHeaderFooter bool) string {
	extra := ""
	if withHeaderFooter {
		extra = `  <Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/>
  <Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>
`
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
` + extra + docPropsContentTypes() + `</Types>`
}

func corporateDocRels(withHeaderFooter bool) string {
	extra := ""
	if withHeaderFooter {
		extra = `
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>`
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` + extra + `
</Relationships>`
}

func styledPara(style, align, text string) string {
	jc := ""
	if align != "" {
		jc = fmt.Sprintf(`<w:jc w:val="%s"/>`, align)
	}
	return fmt.Sprintf(`  <w:p>
    <w:pPr><w:pStyle w:val="%s"/>%s</w:pPr>
    <w:r>
      <w:t xml:space="preserve">%s</w:t>
    </w:r>
  </w:p>
`, style, jc, xmlEscape(text))
}

func coverTitlePara(text string) string {
	return fmt.Sprintf(`  <w:p>
    <w:pPr><w:pStyle w:val="Title"/><w:jc w:val="center"/></w:pPr>
    <w:r>
      <w:t xml:space="preserve">%s</w:t>
    </w:r>
  </w:p>
`, xmlEscape(text))
}

func coverAccentPara(text string, size int) string {
	return fmt.Sprintf(`  <w:p>
    <w:pPr><w:jc w:val="center"/></w:pPr>
    <w:r>
      <w:rPr><w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr>
      <w:t xml:space="preserve">%s</w:t>
    </w:r>
  </w:p>
`, CorporateBlue, size, size, xmlEscape(text))
}

func pageBreakPara() string {
	return `  <w:p>
    <w:r>
      <w:br w:type="page"/>
    </w:r>
  </w:p>
`
}

// tocFieldPara emits a real Word TOC field; Word fills it in on update.
func tocFieldPara() string {
	return `  <w:p>
    <w:r><w:fldChar w:fldCharType="begin"/></w:r>
    <w:r><w:instrText xml:space="preserve">TOC \o "1-3" \h \z \u</w:instrText></w:r>
    <w:r><w:fldChar w:fldCharType="separate"/></w:r>
    <w:r><w:t xml:space="preserve">Table of contents — right-click and choose Update Field.</w:t></w:r>
    <w:r><w:fldChar w:fldCharType="end"/></w:r>
  </w:p>
`
}

func corporateHeaderXML(text string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:p>
    <w:pPr><w:jc w:val="center"/><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="4" w:color="%s"/></w:pBdr></w:pPr>
    <w:r>
      <w:rPr><w:color w:val="%s"/><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr>
      <w:t xml:space="preserve">%s</w:t>
    </w:r>
  </w:p>
</w:hdr>`, CorporateBlue, CorporateBlue, xmlEscape(text))
}

func corporateFooterXML(text string) string {
	prefix := ""
	if text != "" {
		prefix = fmt.Sprintf(`<w:r>
      <w:rPr><w:color w:val="%s"/><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr>
      <w:t xml:space="preserve">%s   </w:t>
    </w:r>
    `, CorporateBlue, xmlEscape(text))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:p>
    <w:pPr><w:jc w:val="center"/></w:pPr>
    %s<w:r>
      <w:rPr><w:color w:val="%s"/><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr>
      <w:t xml:space="preserve">Page </w:t>
    </w:r>
    <w:r><w:fldChar w:fldCharType="begin"/></w:r>
    <w:r><w:instrText xml:space="preserve">PAGE</w:instrText></w:r>
    <w:r><w:fldChar w:fldCharType="separate"/></w:r>
    <w:r>
      <w:rPr><w:color w:val="%s"/><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr>
      <w:t xml:space="preserve">1</w:t>
    </w:r>
    <w:r><w:fldChar w:fldCharType="end"/></w:r>
  </w:p>
</w:ftr>`, prefix, CorporateBlue, CorporateBlue)
}

// headingStyleForLevel maps 1-3 to Heading1-3, clamping out-of-range input.
func headingStyleForLevel(level int) string {
	switch level {
	case 2:
		return "Heading2"
	case 3:
		return "Heading3"
	default:
		return "Heading1"
	}
}

// writeCorporateTable renders a TableGrid table with a corporate-blue header
// row, banded body rows, and ragged rows padded with empty cells.
func writeCorporateTable(body *strings.Builder, table TableData) {
	numCols := len(table.Headers)
	if numCols == 0 && len(table.Rows) > 0 {
		numCols = len(table.Rows[0])
	}
	if numCols == 0 {
		numCols = 1
	}

	body.WriteString(`  <w:tbl>
    <w:tblPr>
      <w:tblStyle w:val="TableGrid"/>
      <w:tblW w:w="5000" w:type="pct"/>
      <w:tblLook w:val="04A0" w:firstRow="1" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="0" w:noVBand="1"/>
    </w:tblPr>
    <w:tblGrid>
`)
	for i := 0; i < numCols; i++ {
		body.WriteString("      <w:gridCol w:w=\"2000\"/>\n")
	}
	body.WriteString("    </w:tblGrid>\n")

	writeCell := func(text, fill string, header bool) {
		shd := ""
		if fill != "" {
			shd = fmt.Sprintf(`<w:tcPr><w:shd w:val="clear" w:color="auto" w:fill="%s"/></w:tcPr>`, fill)
		}
		rPr := fmt.Sprintf(`<w:sz w:val="22"/><w:szCs w:val="22"/>`)
		if header {
			rPr = fmt.Sprintf(`<w:b/><w:color w:val="%s"/>`, CorporateWhite) + rPr
		}
		body.WriteString(fmt.Sprintf(`      <w:tc>
        %s<w:p>
          <w:r>
            <w:rPr>%s</w:rPr>
            <w:t xml:space="preserve">%s</w:t>
          </w:r>
        </w:p>
      </w:tc>
`, shd, rPr, xmlEscape(text)))
	}

	if len(table.Headers) > 0 {
		body.WriteString("    <w:tr>\n      <w:trPr><w:tblHeader/></w:trPr>\n")
		for _, h := range table.Headers {
			writeCell(h, CorporateBlueDark, true)
		}
		body.WriteString("    </w:tr>\n")
	}

	for ri, row := range table.Rows {
		body.WriteString("    <w:tr>\n")
		fill := ""
		if ri%2 == 0 {
			fill = CorporateBlueLight
		}
		for ci := 0; ci < numCols; ci++ {
			cell := ""
			if ci < len(row) {
				cell = row[ci]
			}
			writeCell(cell, fill, false)
		}
		body.WriteString("    </w:tr>\n")
	}

	body.WriteString("  </w:tbl>\n")
}

func CreateDOCX(title string) ([]byte, error) {
	return CreateDOCXFromParagraphs([]DocxParagraph{
		{Text: title, Size: 24},
	})
}

func CreateDOCXFromParagraphs(paras []DocxParagraph) ([]byte, error) {
	if len(paras) == 0 {
		paras = []DocxParagraph{{Text: "New Document", Size: 24}}
	}

	var body strings.Builder
	body.WriteString("<w:body>\n")
	for _, p := range paras {
		align := ""
		switch p.Align {
		case "center":
			align = ` <w:jc w:val="center"/>`
		case "right":
			align = ` <w:jc w:val="right"/>`
		case "left":
			align = ` <w:jc w:val="left"/>`
		}
		sz := p.Size
		if sz == 0 {
			sz = 22
		}
		bold := ""
		if p.Bold {
			bold = `<w:b/>`
		}
		body.WriteString(fmt.Sprintf(`  <w:p>
    <w:pPr>%s</w:pPr>
    <w:r>
      <w:rPr>%s<w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr>
      <w:t xml:space="preserve">%s</w:t>
    </w:r>
  </w:p>
`, align, bold, sz, sz, xmlEscape(p.Text)))
	}
	body.WriteString(corporateSectPr("letter", false) + "</w:body>")

	title0 := ""
	if len(paras) > 0 {
		title0 = paras[0].Text
	}
	files := map[string]string{
		"[Content_Types].xml":          corporateContentTypes(false),
		"_rels/.rels":                  packageRels("word/document.xml"),
		"docProps/core.xml":            docPropsCore(title0),
		"docProps/app.xml":             docPropsApp(),
		"word/_rels/document.xml.rels": corporateDocRels(false),
		"word/document.xml": fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
%s
</w:document>`, body.String()),
		"word/styles.xml": corporateStylesXML(),
	}

	return writeZipOrdered(files)
}

func CreateDOCXFromText(title string, bodyText string) ([]byte, error) {
	paras := []DocxParagraph{
		{Text: title, Size: 28, Bold: true},
	}
	if bodyText != "" {
		lines := strings.Split(bodyText, "\n")
		for _, line := range lines {
			paras = append(paras, DocxParagraph{Text: line, Size: 22})
		}
	}
	return CreateDOCXFromParagraphs(paras)
}

func CreateDOCXWithTable(title string, table TableData) ([]byte, error) {
	var body strings.Builder
	body.WriteString("<w:body>\n")

	body.WriteString(fmt.Sprintf(`  <w:p>
    <w:r>
      <w:rPr><w:sz w:val="28"/><w:szCs w:val="28"/></w:rPr>
      <w:t xml:space="preserve">%s</w:t>
    </w:r>
  </w:p>
`, xmlEscape(title)))

	writeCorporateTable(&body, table)

	body.WriteString(corporateSectPr("letter", false) + "</w:body>")

	files := map[string]string{
		"[Content_Types].xml":          corporateContentTypes(false),
		"_rels/.rels":                  packageRels("word/document.xml"),
		"docProps/core.xml":            docPropsCore(title),
		"docProps/app.xml":             docPropsApp(),
		"word/_rels/document.xml.rels": corporateDocRels(false),
		"word/document.xml": fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
%s
</w:document>`, body.String()),
		"word/styles.xml": corporateStylesXML(),
	}

	return writeZipOrdered(files)
}

// CreateCorporateReport builds a business report with a cover page
// (title/subtitle/date), a TOC field placeholder, styled sections, banded
// tables, and headers/footers with PAGE numbering.
func CreateCorporateReport(title string, opts CorporateOptions, sections []CorporateSection) ([]byte, error) {
	if strings.TrimSpace(title) == "" {
		title = "Business Report"
	}
	pageSize := normalizePageSize(opts.PageSize)
	header := opts.Header
	if header == "" {
		header = title
	}

	var body strings.Builder
	body.WriteString("<w:body>\n")

	body.WriteString(coverTitlePara(title))
	if opts.Subtitle != "" {
		body.WriteString(coverAccentPara(opts.Subtitle, 32))
	}
	if opts.Date != "" {
		body.WriteString(coverAccentPara(opts.Date, 24))
	}
	body.WriteString(pageBreakPara())

	body.WriteString(styledPara("Heading1", "", "Table of Contents"))
	body.WriteString(tocFieldPara())

	for _, s := range sections {
		if s.Heading != "" {
			body.WriteString(styledPara(headingStyleForLevel(s.Level), "", s.Heading))
		}
		for _, line := range s.Body {
			if line == "" {
				continue
			}
			body.WriteString(styledPara("Normal", "", line))
		}
		if s.Quote != "" {
			body.WriteString(styledPara("Quote", "", s.Quote))
		}
		if s.Table != nil {
			writeCorporateTable(&body, *s.Table)
		}
	}

	body.WriteString(corporateSectPr(pageSize, true) + "</w:body>")

	files := map[string]string{
		"[Content_Types].xml":          corporateContentTypes(true),
		"_rels/.rels":                  packageRels("word/document.xml"),
		"docProps/core.xml":            docPropsCore(title),
		"docProps/app.xml":             docPropsApp(),
		"word/_rels/document.xml.rels": corporateDocRels(true),
		"word/document.xml": fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
%s
</w:document>`, body.String()),
		"word/styles.xml":  corporateStylesXML(),
		"word/header1.xml": corporateHeaderXML(header),
		"word/footer1.xml": corporateFooterXML(opts.Footer),
	}

	return writeZipOrdered(files)
}
