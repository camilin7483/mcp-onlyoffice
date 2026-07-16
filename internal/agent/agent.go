package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/ooxml"
	"github.com/cam/mcp-onlyoffice/internal/storage"
)

type SectionType string

const (
	SectionText  SectionType = "text"
	SectionTable SectionType = "table"
)

type Section struct {
	Type    SectionType `json:"type"`
	Heading string      `json:"heading"`
	Content string      `json:"content,omitempty"`
	Headers []string    `json:"headers,omitempty"`
	Rows    [][]string  `json:"rows,omitempty"`
}

type AgentTask struct {
	Task     string    `json:"task"`
	Title    string    `json:"title"`
	Sections []Section `json:"sections,omitempty"`
}

type AgentResult struct {
	Documents []AgentDocument `json:"documents"`
	Duration  string          `json:"duration"`
}

type AgentDocument struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"created_at"`
}

type DocumentAgent struct {
	store storage.Store
}

func New(store storage.Store) *DocumentAgent {
	return &DocumentAgent{store: store}
}

func (a *DocumentAgent) Execute(ctx context.Context, task *AgentTask) (*AgentResult, error) {
	start := time.Now()

	var res *AgentResult
	var err error

	switch task.Task {
	case "create_report":
		res, err = a.createReport(ctx, task)
	default:
		return nil, fmt.Errorf("unknown task: %s", task.Task)
	}

	if err != nil {
		return nil, err
	}

	res.Duration = time.Since(start).Round(time.Millisecond).String()
	return res, nil
}

func (a *DocumentAgent) createReport(ctx context.Context, task *AgentTask) (*AgentResult, error) {
	if task.Title == "" {
		task.Title = "Untitled Report"
	}

	paras := []ooxml.DocxParagraph{
		{Text: task.Title, Size: 28, Bold: true},
	}

	for _, s := range task.Sections {
		if s.Heading != "" {
			paras = append(paras, ooxml.DocxParagraph{Text: s.Heading, Size: 24, Bold: true})
		}
		switch s.Type {
		case SectionText:
			if s.Content != "" {
				for _, line := range strings.Split(s.Content, "\n") {
					if line != "" {
						paras = append(paras, ooxml.DocxParagraph{Text: line, Size: 22})
					}
				}
			}
		case SectionTable:
			if len(s.Headers) > 0 || len(s.Rows) > 0 {
				docxData, err := ooxml.CreateDOCXWithTable(s.Heading, ooxml.TableData{Headers: s.Headers, Rows: s.Rows})
				if err != nil {
					return nil, fmt.Errorf("create table docx: %w", err)
				}
				filename := sanitizeFilename(s.Heading) + ".docx"
				if _, err := a.store.Create(filename, docxData); err != nil {
					return nil, fmt.Errorf("store table docx: %w", err)
				}
			}
		}
	}

	docxData, err := ooxml.CreateDOCXFromParagraphs(paras)
	if err != nil {
		return nil, fmt.Errorf("create docx: %w", err)
	}

	safeName := sanitizeFilename(task.Title)
	filename := safeName + ".docx"
	doc, err := a.store.Create(filename, docxData)
	if err != nil {
		return nil, fmt.Errorf("store docx: %w", err)
	}

	result := &AgentResult{
		Documents: []AgentDocument{
			{
				ID:        doc.ID,
				Filename:  doc.Filename,
				MimeType:  doc.MimeType,
				Size:      doc.Size,
				CreatedAt: doc.CreatedAt.Format(time.RFC3339),
			},
		},
	}

	return result, nil
}

func sanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, name)
	runes := []rune(name)
	if len(runes) > 100 {
		runes = runes[:100]
	}
	return strings.TrimSpace(string(runes))
}

