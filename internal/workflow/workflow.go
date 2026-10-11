package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/agent"
	"github.com/cam/mcp-onlyoffice/internal/ooxml"
	"github.com/cam/mcp-onlyoffice/internal/storage"
)

type StepFunc func(ctx context.Context, params map[string]any, results map[string]any) (any, error)

type StepDef struct {
	Name string
	Run  StepFunc
}

type WorkflowDef struct {
	Name        string
	Description string
	Steps       []StepDef
}

type Engine struct {
	store   storage.Store
	agent   *agent.DocumentAgent
	workflows map[string]*WorkflowDef
}

func NewEngine(store storage.Store, agent *agent.DocumentAgent) *Engine {
	e := &Engine{
		store:     store,
		agent:     agent,
		workflows: make(map[string]*WorkflowDef),
	}
	e.registerBuiltins()
	return e
}

func (e *Engine) Register(w *WorkflowDef) {
	e.workflows[w.Name] = w
}

func (e *Engine) List() []WorkflowDef {
	var list []WorkflowDef
	for _, w := range e.workflows {
		list = append(list, *w)
	}
	return list
}

func (e *Engine) Execute(ctx context.Context, name string, params map[string]any) (*Result, error) {
	w, ok := e.workflows[name]
	if !ok {
		return nil, fmt.Errorf("workflow not found: %s", name)
	}

	start := time.Now()
	results := map[string]any{}
	var stepResults []StepResult

	for _, step := range w.Steps {
		stepStart := time.Now()
		output, err := step.Run(ctx, params, results)
		duration := time.Since(stepStart).Round(time.Millisecond).String()

		if err != nil {
			stepResults = append(stepResults, StepResult{
				Step:     step.Name,
				Error:    err.Error(),
				Duration: duration,
			})
			return &Result{
				Workflow:  name,
				Steps:     stepResults,
				Duration:  time.Since(start).Round(time.Millisecond).String(),
				FailedAt:  step.Name,
			}, nil
		}

		results[step.Name] = output
		stepResults = append(stepResults, StepResult{
			Step:     step.Name,
			Output:   output,
			Duration: duration,
		})
	}

	return &Result{
		Workflow: name,
		Steps:    stepResults,
		Duration: time.Since(start).Round(time.Millisecond).String(),
	}, nil
}

type StepResult struct {
	Step     string `json:"step"`
	Output   any    `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration string `json:"duration"`
}

type Result struct {
	Workflow  string       `json:"workflow"`
	Steps     []StepResult `json:"steps"`
	Duration  string       `json:"duration"`
	FailedAt  string       `json:"failed_at,omitempty"`
}

func (e *Engine) registerBuiltins() {
	e.Register(&WorkflowDef{
		Name:        "create_project_package",
		Description: "Create a complete project package: DOCX report + XLSX data + PPTX presentation",
		Steps: []StepDef{
			{
				Name: "generate_docx",
				Run:  e.stepGenerateDocx,
			},
			{
				Name: "generate_xlsx",
				Run:  e.stepGenerateXlsx,
			},
			{
				Name: "generate_pptx",
				Run:  e.stepGeneratePptx,
			},
			{
				Name: "link_documents",
				Run:  e.stepLinkDocuments,
			},
		},
	})
	e.Register(&WorkflowDef{
		Name:        "batch_create",
		Description: "Create multiple DOCX documents at once from a list of {title, content} items",
		Steps: []StepDef{
			{
				Name: "create_batch",
				Run:  e.stepBatchCreate,
			},
		},
	})
}

func (e *Engine) stepGenerateDocx(ctx context.Context, params map[string]any, results map[string]any) (any, error) {
	title, _ := params["title"].(string)
	if title == "" {
		title = "Project Report"
	}

	content, _ := params["content"].(string)
	data, err := ooxml.CreateDOCXFromText(title, content)
	if err != nil {
		return nil, fmt.Errorf("create docx: %w", err)
	}

	filename := sanitize(title) + ".docx"
	doc, err := e.store.Create(filename, data)
	if err != nil {
		return nil, fmt.Errorf("store docx: %w", err)
	}

	return map[string]any{
		"id":       doc.ID,
		"filename": doc.Filename,
		"size":     doc.Size,
		"mime":     doc.MimeType,
	}, nil
}

func (e *Engine) stepGenerateXlsx(ctx context.Context, params map[string]any, results map[string]any) (any, error) {
	title, _ := params["title"].(string)
	if title == "" {
		title = "Data"
	}

	var headers []string
	if h, ok := params["headers"].([]any); ok {
		for _, v := range h {
			headers = append(headers, fmt.Sprint(v))
		}
	}

	var rows [][]string
	if r, ok := params["rows"].([]any); ok {
		for _, ri := range r {
			if row, ok := ri.([]any); ok {
				var r2 []string
				for _, c := range row {
					r2 = append(r2, fmt.Sprint(c))
				}
				rows = append(rows, r2)
			}
		}
	}

	data, err := ooxml.CreateXLSXFromData(title, headers, rows)
	if err != nil {
		return nil, fmt.Errorf("create xlsx: %w", err)
	}

	filename := sanitize(title) + ".xlsx"
	doc, err := e.store.Create(filename, data)
	if err != nil {
		return nil, fmt.Errorf("store xlsx: %w", err)
	}

	return map[string]any{
		"id":       doc.ID,
		"filename": doc.Filename,
		"size":     doc.Size,
		"mime":     doc.MimeType,
		"rows":     len(rows),
		"cols":     len(headers),
	}, nil
}

func (e *Engine) stepGeneratePptx(ctx context.Context, params map[string]any, results map[string]any) (any, error) {
	title, _ := params["title"].(string)
	if title == "" {
		title = "Presentation"
	}

	var slides []ooxml.SlideData
	if s, ok := params["slides"].([]any); ok {
		for _, si := range s {
			if sm, ok := si.(map[string]any); ok {
				st, _ := sm["title"].(string)
				sc, _ := sm["content"].(string)
				slides = append(slides, ooxml.SlideData{Title: st, Content: sc})
			}
		}
	}
	if len(slides) == 0 {
		slides = []ooxml.SlideData{{Title: title, Content: ""}}
	}

	data, err := ooxml.CreatePPTXFromSlides(slides)
	if err != nil {
		return nil, fmt.Errorf("create pptx: %w", err)
	}

	filename := sanitize(title) + ".pptx"
	doc, err := e.store.Create(filename, data)
	if err != nil {
		return nil, fmt.Errorf("store pptx: %w", err)
	}

	return map[string]any{
		"id":       doc.ID,
		"filename": doc.Filename,
		"size":     doc.Size,
		"mime":     doc.MimeType,
		"slides":   len(slides),
	}, nil
}

func (e *Engine) stepLinkDocuments(ctx context.Context, params map[string]any, results map[string]any) (any, error) {
	var ids []string

	for _, stepName := range []string{"generate_docx", "generate_xlsx", "generate_pptx"} {
		if res, ok := results[stepName]; ok {
			if m, ok := res.(map[string]any); ok {
				if id, ok := m["id"].(string); ok {
					ids = append(ids, id)
				}
			}
		}
	}

	var linked int
	for i, id := range ids {
		for j, other := range ids {
			if i != j {
				if err := e.store.AddRef(id, other); err == nil {
					linked++
				}
			}
		}
	}

	return map[string]any{"linked_documents": len(ids), "cross_references": linked}, nil
}

// stepBatchCreate creates one DOCX per item in params["items"] ({title, content}).
func (e *Engine) stepBatchCreate(ctx context.Context, params map[string]any, results map[string]any) (any, error) {
	raw, _ := params["items"].([]any)
	if len(raw) == 0 {
		return nil, fmt.Errorf("batch_create: params.items is required (non-empty list of {title, content})")
	}
	var created []map[string]any
	var failed []string
	for i, ri := range raw {
		item, _ := ri.(map[string]any)
		if item == nil {
			failed = append(failed, fmt.Sprintf("item_%d: not an object", i))
			continue
		}
		title, _ := item["title"].(string)
		if title == "" {
			title = fmt.Sprintf("Batch Document %d", i+1)
		}
		content, _ := item["content"].(string)
		data, err := ooxml.CreateDOCXFromText(title, content)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", title, err))
			continue
		}
		doc, err := e.store.Create(sanitize(title)+".docx", data)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", title, err))
			continue
		}
		created = append(created, map[string]any{"id": doc.ID, "filename": doc.Filename, "size": doc.Size})
	}
	out := map[string]any{"created": created, "count": len(created)}
	if len(failed) > 0 {
		out["failed"] = failed
	}
	return out, nil
}

func sanitize(name string) string {
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
