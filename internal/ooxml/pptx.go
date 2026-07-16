package ooxml

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

func CreatePPTX(title string) ([]byte, error) {
	slides := []SlideData{{Title: title, Content: ""}}
	return CreatePPTXFromSlides(slides)
}

func CreatePPTXFromSlides(slides []SlideData) ([]byte, error) {
	if len(slides) == 0 {
		slides = []SlideData{{Title: "Presentation"}}
	}

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	ct := pptxContentTypes(len(slides))
	presRels := pptxPresRels(slides)
	presXML := pptxPresXML(slides)

	files := map[string]string{
		"[Content_Types].xml": ct,
		"_rels/.rels":         pptxTopRels(),
		"ppt/_rels/presentation.xml.rels":       presRels,
		"ppt/presentation.xml":                  presXML,
		"ppt/slideMasters/slideMaster1.xml":     pptxSlideMaster(),
		"ppt/theme/theme1.xml":                  pptxTheme(),
		"ppt/presProps.xml":                     pptxPresProps(),
		"ppt/viewProps.xml":                     pptxViewProps(),
		"ppt/tableStyles.xml":                   pptxTableStyles(),
		"ppt/slideMasters/_rels/slideMaster1.xml.rels": pptxSlideMasterRels(),
		"ppt/slideLayouts/slideLayout1.xml":            pptxSlideLayout(),
		"ppt/slideLayouts/_rels/slideLayout1.xml.rels": pptxSlideLayoutRels(),
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

	for i, slide := range slides {
		slideXML := buildSlideXML(slide.Title, slide.Content)
		f, err := w.Create(fmt.Sprintf("ppt/slides/slide%d.xml", i+1))
		if err != nil {
			return nil, fmt.Errorf("create slide%d: %w", i+1, err)
		}
		if _, err := f.Write([]byte(slideXML)); err != nil {
			return nil, fmt.Errorf("write slide%d: %w", i+1, err)
		}

		slideRel := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
</Relationships>`)
		f2, err := w.Create(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i+1))
		if err != nil {
			return nil, fmt.Errorf("create slide rels %d: %w", i+1, err)
		}
		if _, err := f2.Write([]byte(slideRel)); err != nil {
			return nil, fmt.Errorf("write slide rels %d: %w", i+1, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}

	return buf.Bytes(), nil
}

func buildSlideXML(title, content string) string {
	var contentXML string
	if content != "" {
		id := 3
		for _, line := range strings.Split(content, "\n") {
			if line == "" {
				continue
			}
			contentXML += fmt.Sprintf(`      <p:sp>
        <p:nvSpPr><p:cNvPr id="%d" name="Content"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr/></p:nvSpPr>
        <p:spPr/>
        <p:txBody>
          <a:bodyPr/>
          <a:p>
            <a:r>
              <a:rPr lang="en-US" sz="2000"/>
              <a:t>%s</a:t>
            </a:r>
          </a:p>
        </p:txBody>
      </p:sp>
`, id, xmlEscape(line))
			id++
		}
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld>
    <p:spTree>
      <p:nvGrpSpPr><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr></p:nvGrpSpPr>
      <p:grpSpPr/>
      <p:sp>
        <p:nvSpPr><p:cNvPr id="2" name="Title"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr/></p:nvSpPr>
        <p:spPr/>
        <p:txBody>
          <a:bodyPr/>
          <a:p>
            <a:r>
              <a:rPr lang="en-US" sz="4400"/>
              <a:t>%s</a:t>
            </a:r>
          </a:p>
        </p:txBody>
      </p:sp>
%s    </p:spTree>
  </p:cSld>
  <p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr>
</p:sld>`, xmlEscape(title), contentXML)
}

func pptxContentTypes(n int) string {
	ct := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>
  <Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>
  <Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>
  <Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>
  <Override PartName="/ppt/presProps.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presProps+xml"/>
  <Override PartName="/ppt/viewProps.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.viewProps+xml"/>
  <Override PartName="/ppt/tableStyles.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.tableStyles+xml"/>
`
	for i := 1; i <= n; i++ {
		ct += fmt.Sprintf("  <Override PartName=\"/ppt/slides/slide%d.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.presentationml.slide+xml\"/>\n", i)
	}
	ct += "</Types>"
	return ct
}

func pptxTopRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>
</Relationships>`
}

func pptxPresRels(slides []SlideData) string {
	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="theme/theme1.xml"/>
`
	for i := range slides {
		rels += fmt.Sprintf("  <Relationship Id=\"rId%d\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide\" Target=\"slides/slide%d.xml\"/>\n", i+3, i+1)
	}
	rels += "</Relationships>"
	return rels
}

func pptxPresXML(slides []SlideData) string {
	xml := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentation xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:sldMasterIdLst>
    <p:sldMasterId id="2147483648" r:id="rId1"/>
  </p:sldMasterIdLst>
  <p:sldIdLst>
`
	for i := range slides {
		xml += fmt.Sprintf("    <p:sldId id=\"%d\" r:id=\"rId%d\"/>\n", 256+i, i+3)
	}
	xml += `  </p:sldIdLst>
  <p:sldSz cx="9144000" cy="6858000"/>
  <p:notesSz cx="6858000" cy="9144000"/>
</p:presentation>`
	return xml
}

func pptxSlideMaster() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldMaster xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld>
    <p:spTree>
      <p:nvGrpSpPr><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr></p:nvGrpSpPr>
      <p:grpSpPr/>
    </p:spTree>
  </p:cSld>
  <p:sldLayoutIdLst>
    <p:sldLayoutId id="2147483649" r:id="rId2"/>
  </p:sldLayoutIdLst>
</p:sldMaster>`
}

func pptxSlideLayout() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldLayout xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" type="blank">
  <p:cSld>
    <p:spTree>
      <p:nvGrpSpPr><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr></p:nvGrpSpPr>
      <p:grpSpPr/>
    </p:spTree>
  </p:cSld>
</p:sldLayout>`
}

func pptxSlideLayoutRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/>
</Relationships>`
}

func pptxSlideMasterRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/theme1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
</Relationships>`
}

func pptxTheme() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Default">
  <a:themeElements>
    <a:clrScheme name="Default"><a:dk1><a:srgbClr val="000000"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="44546A"/></a:dk2><a:lt2><a:srgbClr val="E7E6E6"/></a:lt2><a:accent1><a:srgbClr val="4472C4"/></a:accent1><a:accent2><a:srgbClr val="ED7D31"/></a:accent2><a:accent3><a:srgbClr val="A5A5A5"/></a:accent3><a:accent4><a:srgbClr val="FFC000"/></a:accent4><a:accent5><a:srgbClr val="5B9BD5"/></a:accent5><a:accent6><a:srgbClr val="70AD47"/></a:accent6><a:hlink><a:srgbClr val="0563C1"/></a:hlink><a:folHlink><a:srgbClr val="954F72"/></a:folHlink></a:clrScheme>
    <a:fontScheme name="Default"><a:majorFont><a:latin typeface="Calibri Light"/></a:majorFont><a:minorFont><a:latin typeface="Calibri"/></a:minorFont></a:fontScheme>
    <a:fmtScheme name="Default"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst><a:lnStyleLst><a:ln w="6350"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst><a:effectStyleLst/></a:fmtScheme>
  </a:themeElements>
</a:theme>`
}

func pptxPresProps() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presProps xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"/>`
}

func pptxViewProps() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:viewProps xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:normalViewLogPref/><p:slideViewPr><p:cSldViewPr><p:cViewPref/><p:guideLst/></p:cSldViewPr></p:slideViewPr>
</p:viewProps>`
}

func pptxTableStyles() string {
	return `<a:tblStyleLst xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" def="{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}"/>`
}
