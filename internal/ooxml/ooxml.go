package ooxml

import (
	"strings"
)

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
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
