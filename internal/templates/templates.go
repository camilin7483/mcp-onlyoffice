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
			author := getStr(params, "author", "")
			summary := getStr(params, "summary", "")
			recommendations := getStr(params, "recommendations", "")

			sections := []agent.Section{
				{Type: agent.SectionText, Heading: "Executive Summary", Content: summary},
				{Type: agent.SectionText, Heading: "Analysis", Content: getStr(params, "analysis", "Analysis content pending.")},
			}

			if headers, rows, ok := extractTable(params, "data_table"); ok {
				sections = append(sections, agent.Section{
					Type: agent.SectionTable, Heading: "Data", Headers: headers, Rows: rows,
				})
			}

			sections = append(sections, agent.Section{Type: agent.SectionText, Heading: "Recommendations", Content: recommendations})

			if author != "" {
				sections = append(sections, agent.Section{
					Type: agent.SectionText, Heading: "Prepared by",
					Content: fmt.Sprintf("%s — %s", author, time.Now().Format("January 2, 2006")),
				})
			}

			return &agent.AgentTask{Task: "create_report", Title: title, Sections: sections}, nil
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
