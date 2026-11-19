package ooxml

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
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

const maxEditBytes = 64 << 20 // 64 MiB input cap for every edit function

var (
	paraRe    = regexp.MustCompile(`(?s)<w:p\b[^>]*>.*?</w:p>`)
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

// zipEntry is one package part carried through an edit.
type zipEntry struct {
	name   string
	method uint16
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
	entries := make([]zipEntry, 0, len(zr.File))
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
		entries = append(entries, zipEntry{name: f.Name, method: f.Method, data: data})
	}
	return entries, nil
}

// writeEditEntries repacks entries in their original order, swapping in
// changed parts. Untouched parts keep method and exact content bytes.
func writeEditEntries(entries []zipEntry, changed map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		data := e.data
		if replacement, ok := changed[e.name]; ok {
			data = replacement
		}
		fh := &zip.FileHeader{Name: e.name, Method: e.method}
		fh.SetMode(0o644)
		f, err := w.CreateHeader(fh)
		if err != nil {
			return nil, fmt.Errorf("ooxml: create %s: %w", e.name, err)
		}
		if _, err := f.Write(data); err != nil {
			return nil, fmt.Errorf("ooxml: write %s: %w", e.name, err)
		}
	}
	// changed may name a new part (not used by current edits, kept for symmetry).
	names := make([]string, 0, len(changed))
	for name := range changed {
		known := false
		for _, e := range entries {
			if e.name == name {
				known = true
				break
			}
		}
		if !known {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			return nil, fmt.Errorf("ooxml: create %s: %w", name, err)
		}
		if _, err := f.Write(changed[name]); err != nil {
			return nil, fmt.Errorf("ooxml: write %s: %w", name, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("ooxml: close package: %w", err)
	}
	return buf.Bytes(), nil
}

func findPart(entries []zipEntry, name string) ([]byte, error) {
	for _, e := range entries {
		if e.name == name {
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

// replaceParaText keeps w:pPr and the first run's w:rPr, puts text into the
// first text run, drops the remaining text runs, keeps non-text runs.
func replaceParaText(para, text string) string {
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
	re := regexp.MustCompile(regexp.QuoteMeta(attr) + `="([^"]*)"`)
	if m := re.FindStringSubmatch(tag); m != nil {
		return m[1]
	}
	return ""
}

var (
	sheetTagRe = regexp.MustCompile(`<sheet\b[^>]*>`)
	relTagRe   = regexp.MustCompile(`<Relationship\b[^>]*>`)
	rowRe      = regexp.MustCompile(`(?s)<row\b[^>]*>.*?</row>`)
	rowOpenRe  = regexp.MustCompile(`<row\b[^>]*>`)
	cellTagRe  = regexp.MustCompile(`(?s)<c\b[^>]*>.*?</c>|<c\b[^>]*/>`)
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

// SetCellValue edits one A1-style cell of one sheet: creates the cell when
// absent, writes t="str" or a formula element, and never touches
// sharedStrings.xml. numberFormat (an Excel format code, "" = keep) is
// materialized into xl/styles.xml as a new numFmt/xf pair when needed.
func SetCellValue(src []byte, sheet, cell, value string, formula bool, numberFormat string) ([]byte, error) {
	m := cellRefRe.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(cell)))
	if m == nil {
		return nil, fmt.Errorf("ooxml: invalid cell reference: %s", cell)
	}
	ref, rowNum := m[0], m[2]
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
	styleAttr := ""
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
		styleAttr = ` s="` + strconv.Itoa(xf) + `"`
	}
	newCell := ""
	if formula {
		f := strings.TrimPrefix(value, "=")
		newCell = `<c r="` + ref + `"` + styleAttr + `><f>` + xmlEscape(f) + `</f><v>0</v></c>`
	} else {
		newCell = `<c r="` + ref + `" t="str"` + styleAttr + `><v>` + xmlEscape(value) + `</v></c>`
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
			if open := row[cl[0] : strings.Index(row[cl[0]:], ">")+cl[0]+1]; tagAttr(open, "r") == ref {
				hit = i
				break
			}
		}
		if hit >= 0 {
			cl := cellLocs[hit]
			row = row[:cl[0]] + newCell + row[cl[1]:]
		} else {
			closeIdx := strings.LastIndex(row, "</row>")
			if closeIdx < 0 {
				return nil, fmt.Errorf("ooxml: malformed row %s in %s", rowNum, part)
			}
			row = row[:closeIdx] + newCell + row[closeIdx:]
		}
		s = s[:loc[0]] + row + s[loc[1]:]
	} else {
		newRow := `<row r="` + rowNum + `">` + newCell + `</row>`
		closeIdx := strings.LastIndex(s, "</sheetData>")
		if closeIdx < 0 {
			return nil, fmt.Errorf("ooxml: %s has no <sheetData>", part)
		}
		s = s[:closeIdx] + newRow + s[closeIdx:]
	}
	if err := wellFormed([]byte(s)); err != nil {
		return nil, fmt.Errorf("ooxml: edit produced malformed %s: %w", part, err)
	}
	changed[part] = []byte(s)
	return writeEditEntries(entries, changed)
}

var (
	numFmtsBlockRe = regexp.MustCompile(`(?s)<numFmts\b[^>]*>.*?</numFmts>`)
	numFmtRe       = regexp.MustCompile(`<numFmt\b[^>]*>`)
	cellXfsRe      = regexp.MustCompile(`(?s)<cellXfs\b[^>]*>.*?</cellXfs>`)
	xfOpenRe       = regexp.MustCompile(`<xf\b[^>]*>`)
	countRe        = regexp.MustCompile(`count="(\d+)"`)
)

// ensureNumberFormat returns the cellXfs index for formatCode, extending
// styles.xml when needed. newStyles is "" when nothing changed.
func ensureNumberFormat(styles []byte, formatCode string) (int, string, error) {
	if err := wellFormed(styles); err != nil {
		return 0, "", fmt.Errorf("ooxml: malformed xl/styles.xml: %w", err)
	}
	s := string(styles)
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
			block = countRe.ReplaceAllStringFunc(block, func(m string) string {
				n, _ := strconv.Atoi(countRe.FindStringSubmatch(m)[1])
				return `count="` + strconv.Itoa(n+1) + `"`
			})
			block = strings.Replace(block, "</numFmts>", tag+"</numFmts>", 1)
			s = s[:loc[0]] + block + s[loc[1]:]
		} else {
			open := regexp.MustCompile(`<styleSheet\b[^>]*>`).FindStringIndex(s)
			if open == nil {
				return 0, "", fmt.Errorf("ooxml: xl/styles.xml has no <styleSheet>")
			}
			s = s[:open[1]] + `<numFmts count="1">` + tag + `</numFmts>` + s[open[1]:]
		}
	}
	loc := cellXfsRe.FindStringIndex(s)
	if loc == nil {
		return 0, "", fmt.Errorf("ooxml: xl/styles.xml has no <cellXfs>")
	}
	block := s[loc[0]:loc[1]]
	xfs := xfOpenRe.FindAllString(block, -1)
	for i, tag := range xfs {
		if id, _ := strconv.Atoi(tagAttr(tag, "numFmtId")); id == idFor {
			if s == string(styles) {
				return i, "", nil
			}
			return i, s, nil
		}
	}
	idx := len(xfs)
	tag := `<xf numFmtId="` + strconv.Itoa(idFor) + `" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>`
	block = countRe.ReplaceAllStringFunc(block, func(m string) string {
		n, _ := strconv.Atoi(countRe.FindStringSubmatch(m)[1])
		return `count="` + strconv.Itoa(n+1) + `"`
	})
	block = strings.Replace(block, "</cellXfs>", tag+"</cellXfs>", 1)
	s = s[:loc[0]] + block + s[loc[1]:]
	if err := wellFormed([]byte(s)); err != nil {
		return 0, "", fmt.Errorf("ooxml: edit produced malformed xl/styles.xml: %w", err)
	}
	return idx, s, nil
}
