package analyzer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/cam/mcp-onlyoffice/internal/storage"
)

type Analysis struct {
	ID         string        `json:"id"`
	Filename   string        `json:"filename"`
	MimeType   string        `json:"mime_type"`
	Size       int64         `json:"size"`
	Format     string        `json:"format"`
	Valid      bool          `json:"valid"`
	Issues     []Issue       `json:"issues,omitempty"`
	Docx       *DocxAnalysis `json:"docx,omitempty"`
	Xlsx       *XlsxAnalysis `json:"xlsx,omitempty"`
	Pptx       *PptxAnalysis `json:"pptx,omitempty"`
}

type Issue struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type DocxAnalysis struct {
	Paragraphs int      `json:"paragraphs"`
	WordCount  int      `json:"word_count"`
	Headings   []string `json:"headings,omitempty"`
	HasTable   bool     `json:"has_table"`
}

type XlsxAnalysis struct {
	Sheets    int      `json:"sheets"`
	Rows      int      `json:"rows"`
	Cols      int      `json:"cols"`
	Headers   []string `json:"headers,omitempty"`
	EmptyCols int      `json:"empty_cols,omitempty"`
}

type PptxAnalysis struct {
	Slides  int      `json:"slides"`
	Titles  []string `json:"titles,omitempty"`
	Content bool     `json:"has_content"`
}

type Analyzer struct {
	store storage.Store
}

func New(store storage.Store) *Analyzer {
	return &Analyzer{store: store}
}

func (a *Analyzer) Analyze(id string) (*Analysis, error) {
	doc, err := a.store.Get(id)
	if err != nil {
		return nil, fmt.Errorf("get doc: %w", err)
	}

	data, err := a.store.GetData(id)
	if err != nil {
		return nil, fmt.Errorf("get data: %w", err)
	}

	analysis := &Analysis{
		ID:       doc.ID,
		Filename: doc.Filename,
		MimeType: doc.MimeType,
		Size:     doc.Size,
	}

	switch {
	case strings.HasSuffix(strings.ToLower(doc.Filename), ".docx"):
		analysis.Format = "docx"
		a.analyzeDocx(data, analysis)
	case strings.HasSuffix(strings.ToLower(doc.Filename), ".xlsx"):
		analysis.Format = "xlsx"
		a.analyzeXlsx(data, analysis)
	case strings.HasSuffix(strings.ToLower(doc.Filename), ".pptx"):
		analysis.Format = "pptx"
		a.analyzePptx(data, analysis)
	case strings.HasSuffix(strings.ToLower(doc.Filename), ".csv"):
		analysis.Format = "csv"
		analysis.Valid = true
	default:
		analysis.Format = "unknown"
		analysis.Valid = true
	}

	return analysis, nil
}

func (a *Analyzer) analyzeDocx(data []byte, analysis *Analysis) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		analysis.Issues = append(analysis.Issues, Issue{Severity: "error", Message: "invalid zip: " + err.Error()})
		return
	}

	analysis.Valid = true
	dx := &DocxAnalysis{}
	hasDocument := false

	for _, f := range zr.File {
		switch {
		case f.Name == "word/document.xml":
			hasDocument = true
			rc, err := f.Open()
			if err != nil {
				analysis.Issues = append(analysis.Issues, Issue{Severity: "error", Message: "cannot open document.xml: " + err.Error()})
				continue
			}
			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				analysis.Issues = append(analysis.Issues, Issue{Severity: "warn", Message: "cannot read document.xml: " + err.Error()})
				continue
			}
			dx.analyzeXML(string(content))

		case f.Name == "word/document.xml" && f.UncompressedSize == 0:
			analysis.Issues = append(analysis.Issues, Issue{Severity: "warn", Message: "document.xml is empty"})
		}
	}

	if !hasDocument {
		analysis.Issues = append(analysis.Issues, Issue{Severity: "error", Message: "missing word/document.xml"})
	}

	analysis.Docx = dx
}

func (dx *DocxAnalysis) analyzeXML(content string) {
	dx.Paragraphs = strings.Count(content, "<w:p")
	dx.HasTable = strings.Contains(content, "<w:tbl")

	re := regexp.MustCompile(`<w:t[^>]*>([^<]+)</w:t>`)
	matches := re.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		text := strings.TrimSpace(m[1])
		if text == "" {
			continue
		}
		dx.WordCount += len(strings.Fields(text))
	}

	headingRe := regexp.MustCompile(`<w:pStyle w:val="Heading(\d)"/>`)
	headingMatches := headingRe.FindAllStringSubmatch(content, -1)
	for _, m := range headingMatches {
		dx.Headings = append(dx.Headings, "Heading "+m[1])
	}
}

func (a *Analyzer) analyzeXlsx(data []byte, analysis *Analysis) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		analysis.Issues = append(analysis.Issues, Issue{Severity: "error", Message: "invalid zip: " + err.Error()})
		return
	}

	analysis.Valid = true
	xa := &XlsxAnalysis{}

	for _, f := range zr.File {
		if strings.Contains(f.Name, "worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			xa.Sheets++
			rc, err := f.Open()
			if err != nil {
				continue
			}
			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			xa.countCells(string(content))
		}
	}

	if xa.Sheets == 0 {
		analysis.Issues = append(analysis.Issues, Issue{Severity: "error", Message: "no worksheets found"})
	}

	analysis.Xlsx = xa
}

func (xa *XlsxAnalysis) countCells(content string) {
	rowCount := strings.Count(content, "<row")
	xa.Rows += rowCount

	cellRe := regexp.MustCompile(`<c r="([A-Z]+)\d+`)
	cols := map[string]bool{}
	for _, m := range cellRe.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 {
			cols[m[1]] = true
		}
	}
	if len(cols) > xa.Cols {
		xa.Cols = len(cols)
	}

	if xa.Headers == nil {
		headerRe := regexp.MustCompile(`<c r="[A-Z]+1".*?<v>([^<]+)`)
		headerMatches := headerRe.FindAllStringSubmatch(content, -1)
		for _, m := range headerMatches {
			xa.Headers = append(xa.Headers, m[1])
		}
	}

	emptyRe := regexp.MustCompile(`<c r="[A-Z]+\d+"\s*/>`)
	xa.EmptyCols += len(emptyRe.FindAllString(content, -1))
}

func (a *Analyzer) analyzePptx(data []byte, analysis *Analysis) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		analysis.Issues = append(analysis.Issues, Issue{Severity: "error", Message: "invalid zip: " + err.Error()})
		return
	}

	analysis.Valid = true
	pa := &PptxAnalysis{}

	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			pa.Slides++
			rc, err := f.Open()
			if err != nil {
				continue
			}
			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			pa.extractSlideInfo(string(content))
		}
	}

	if pa.Slides == 0 {
		analysis.Issues = append(analysis.Issues, Issue{Severity: "error", Message: "no slides found"})
	}

	analysis.Pptx = pa
}

func (pa *PptxAnalysis) extractSlideInfo(content string) {
	titleRe := regexp.MustCompile(`<a:t>([^<]+)</a:t>`)
	matches := titleRe.FindAllStringSubmatch(content, -1)
	for i, m := range matches {
		text := strings.TrimSpace(m[1])
		if text == "" {
			continue
		}
		if i == 0 {
			pa.Titles = append(pa.Titles, text)
		}
		pa.Content = true
	}
}

func DetectInconsistencies(analysis *Analysis) []Issue {
	var issues []Issue

	if analysis.Docx != nil {
		if analysis.Docx.Paragraphs == 0 {
			issues = append(issues, Issue{Severity: "warn", Message: "Document has no paragraphs"})
		}
		if analysis.Docx.WordCount < 10 {
			issues = append(issues, Issue{Severity: "info", Message: "Document is very short"})
		}
	}

	if analysis.Xlsx != nil {
		if analysis.Xlsx.Rows > 0 && len(analysis.Xlsx.Headers) == 0 {
			issues = append(issues, Issue{Severity: "info", Message: "Spreadsheet has no headers in first row"})
		}
	}

	if analysis.Pptx != nil {
		if analysis.Pptx.Slides == 1 {
			issues = append(issues, Issue{Severity: "info", Message: "Presentation has only one slide"})
		}
		if len(analysis.Pptx.Titles) < analysis.Pptx.Slides {
			issues = append(issues, Issue{Severity: "warn", Message: "Some slides may be missing titles"})
		}
	}

	return issues
}

func SuggestImprovements(analysis *Analysis) []string {
	var suggestions []string
	issues := DetectInconsistencies(analysis)
	for _, iss := range issues {
		suggestions = append(suggestions, iss.Message)
	}
	return suggestions
}
