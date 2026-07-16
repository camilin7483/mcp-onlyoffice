package ooxml

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

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
      <w:t>%s</w:t>
    </w:r>
  </w:p>
`, align, bold, sz, sz, xmlEscape(p.Text)))
	}
	body.WriteString("</w:body>")

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
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`,
		"word/document.xml": fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
%s
</w:document>`, body.String()),
		"word/styles.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal">
    <w:name w:val="Normal"/>
  </w:style>
</w:styles>`,
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
      <w:t>%s</w:t>
    </w:r>
  </w:p>
`, xmlEscape(title)))

	numCols := len(table.Headers)
	if numCols == 0 && len(table.Rows) > 0 {
		numCols = len(table.Rows[0])
	}
	if numCols == 0 {
		numCols = 1
	}

	body.WriteString(fmt.Sprintf(`  <w:tbl>
    <w:tblPr>
      <w:tblStyle w:val="TableGrid"/>
      <w:tblW w:w="5000" w:type="pct"/>
    </w:tblPr>
    <w:tblGrid>
`))
	for i := 0; i < numCols; i++ {
		body.WriteString(`      <w:gridCol w:w="2000"/>
`)
	}
	body.WriteString("    </w:tblGrid>\n")

	if len(table.Headers) > 0 {
		body.WriteString("    <w:tr>\n")
		for _, h := range table.Headers {
			body.WriteString(fmt.Sprintf(`      <w:tc>
        <w:p>
          <w:r>
            <w:rPr><w:b/></w:rPr>
            <w:t>%s</w:t>
          </w:r>
        </w:p>
      </w:tc>
`, xmlEscape(h)))
		}
		body.WriteString("    </w:tr>\n")
	}

	for _, row := range table.Rows {
		body.WriteString("    <w:tr>\n")
		for _, cell := range row {
			body.WriteString(fmt.Sprintf(`      <w:tc>
        <w:p>
          <w:r>
            <w:t>%s</w:t>
          </w:r>
        </w:p>
      </w:tc>
`, xmlEscape(cell)))
		}
		body.WriteString("    </w:tr>\n")
	}

	body.WriteString("  </w:tbl>\n")
	body.WriteString("</w:body>")

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
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`,
		"word/document.xml": fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
%s
</w:document>`, body.String()),
		"word/styles.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal">
    <w:name w:val="Normal"/>
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
</w:styles>`,
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
