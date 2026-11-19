package ooxml

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// edit.go — in-place OOXML editing with preservation.
//
// The writers in this package build packages from scratch; the functions
// here do the opposite: open an existing package and rewrite only the part
// a change touches. Every other part is carried over in its original order
// with the same compression method and identical decompressed bytes
// (unknown parts, styles, themes, rels, media, comments, footnotes,
// headers/footers). Recompression means the raw deflate bytes of untouched
// parts may differ, but their content never does.
//
// Paragraph model (documented choice): w:p elements are counted in document
// order across the whole w:body, including paragraphs inside tables
// (w:tbl/w:tr/w:tc/w:p). This is the simplest consistent model: one flat
// index, no special cases for table vs body text.
//
// Limitation: paragraphs that contain a text box (w:txbxContent with its
// own nested w:p) are not edited. The flat-regex paragraph scan cannot
// match nested w:p elements correctly, so targeting such a paragraph
// returns ErrNestedParagraph (naming the index) instead of corrupting
// the part. Extract the text box content with a real XML reader to edit
// those paragraphs.

const maxEditBytes = 64 << 20 // 64 MiB input cap for every edit function

const (
	// maxEditEntries caps how many parts an edit carries; hostile
	// packages with thousands of entries are refused before any part is read.
	maxEditEntries = 2048
	// maxEditTotalBytes caps total decompressed bytes across all parts of
	// one edit; a zip bomb of small parts is refused partway through.
	maxEditTotalBytes = 256 << 20 // 256 MiB
)

// ErrNestedParagraph is returned when a targeted paragraph contains a
// nested w:p inside w:txbxContent. Use errors.Is to detect it; the
// concrete *NestedParagraphError names the paragraph index.
var ErrNestedParagraph = errors.New("ooxml: nested paragraph (w:txbxContent)")

// NestedParagraphError names the paragraph index that cannot be edited.
type NestedParagraphError struct {
	Index int
}

func (e *NestedParagraphError) Error() string {
	return fmt.Sprintf("ooxml: nested paragraph at index %d (w:txbxContent): edit refused", e.Index)
}

// Unwrap lets errors.Is(err, ErrNestedParagraph) work.
func (e *NestedParagraphError) Unwrap() error { return ErrNestedParagraph }

func checkNestedPara(para string, index int) error {
	if strings.Contains(para, "w:txbxContent") {
		return &NestedParagraphError{Index: index}
	}
	return nil
}

var (
	paraRe    = regexp.MustCompile(`(?s)<w:p\b[^>]*/>|<w:p\b[^>]*>.*?</w:p>`)
	runRe     = regexp.MustCompile(`(?s)<w:r\b[^>]*>.*?</w:r>`)
	textTag   = regexp.MustCompile(`<w:t[\s>]`)
	tRe       = regexp.MustCompile(`(?s)<w:t\b[^>]*>.*?</w:t>`)
	rPrRe     = regexp.MustCompile(`(?s)<w:rPr\b[^>]*/>|(?s)<w:rPr\b[^>]*>.*?</w:rPr>`)
	pPrOpen   = regexp.MustCompile(`<w:pPr\b[^>]*>`)
	pPrSelf   = regexp.MustCompile(`<w:pPr(\s[^>]*)?/>`)
	pStyleTag = regexp.MustCompile(`<w:pStyle\b[^>]*>`)
	pOpenRe   = regexp.MustCompile(`<w:p\b[^>]*>`)
	cellRefRe = regexp.MustCompile(`^([A-Z]{1,3})([1-9][0-9]*)$`)
)

// Hoisted attribute regexes: tagAttr used to compile one regexp per call.
var (
	attrIDRe         = regexp.MustCompile(`Id="([^"]*)"`)
	attrTargetRe     = regexp.MustCompile(`Target="([^"]*)"`)
	attrNameRe       = regexp.MustCompile(`name="([^"]*)"`)
	attrRIdRe        = regexp.MustCompile(`r:id="([^"]*)"`)
	attrRRe          = regexp.MustCompile(`\br="([^"]*)"`)
	attrSRe          = regexp.MustCompile(`\bs="([^"]*)"`)
	attrCmRe         = regexp.MustCompile(`\bcm="([^"]*)"`)
	attrVmRe         = regexp.MustCompile(`\bvm="([^"]*)"`)
	attrTRe          = regexp.MustCompile(`\bt="([^"]*)"`)
	attrNumFmtIDRe   = regexp.MustCompile(`numFmtId="([^"]*)"`)
	attrFormatCodeRe = regexp.MustCompile(`formatCode="([^"]*)"`)
)

// Digest returns the sha256 of the package content as 16 hex chars.
// It is the opaque revision token edits compare with expected_revision.
func Digest(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])[:16]
}

func checkEditSize(src []byte) error {
	if len(src) > maxEditBytes {
		return fmt.Errorf("ooxml: package is %d bytes, over the %d byte limit", len(src), maxEditBytes)
	}
	return nil
}

// wellFormed reports whether data parses as well-formed XML.
func wellFormed(data []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// zipEntry is one package part carried through an edit, keeping its
// original header so untouched parts keep Modified, Extra, Comment,
// ExternalAttrs, versions and flag bits.
type zipEntry struct {
	header zip.FileHeader
	data   []byte
}

func readEditEntries(src []byte) ([]zipEntry, error) {
	if err := checkEditSize(src); err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		return nil, fmt.Errorf("ooxml: invalid package: %w", err)
	}
	if len(zr.File) > maxEditEntries {
		return nil, fmt.Errorf("ooxml: package has %d entries, over the %d entry limit", len(zr.File), maxEditEntries)
	}
	entries := make([]zipEntry, 0, len(zr.File))
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("ooxml: open part %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxEditBytes+1))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("ooxml: read part %s: %w", f.Name, err)
		}
		if int64(len(data)) > maxEditBytes {
			return nil, fmt.Errorf("ooxml: part %s exceeds the %d byte limit", f.Name, maxEditBytes)
		}
		total += int64(len(data))
		if total > maxEditTotalBytes {
			return nil, fmt.Errorf("ooxml: package decompresses past the %d byte total limit", maxEditTotalBytes)
		}
		entries = append(entries, zipEntry{header: f.FileHeader, data: data})
	}
	return entries, nil
}

// writeEditEntries repacks entries in their original order, swapping in
// changed parts. Untouched parts keep header and exact content bytes.
func writeEditEntries(entries []zipEntry, changed map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		data := e.data
		if replacement, ok := changed[e.header.Name]; ok {
			data = replacement
		}
		fh := e.header
		f, err := w.CreateHeader(&fh)
		if err != nil {
			return nil, fmt.Errorf("ooxml: create %s: %w", e.header.Name, err)
		}
		if _, err := f.Write(data); err != nil {
			return nil, fmt.Errorf("ooxml: write %s: %w", e.header.Name, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("ooxml: close package: %w", err)
	}
	return buf.Bytes(), nil
}

func findPart(entries []zipEntry, name string) ([]byte, error) {
	for _, e := range entries {
		if e.header.Name == name {
			return e.data, nil
		}
	}
	return nil, fmt.Errorf("ooxml: required part missing: %s", name)
}

func xmlUnescape(s string) string {
	s = strings.ReplaceAll(s, "&apos;", "'")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&amp;", "&")
	return s
}

// paraTexts returns the concatenated w:t text of one raw paragraph.
func paraTexts(para string) string {
	var b strings.Builder
	for _, m := range tRe.FindAllString(para, -1) {
		inner := m[strings.Index(m, ">")+1 : strings.LastIndex(m, "<")]
		b.WriteString(xmlUnescape(inner))
	}
	return b.String()
}

// isSelfClosingPara reports whether a matched paragraph is <w:p .../>.
func isSelfClosingPara(para string) bool {
	return strings.HasSuffix(strings.TrimSpace(para), "/>")
}

// selfClosingOpen converts <w:p attrs/> to <w:p attrs>.
func selfClosingOpen(para string) string {
	return strings.TrimSuffix(strings.TrimSpace(para), "/>") + ">"
}

// replaceParaText keeps w:pPr and the first run's w:rPr, puts text into the
// first text run, drops the remaining text runs, keeps non-text runs.
// A self-closing <w:p .../> becomes a normal <w:p ...>...</w:p>.
func replaceParaText(para, text string) string {
	if isSelfClosingPara(para) {
		open := selfClosingOpen(para)
		return open + `<w:r><w:t xml:space="preserve">` + xmlEscape(text) + `</w:t></w:r></w:p>`
	}
	runs := runRe.FindAllStringIndex(para, -1)
	if len(runs) == 0 {
		return para[:len(para)-len("</w:p>")] +
			`<w:r><w:t xml:space="preserve">` + xmlEscape(text) + `</w:t></w:r></w:p>`
	}
	firstText := -1
	for i, loc := range runs {
		if textTag.MatchString(para[loc[0]:loc[1]]) {
			if firstText < 0 {
				firstText = i
			}
		}
	}
	if firstText < 0 {
		return para[:len(para)-len("</w:p>")] +
			`<w:r><w:t xml:space="preserve">` + xmlEscape(text) + `</w:t></w:r></w:p>`
	}
	var b strings.Builder
	pos := 0
	for i, loc := range runs {
		run := para[loc[0]:loc[1]]
		if !textTag.MatchString(run) {
			continue // non-text run: keep via gap copy below
		}
		if i != firstText {
			b.WriteString(para[pos:loc[0]])
			pos = loc[1] // drop this text run
			continue
		}
		// first text run: keep its rPr, swap the text
		rPr := ""
		if m := rPrRe.FindString(run); m != "" {
			rPr = m
		}
		b.WriteString(para[pos:loc[0]])
		b.WriteString(`<w:r>` + rPr + `<w:t xml:space="preserve">` + xmlEscape(text) + `</w:t></w:r>`)
		pos = loc[1]
	}
	b.WriteString(para[pos:])
	return b.String()
}

// ParagraphText returns the plain text of the paragraph at 0-based index.
func ParagraphText(src []byte, paragraph int) (string, error) {
	entries, err := readEditEntries(src)
	if err != nil {
		return "", err
	}
	doc, err := findPart(entries, "word/document.xml")
	if err != nil {
		return "", err
	}
	if err := wellFormed(doc); err != nil {
		return "", fmt.Errorf("ooxml: malformed word/document.xml: %w", err)
	}
	paras := paraRe.FindAllString(string(doc), -1)
	if paragraph < 0 || paragraph >= len(paras) {
		return "", fmt.Errorf("ooxml: paragraph index %d out of range (%d paragraphs)", paragraph, len(paras))
	}
	if err := checkNestedPara(paras[paragraph], paragraph); err != nil {
		return "", err
	}
	return paraTexts(paras[paragraph]), nil
}

// editDocParagraph applies fn to one paragraph of word/document.xml.
func editDocParagraph(src []byte, paragraph int, fn func(para string) (string, error)) ([]byte, error) {
	entries, err := readEditEntries(src)
	if err != nil {
		return nil, err
	}
	doc, err := findPart(entries, "word/document.xml")
	if err != nil {
		return nil, err
	}
	if err := wellFormed(doc); err != nil {
		return nil, fmt.Errorf("ooxml: malformed word/document.xml: %w", err)
	}
	s := string(doc)
	locs := paraRe.FindAllStringIndex(s, -1)
	if paragraph < 0 || paragraph >= len(locs) {
		return nil, fmt.Errorf("ooxml: paragraph index %d out of range (%d paragraphs)", paragraph, len(locs))
	}
	loc := locs[paragraph]
	if err := checkNestedPara(s[loc[0]:loc[1]], paragraph); err != nil {
		return nil, err
	}
	edited, err := fn(s[loc[0]:loc[1]])
	if err != nil {
		return nil, err
	}
	if err := wellFormed([]byte(edited)); err != nil {
		return nil, fmt.Errorf("ooxml: edit produced malformed paragraph: %w", err)
	}
	s = s[:loc[0]] + edited + s[loc[1]:]
	if err := wellFormed([]byte(s)); err != nil {
		return nil, fmt.Errorf("ooxml: edit produced malformed word/document.xml: %w", err)
	}
	return writeEditEntries(entries, map[string][]byte{"word/document.xml": []byte(s)})
}

// ReplaceParagraphText sets the text of one paragraph, keeping its
// properties and the first run's properties.
func ReplaceParagraphText(src []byte, paragraph int, text string) ([]byte, error) {
	return editDocParagraph(src, paragraph, func(para string) (string, error) {
		return replaceParaText(para, text), nil
	})
}

// InsertParagraphAfter inserts a new paragraph right after the one at index.
func InsertParagraphAfter(src []byte, paragraph int, text, style string) ([]byte, error) {
	entries, err := readEditEntries(src)
	if err != nil {
		return nil, err
	}
	doc, err := findPart(entries, "word/document.xml")
	if err != nil {
		return nil, err
	}
	if err := wellFormed(doc); err != nil {
		return nil, fmt.Errorf("ooxml: malformed word/document.xml: %w", err)
	}
	s := string(doc)
	locs := paraRe.FindAllStringIndex(s, -1)
	if paragraph < 0 || paragraph >= len(locs) {
		return nil, fmt.Errorf("ooxml: paragraph index %d out of range (%d paragraphs)", paragraph, len(locs))
	}
	if err := checkNestedPara(s[locs[paragraph][0]:locs[paragraph][1]], paragraph); err != nil {
		return nil, err
	}
	pPr := ""
	if style != "" {
		pPr = `<w:pPr><w:pStyle w:val="` + xmlEscape(style) + `"/></w:pPr>`
	}
	ins := `<w:p>` + pPr + `<w:r><w:t xml:space="preserve">` + xmlEscape(text) + `</w:t></w:r></w:p>`
	end := locs[paragraph][1]
	s = s[:end] + ins + s[end:]
	if err := wellFormed([]byte(s)); err != nil {
		return nil, fmt.Errorf("ooxml: edit produced malformed word/document.xml: %w", err)
	}
	return writeEditEntries(entries, map[string][]byte{"word/document.xml": []byte(s)})
}

// SetParagraphStyle changes only w:pStyle of one paragraph.
func SetParagraphStyle(src []byte, paragraph int, style string) ([]byte, error) {
	if style == "" {
		return nil, fmt.Errorf("ooxml: style is required")
	}
	val := `<w:pStyle w:val="` + xmlEscape(style) + `"/>`
	return editDocParagraph(src, paragraph, func(para string) (string, error) {
		if m := pStyleTag.FindString(para); m != "" {
			return pStyleTag.ReplaceAllString(para, val), nil
		}
		if isSelfClosingPara(para) {
			return selfClosingOpen(para) + `<w:pPr>` + val + `</w:pPr></w:p>`, nil
		}
		if loc := pPrSelf.FindStringIndex(para); loc != nil {
			return para[:loc[0]] + `<w:pPr>` + val + `</w:pPr>` + para[loc[1]:], nil
		}
		if loc := pPrOpen.FindStringIndex(para); loc != nil {
			return para[:loc[1]] + val + para[loc[1]:], nil
		}
		if loc := pOpenRe.FindStringIndex(para); loc != nil {
			return para[:loc[1]] + `<w:pPr>` + val + `</w:pPr>` + para[loc[1]:], nil
		}
		return "", fmt.Errorf("ooxml: paragraph has no open tag")
	})
}

// tagAttr extracts attr="value" from one raw tag.
func tagAttr(tag, attr string) string {
	var re *regexp.Regexp
	switch attr {
	case "Id":
		re = attrIDRe
	case "Target":
		re = attrTargetRe
	case "name":
		re = attrNameRe
	case "r:id":
		re = attrRIdRe
	case "r":
		re = attrRRe
	case "s":
		re = attrSRe
	case "cm":
		re = attrCmRe
	case "vm":
		re = attrVmRe
	case "t":
		re = attrTRe
	case "numFmtId":
		re = attrNumFmtIDRe
	case "formatCode":
		re = attrFormatCodeRe
	default:
		needle := attr + `="`
		idx := strings.Index(tag, needle)
		if idx < 0 {
			return ""
		}
		rest := tag[idx+len(needle):]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			return ""
		}
		return rest[:end]
	}
	if m := re.FindStringSubmatch(tag); m != nil {
		return m[1]
	}
	return ""
}

var (
	sheetTagRe      = regexp.MustCompile(`<sheet\b[^>]*>`)
	relTagRe        = regexp.MustCompile(`<Relationship\b[^>]*>`)
	rowRe           = regexp.MustCompile(`(?s)<row\b[^>]*>.*?</row>`)
	rowOpenRe       = regexp.MustCompile(`<row\b[^>]*>`)
	cellTagRe       = regexp.MustCompile(`(?s)<c\b[^>]*/>|<c\b[^>]*>.*?</c>`)
	sheetDataSelfRe = regexp.MustCompile(`<sheetData\b[^>]*/>`)
)

// sheetPartName maps a workbook sheet name to its sheet part via
// xl/workbook.xml + xl/_rels/workbook.xml.rels.
func sheetPartName(entries []zipEntry, sheet string) (string, error) {
	wb, err := findPart(entries, "xl/workbook.xml")
	if err != nil {
		return "", err
	}
	rels, err := findPart(entries, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return "", err
	}
	if err := wellFormed(wb); err != nil {
		return "", fmt.Errorf("ooxml: malformed xl/workbook.xml: %w", err)
	}
	targetByID := map[string]string{}
	for _, tag := range relTagRe.FindAllString(string(rels), -1) {
		if id, tgt := tagAttr(tag, "Id"), tagAttr(tag, "Target"); id != "" && tgt != "" {
			targetByID[id] = strings.TrimPrefix(tgt, "/")
		}
	}
	sheets := sheetTagRe.FindAllString(string(wb), -1)
	if len(sheets) == 0 {
		return "", fmt.Errorf("ooxml: xl/workbook.xml defines no sheets")
	}
	pick := ""
	if sheet == "" {
		for _, tag := range sheets {
			if id := tagAttr(tag, "r:id"); id != "" {
				if tgt, ok := targetByID[id]; ok {
					pick = tgt
					break
				}
			}
		}
	} else {
		for _, tag := range sheets {
			if tagAttr(tag, "name") == sheet {
				pick = targetByID[tagAttr(tag, "r:id")]
				break
			}
		}
		if pick == "" {
			return "", fmt.Errorf("ooxml: sheet not found: %s", sheet)
		}
	}
	if pick == "" {
		return "", fmt.Errorf("ooxml: sheet %q has no relationship target", sheet)
	}
	part := "xl/" + strings.TrimPrefix(pick, "xl/")
	if _, err := findPart(entries, part); err != nil {
		return "", fmt.Errorf("ooxml: required part missing: %s", part)
	}
	return part, nil
}

// colNum converts Excel column letters (A-XFD) to a 1-based number.
func colNum(letters string) int {
	n := 0
	for i := 0; i < len(letters); i++ {
		n = n*26 + int(letters[i]-'A'+1)
	}
	return n
}

// splitCellRef splits an uppercase A1 ref into column letters and row number.
func splitCellRef(ref string) (string, int) {
	i := 0
	for i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z' {
		i++
	}
	row, _ := strconv.Atoi(ref[i:])
	return ref[:i], row
}

// SetCellValue edits one A1-style cell of one sheet: creates the cell when
// absent, writes t="str" or a formula element, and never touches
// sharedStrings.xml. numberFormat (an Excel format code, "" = keep) is
// materialized into xl/styles.xml as a new numFmt/xf pair when needed.
// Existing s/cm/vm attributes are preserved unless overridden by a new style.
func SetCellValue(src []byte, sheet, cell, value string, formula bool, numberFormat string) ([]byte, error) {
	m := cellRefRe.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(cell)))
	if m == nil {
		return nil, fmt.Errorf("ooxml: invalid cell reference: %s", cell)
	}
	ref, rowNum := m[0], m[2]
	letters, rowInt := splitCellRef(ref)
	cn := colNum(letters)
	if cn < 1 || cn > 16384 {
		return nil, fmt.Errorf("ooxml: invalid cell reference: %s", cell)
	}
	if rowInt < 1 || rowInt > 1048576 {
		return nil, fmt.Errorf("ooxml: invalid cell reference: %s", cell)
	}
	entries, err := readEditEntries(src)
	if err != nil {
		return nil, err
	}
	part, err := sheetPartName(entries, sheet)
	if err != nil {
		return nil, err
	}
	sheetXML, err := findPart(entries, part)
	if err != nil {
		return nil, err
	}
	if err := wellFormed(sheetXML); err != nil {
		return nil, fmt.Errorf("ooxml: malformed %s: %w", part, err)
	}
	changed := map[string][]byte{}
	newStyleAttr := ""
	if numberFormat != "" {
		styles, err := findPart(entries, "xl/styles.xml")
		if err != nil {
			return nil, err
		}
		xf, newStyles, err := ensureNumberFormat(styles, numberFormat)
		if err != nil {
			return nil, err
		}
		if newStyles != "" {
			changed["xl/styles.xml"] = []byte(newStyles)
		}
		newStyleAttr = ` s="` + strconv.Itoa(xf) + `"`
	}
	buildCell := func(extraAttrs string) string {
		if formula {
			f := strings.TrimPrefix(value, "=")
			return `<c r="` + ref + `"` + extraAttrs + `><f>` + xmlEscape(f) + `</f></c>`
		}
		return `<c r="` + ref + `" t="str"` + extraAttrs + `><v>` + xmlEscape(value) + `</v></c>`
	}
	s := string(sheetXML)
	rowLocs := rowRe.FindAllStringIndex(s, -1)
	rowIdx := -1
	for i, loc := range rowLocs {
		if open := rowOpenRe.FindString(s[loc[0]:loc[1]]); tagAttr(open, "r") == rowNum {
			rowIdx = i
			break
		}
	}
	if rowIdx >= 0 {
		loc := rowLocs[rowIdx]
		row := s[loc[0]:loc[1]]
		cellLocs := cellTagRe.FindAllStringIndex(row, -1)
		hit := -1
		for i := range cellLocs {
			cl := cellLocs[i]
			endRel := strings.Index(row[cl[0]:], ">")
			if endRel < 0 {
				continue
			}
			open := row[cl[0] : cl[0]+endRel+1]
			if tagAttr(open, "r") == ref {
				hit = i
				break
			}
		}
		var newCell string
		if hit >= 0 {
			cl := cellLocs[hit]
			endRel := strings.Index(row[cl[0]:], ">")
			open := row[cl[0] : cl[0]+endRel+1]
			attrs := ""
			if newStyleAttr != "" {
				attrs = newStyleAttr
			} else if v := tagAttr(open, "s"); v != "" {
				attrs = ` s="` + v + `"`
			}
			if v := tagAttr(open, "cm"); v != "" {
				attrs += ` cm="` + v + `"`
			}
			if v := tagAttr(open, "vm"); v != "" {
				attrs += ` vm="` + v + `"`
			}
			newCell = buildCell(attrs)
			row = row[:cl[0]] + newCell + row[cl[1]:]
		} else {
			newCell = buildCell(newStyleAttr)
			newCol := cn
			pos := -1
			for _, cl := range cellLocs {
				endRel := strings.Index(row[cl[0]:], ">")
				if endRel < 0 {
					continue
				}
				open := row[cl[0] : cl[0]+endRel+1]
				er := tagAttr(open, "r")
				el, _ := splitCellRef(er)
				if el == "" {
					continue
				}
				if colNum(el) > newCol {
					pos = cl[0]
					break
				}
			}
			if pos >= 0 {
				row = row[:pos] + newCell + row[pos:]
			} else {
				closeIdx := strings.LastIndex(row, "</row>")
				if closeIdx < 0 {
					return nil, fmt.Errorf("ooxml: malformed row %s in %s", rowNum, part)
				}
				row = row[:closeIdx] + newCell + row[closeIdx:]
			}
		}
		s = s[:loc[0]] + row + s[loc[1]:]
	} else {
		newCell := buildCell(newStyleAttr)
		newRow := `<row r="` + rowNum + `">` + newCell + `</row>`
		if closeIdx := strings.LastIndex(s, "</sheetData>"); closeIdx >= 0 {
			newRowNum, _ := strconv.Atoi(rowNum)
			pos := -1
			for _, loc := range rowLocs {
				open := rowOpenRe.FindString(s[loc[0]:loc[1]])
				if rn, _ := strconv.Atoi(tagAttr(open, "r")); rn > newRowNum {
					pos = loc[0]
					break
				}
			}
			if pos >= 0 {
				s = s[:pos] + newRow + s[pos:]
			} else {
				s = s[:closeIdx] + newRow + s[closeIdx:]
			}
		} else if loc := sheetDataSelfRe.FindStringIndex(s); loc != nil {
			selfTag := s[loc[0]:loc[1]]
			open := strings.TrimSuffix(selfTag, "/>") + ">"
			s = s[:loc[0]] + open + newRow + "</sheetData>" + s[loc[1]:]
		} else {
			return nil, fmt.Errorf("ooxml: %s has no <sheetData>", part)
		}
	}
	if formula {
		wb, err := findPart(entries, "xl/workbook.xml")
		if err != nil {
			return nil, err
		}
		wbs := string(wb)
		if !strings.Contains(wbs, "<calcPr") {
			idx := strings.LastIndex(wbs, "</workbook>")
			if idx < 0 {
				return nil, fmt.Errorf("ooxml: xl/workbook.xml has no </workbook>")
			}
			wbs = wbs[:idx] + `<calcPr calcId="0" fullCalcOnLoad="1"/>` + wbs[idx:]
			if err := wellFormed([]byte(wbs)); err != nil {
				return nil, fmt.Errorf("ooxml: edit produced malformed xl/workbook.xml: %w", err)
			}
			changed["xl/workbook.xml"] = []byte(wbs)
		}
	}
	if err := wellFormed([]byte(s)); err != nil {
		return nil, fmt.Errorf("ooxml: edit produced malformed %s: %w", part, err)
	}
	changed[part] = []byte(s)
	return writeEditEntries(entries, changed)
}

var (
	numFmtsBlockRe   = regexp.MustCompile(`(?s)<numFmts\b[^>]*>.*?</numFmts>`)
	numFmtsSelfRe    = regexp.MustCompile(`<numFmts\b[^>]*/>`)
	numFmtsOpenRe    = regexp.MustCompile(`<numFmts\b[^>]*>`)
	numFmtRe         = regexp.MustCompile(`<numFmt\b[^>]*>`)
	cellXfsRe        = regexp.MustCompile(`(?s)<cellXfs\b[^>]*>.*?</cellXfs>`)
	cellXfsSelfRe    = regexp.MustCompile(`<cellXfs\b[^>]*/>`)
	cellXfsOpenRe    = regexp.MustCompile(`<cellXfs\b[^>]*>`)
	xfOpenRe         = regexp.MustCompile(`<xf\b[^>]*>`)
	countRe          = regexp.MustCompile(`count="(\d+)"`)
	styleSheetOpenRe = regexp.MustCompile(`<styleSheet\b[^>]*>`)
)

// bumpCount increments count="N" in block, or inserts count when absent.
func bumpCount(block, openPat string, n int, openRe *regexp.Regexp) string {
	_ = openPat
	if countRe.MatchString(block) {
		return countRe.ReplaceAllStringFunc(block, func(m string) string {
			sub := countRe.FindStringSubmatch(m)
			c, _ := strconv.Atoi(sub[1])
			return `count="` + strconv.Itoa(c+1) + `"`
		})
	}
	return openRe.ReplaceAllStringFunc(block, func(open string) string {
		if strings.HasSuffix(open, "/>") {
			return strings.TrimSuffix(open, "/>") + ` count="` + strconv.Itoa(n) + `"/>`
		}
		return strings.TrimSuffix(open, ">") + ` count="` + strconv.Itoa(n) + `">`
	})
}

// ensureNumberFormat returns the cellXfs index for formatCode, extending
// styles.xml when needed. newStyles is "" when nothing changed.
func ensureNumberFormat(styles []byte, formatCode string) (int, string, error) {
	if err := wellFormed(styles); err != nil {
		return 0, "", fmt.Errorf("ooxml: malformed xl/styles.xml: %w", err)
	}
	s := string(styles)
	changed := false
	maxID, idFor := 163, -1
	for _, tag := range numFmtRe.FindAllString(s, -1) {
		id, _ := strconv.Atoi(tagAttr(tag, "numFmtId"))
		if id > maxID {
			maxID = id
		}
		if tagAttr(tag, "formatCode") == formatCode {
			idFor = id
		}
	}
	if idFor < 0 {
		idFor = maxID + 1
		tag := `<numFmt numFmtId="` + strconv.Itoa(idFor) + `" formatCode="` + xmlEscape(formatCode) + `"/>`
		if loc := numFmtsBlockRe.FindStringIndex(s); loc != nil {
			block := s[loc[0]:loc[1]]
			n := len(numFmtRe.FindAllString(block, -1)) + 1
			block = bumpCount(block, "numFmts", n, numFmtsOpenRe)
			block = strings.Replace(block, "</numFmts>", tag+"</numFmts>", 1)
			s = s[:loc[0]] + block + s[loc[1]:]
		} else if loc := numFmtsSelfRe.FindStringIndex(s); loc != nil {
			s = s[:loc[0]] + `<numFmts count="1">` + tag + `</numFmts>` + s[loc[1]:]
		} else {
			open := styleSheetOpenRe.FindStringIndex(s)
			if open == nil {
				return 0, "", fmt.Errorf("ooxml: xl/styles.xml has no <styleSheet>")
			}
			s = s[:open[1]] + `<numFmts count="1">` + tag + `</numFmts>` + s[open[1]:]
		}
		changed = true
	}
	if loc := cellXfsRe.FindStringIndex(s); loc != nil {
		block := s[loc[0]:loc[1]]
		xfs := xfOpenRe.FindAllString(block, -1)
		for i, tag := range xfs {
			if id, _ := strconv.Atoi(tagAttr(tag, "numFmtId")); id == idFor {
				if !changed {
					return i, "", nil
				}
				return i, s, nil
			}
		}
		idx := len(xfs)
		tag := `<xf numFmtId="` + strconv.Itoa(idFor) + `" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>`
		block = bumpCount(block, "cellXfs", idx+1, cellXfsOpenRe)
		block = strings.Replace(block, "</cellXfs>", tag+"</cellXfs>", 1)
		s = s[:loc[0]] + block + s[loc[1]:]
		if err := wellFormed([]byte(s)); err != nil {
			return 0, "", fmt.Errorf("ooxml: edit produced malformed xl/styles.xml: %w", err)
		}
		return idx, s, nil
	}
	if loc := cellXfsSelfRe.FindStringIndex(s); loc != nil {
		tag := `<xf numFmtId="` + strconv.Itoa(idFor) + `" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>`
		s = s[:loc[0]] + `<cellXfs count="1">` + tag + `</cellXfs>` + s[loc[1]:]
		if err := wellFormed([]byte(s)); err != nil {
			return 0, "", fmt.Errorf("ooxml: edit produced malformed xl/styles.xml: %w", err)
		}
		return 0, s, nil
	}
	return 0, "", fmt.Errorf("ooxml: xl/styles.xml has no <cellXfs>")
}
