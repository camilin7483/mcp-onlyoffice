package templates

import (
	"fmt"
	"strings"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/agent"
)

type Template struct {
	Name        string
	Description string
	Render      func(params map[string]any) (*agent.AgentTask, error)
}

// Corporate business pack: palette and style names shared with the native
// DOCX builder (internal/ooxml) and the Python engine. The Go agent renders
// sections as body content; real header/footer parts and the TOC field are
// emitted by ooxml.CreateCorporateReport and python documents.create_report
// (see CorporateMeta), which consume the same params.
// ponytail: header/footer/page-size travel as template params + meta, not as
// body sections, so they never pollute the document body.
const (
	CorporateBlueDark  = "1F4E79"
	CorporateBlue      = "2E75B6"
	CorporateBlueLight = "D9E2F3"
)

const (
	CorporateStyleNormal   = "Normal"
	CorporateStyleHeading1 = "Heading1"
	CorporateStyleHeading2 = "Heading2"
	CorporateStyleHeading3 = "Heading3"
	CorporateStyleQuote    = "Quote"
)

// CorporatePageSize validates the page_size param ("letter" default, "A4").
func CorporatePageSize(params map[string]any) string {
	if s, ok := params["page_size"].(string); ok && (s == "A4" || s == "a4") {
		return "A4"
	}
	return "letter"
}

// CorporateMeta extracts the corporate pack hints (header/footer/page size,
// palette) for presenters that emit real DOCX parts.
func CorporateMeta(title string, params map[string]any) map[string]string {
	header := getStr(params, "header", "")
	if header == "" {
		header = title
	}
	return map[string]string{
		"header":     header,
		"footer":     getStr(params, "footer", ""),
		"page_size":  CorporatePageSize(params),
		"accent":     CorporateBlue,
		"accentDark": CorporateBlueDark,
	}
}

// CorporateCoverSection builds the cover block: title, subtitle, date.
func CorporateCoverSection(title, subtitle, date, author string) agent.Section {
	lines := []string{title}
	if subtitle != "" {
		lines = append(lines, subtitle)
	}
	if date != "" {
		lines = append(lines, date)
	}
	if author != "" {
		lines = append(lines, "Prepared by: "+author)
	}
	return agent.Section{Type: agent.SectionText, Content: strings.Join(lines, "\n")}
}

// CorporateTOCSection is the TOC placeholder; Word fills the field on update.
func CorporateTOCSection() agent.Section {
	return agent.Section{
		Type:    agent.SectionText,
		Heading: "Table of Contents",
		Content: "TOC field placeholder — update the table of contents field in Word to generate entries from Heading1-3.",
	}
}

// CorporateHeaderText resolves the running header (defaults to the title).
func CorporateHeaderText(title string, params map[string]any) string {
	if h := getStr(params, "header", ""); h != "" {
		return h
	}
	return title
}

// CorporateFooterText resolves the footer; page numbering is emitted as a
// PAGE field by the DOCX presenters, so the text stays numbering-free.
func CorporateFooterText(params map[string]any) string {
	return getStr(params, "footer", "")
}

// CorporateStyleForLevel maps a 1-based depth to Heading1-3 (else Normal).
func CorporateStyleForLevel(level int) string {
	switch level {
	case 1:
		return CorporateStyleHeading1
	case 2:
		return CorporateStyleHeading2
	case 3:
		return CorporateStyleHeading3
	default:
		return CorporateStyleNormal
	}
}

var registry []Template

func Register(t Template) {
	registry = append(registry, t)
}

func List() []Template {
	return registry
}

func Find(name string) *Template {
	for _, t := range registry {
		if t.Name == name {
			return &t
		}
	}
	return nil
}

func Render(name string, params map[string]any) (*agent.AgentTask, error) {
	t := Find(name)
	if t == nil {
		return nil, fmt.Errorf("template not found: %s", name)
	}
	return t.Render(params)
}

func init() {
	Register(Template{
		Name:        "business_report",
		Description: "Business report with executive summary, analysis, and recommendations",
		Render: func(params map[string]any) (*agent.AgentTask, error) {
			title := getStr(params, "title", "Business Report")
			subtitle := getStr(params, "subtitle", "")
			date := getStr(params, "date", time.Now().Format("January 2, 2006"))
			author := getStr(params, "author", "")
			summary := getStr(params, "summary", "")
			recommendations := getStr(params, "recommendations", "")
			includeTOC := getBool(params, "include_toc", true)

			sections := []agent.Section{
				CorporateCoverSection(title, subtitle, date, author),
			}
			if includeTOC {
				sections = append(sections, CorporateTOCSection())
			}
			sections = append(sections,
				agent.Section{Type: agent.SectionText, Heading: "Executive Summary", Content: summary},
				agent.Section{Type: agent.SectionText, Heading: "Analysis", Content: getStr(params, "analysis", "Analysis content pending.")},
			)

			if headers, rows, ok := extractTable(params, "data_table"); ok {
				sections = append(sections, agent.Section{
					Type: agent.SectionTable, Heading: "Data", Headers: headers, Rows: rows,
				})
			}

			sections = append(sections, agent.Section{Type: agent.SectionText, Heading: "Recommendations", Content: recommendations})

			return &agent.AgentTask{Task: "create_report", Template: "business_report", Title: title, Sections: sections}, nil
		},
	})

	Register(Template{
		Name:        "technical_doc",
		Description: "Technical documentation with overview, architecture, and implementation details",
		Render: func(params map[string]any) (*agent.AgentTask, error) {
			title := getStr(params, "title", "Technical Documentation")

			sections := []agent.Section{
				{Type: agent.SectionText, Heading: "Overview", Content: getStr(params, "overview", "")},
				{Type: agent.SectionText, Heading: "Architecture", Content: getStr(params, "architecture", "")},
			}

			if headers, rows, ok := extractTable(params, "components"); ok {
				sections = append(sections, agent.Section{
					Type: agent.SectionTable, Heading: "Components", Headers: headers, Rows: rows,
				})
			}

			sections = append(sections,
				agent.Section{Type: agent.SectionText, Heading: "Implementation", Content: getStr(params, "implementation", "")},
				agent.Section{Type: agent.SectionText, Heading: "Deployment", Content: getStr(params, "deployment", "")},
			)

			return &agent.AgentTask{Task: "create_report", Title: title, Sections: sections}, nil
		},
	})

	Register(Template{
		Name:        "meeting_minutes",
		Description: "Meeting minutes with attendees, agenda items, decisions, and action items",
		Render: func(params map[string]any) (*agent.AgentTask, error) {
			title := getStr(params, "title", "Meeting Minutes")
			date := getStr(params, "date", time.Now().Format("January 2, 2006"))

			sections := []agent.Section{
				{Type: agent.SectionText, Heading: "Meeting", Content: fmt.Sprintf("%s — %s", title, date)},
				{Type: agent.SectionText, Heading: "Attendees", Content: getStr(params, "attendees", "N/A")},
			}

			if headers, rows, ok := extractTable(params, "agenda"); ok {
				sections = append(sections, agent.Section{
					Type: agent.SectionTable, Heading: "Agenda", Headers: headers, Rows: rows,
				})
			}

			sections = append(sections,
				agent.Section{Type: agent.SectionText, Heading: "Discussion", Content: getStr(params, "discussion", "")},
				agent.Section{Type: agent.SectionText, Heading: "Decisions", Content: getStr(params, "decisions", "")},
			)

			if headers, rows, ok := extractTable(params, "action_items"); ok {
				sections = append(sections, agent.Section{
					Type: agent.SectionTable, Heading: "Action Items", Headers: headers, Rows: rows,
				})
			}

			return &agent.AgentTask{Task: "create_report", Title: title, Sections: sections}, nil
		},
	})

	Register(Template{
		Name:        "proposal",
		Description: "Business proposal with executive summary, scope, timeline, and budget",
		Render: func(params map[string]any) (*agent.AgentTask, error) {
			title := getStr(params, "title", "Proposal")

			sections := []agent.Section{
				{Type: agent.SectionText, Heading: "Executive Summary", Content: getStr(params, "summary", "")},
				{Type: agent.SectionText, Heading: "Scope of Work", Content: getStr(params, "scope", "")},
				{Type: agent.SectionText, Heading: "Timeline", Content: getStr(params, "timeline", "")},
			}

			if headers, rows, ok := extractTable(params, "budget"); ok {
				sections = append(sections, agent.Section{
					Type: agent.SectionTable, Heading: "Budget", Headers: headers, Rows: rows,
				})
			}

			sections = append(sections,
				agent.Section{Type: agent.SectionText, Heading: "Terms", Content: getStr(params, "terms", "")},
			)

			return &agent.AgentTask{Task: "create_report", Title: title, Sections: sections}, nil
		},
	})
}

func getStr(params map[string]any, key, def string) string {
	if v, ok := params[key].(string); ok {
		return v
	}
	return def
}

func getBool(params map[string]any, key string, def bool) bool {
	if v, ok := params[key].(bool); ok {
		return v
	}
	return def
}

func extractTable(params map[string]any, key string) ([]string, [][]string, bool) {
	raw, ok := params[key].(map[string]any)
	if !ok {
		return nil, nil, false
	}

	var headers []string
	if h, ok := raw["headers"].([]any); ok {
		for _, v := range h {
			headers = append(headers, fmt.Sprint(v))
		}
	}

	var rows [][]string
	if r, ok := raw["rows"].([]any); ok {
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

	if len(headers) == 0 && len(rows) == 0 {
		return nil, nil, false
	}
	return headers, rows, true
}

func RenderText(templateName string, params map[string]any) (string, error) {
	task, err := Render(templateName, params)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", task.Title))
	for _, s := range task.Sections {
		if s.Heading != "" {
			b.WriteString(fmt.Sprintf("## %s\n\n", s.Heading))
		}
		if s.Content != "" {
			b.WriteString(s.Content + "\n\n")
		}
		if len(s.Headers) > 0 {
			b.WriteString("| " + strings.Join(s.Headers, " | ") + " |\n")
			b.WriteString("|" + strings.Repeat("---|", len(s.Headers)) + "\n")
			for _, row := range s.Rows {
				b.WriteString("| " + strings.Join(row, " | ") + " |\n")
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String()), nil
}
