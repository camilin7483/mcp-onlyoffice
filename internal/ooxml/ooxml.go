package ooxml

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func xmlEscape(s string) string {
	s = stripIllegalXMLChars(s)
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// stripIllegalXMLChars removes characters forbidden by XML 1.0 so user
// text can never produce malformed parts (control chars from input).
// A well-encoded U+FFFD (EF BF BD, size 3 on decode) is legal text and is
// kept; only the RuneError produced by INVALID byte sequences (size 1)
// is dropped.
func stripIllegalXMLChars(s string) string {
	if strings.IndexFunc(s, func(r rune) bool { return !isLegalXMLChar(r) }) < 0 {
		// Fast path missed lone surrogates/invalid bytes decoded as
		// RuneError size 1 — fall through to the byte-precise loop.
		if utf8.ValidString(s) {
			return s
		}
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			i++ // skip one invalid byte, keep scanning
			continue
		}
		if isLegalXMLChar(r) {
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}

func isLegalXMLChar(r rune) bool {
	if r == 0x09 || r == 0x0A || r == 0x0D {
		return true
	}
	if r < 0x20 || r == 0xFFFE || r == 0xFFFF || (r >= 0xD800 && r <= 0xDFFF) {
		return false
	}
	// NOTE: U+FFFD (== utf8.RuneError) is legal when well-encoded
	// (decoded with size 3); invalid byte sequences never reach here
	// as runes — stripIllegalXMLChars drops them by decode size.
	return r <= 0x10FFFF
}

// writeZipOrdered packs parts in sorted order so output is deterministic
// ([Content_Types].xml lands first, as strict readers expect).
func writeZipOrdered(files map[string]string) ([]byte, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", name, err)
		}
		if _, err := f.Write([]byte(files[name])); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}
	return buf.Bytes(), nil
}

func packageRels(officeTarget string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="%s"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>
</Relationships>`, officeTarget)
}

func docPropsContentTypes() string {
	return `  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
  <Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>
`
}

func docPropsCore(title string) string {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <dc:title>%s</dc:title>
  <dc:creator>mcp-onlyoffice</dc:creator>
  <cp:lastModifiedBy>mcp-onlyoffice</cp:lastModifiedBy>
  <dcterms:created xsi:type="dcterms:W3CDTF">%s</dcterms:created>
  <dcterms:modified xsi:type="dcterms:W3CDTF">%s</dcterms:modified>
</cp:coreProperties>`, xmlEscape(title), now, now)
}

func docPropsApp() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">
  <Application>mcp-onlyoffice</Application>
  <TotalTime>0</TotalTime>
</Properties>`
}

// docSectPr gives Word a default page definition; without it ONLYOFFICE
// Desktop opens the file with a "repair" prompt.
const docSectPr = `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720"/></w:sectPr>`

// sanitizeSheetName strips characters Excel forbids in sheet names.
func sanitizeSheetName(name string) string {
	name = stripIllegalXMLChars(strings.TrimSpace(name))
	for _, bad := range []string{"[", "]", ":", "*", "?", "/", "\\"} {
		name = strings.ReplaceAll(name, bad, "_")
	}
	if name == "" {
		return "Sheet1"
	}
	if len([]rune(name)) > 31 {
		name = string([]rune(name)[:31])
	}
	return name
}

func colName(n int) string {
	s := ""
	for n >= 0 {
		s = string(rune('A'+n%26)) + s
		n = n/26 - 1
	}
	return s
}

type DocxParagraph struct {
	Text  string
	Bold  bool
	Size  int
	Align string
}

type SlideData struct {
	Title   string
	Content string
}

type TableData struct {
	Headers []string
	Rows    [][]string
}
