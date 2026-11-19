package templates

import (
	"strings"
	"testing"
)

func TestListContainsKnownTemplates(t *testing.T) {
	names := map[string]bool{}
	for _, tmpl := range List() {
		names[tmpl.Name] = true
	}
	for _, want := range []string{"business_report", "technical_doc", "meeting_minutes", "proposal"} {
		if !names[want] {
			t.Fatalf("template %q not in registry: %v", want, names)
		}
	}
}

func TestRenderBusinessReport(t *testing.T) {
	task, err := Render("business_report", map[string]any{
		"title":           "Q3 Review",
		"summary":         "Revenue grew.",
		"recommendations": "Keep going.",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if task.Task != "create_report" {
		t.Fatalf("task = %q, want create_report", task.Task)
	}
	if task.Title != "Q3 Review" {
		t.Fatalf("title = %q", task.Title)
	}
	if len(task.Sections) == 0 {
		t.Fatal("expected non-empty sections")
	}
}

func TestRenderUnknownTemplate(t *testing.T) {
	if _, err := Render("no_such_template", nil); err == nil {
		t.Fatal("expected error for unknown template")
	}
}

func TestRenderTextEscapingPassthrough(t *testing.T) {
	evil := `<script>alert("x") & 'quotes'`
	out, err := RenderText("proposal", map[string]any{
		"title":   evil,
		"summary": evil,
	})
	if err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	// Content is document body text, not HTML: it must survive verbatim.
	if !strings.Contains(out, evil) {
		t.Fatalf("expected verbatim params in output, got:\n%s", out)
	}
}
