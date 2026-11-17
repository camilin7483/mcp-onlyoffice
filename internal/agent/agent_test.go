package agent

import (
	"context"
	"testing"

	"github.com/cam/mcp-onlyoffice/internal/storage"
)

func newTestAgent(t *testing.T) *DocumentAgent {
	t.Helper()
	store, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return New(store)
}

func TestExecuteCreateReport(t *testing.T) {
	a := newTestAgent(t)
	res, err := a.Execute(context.Background(), &AgentTask{
		Task:  "create_report",
		Title: "Quarterly Report",
		Sections: []Section{
			{Type: SectionText, Heading: "Intro", Content: "hello world\nsecond line"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Documents) == 0 {
		t.Fatal("expected at least one document")
	}
	d := res.Documents[0]
	if d.ID == "" || d.Filename == "" {
		t.Fatalf("document missing id/filename: %+v", d)
	}
	if d.Size <= 0 {
		t.Fatalf("expected positive size, got %d", d.Size)
	}
	if res.Duration == "" {
		t.Fatal("expected non-empty duration")
	}
}

func TestExecuteEmptyTaskErrors(t *testing.T) {
	a := newTestAgent(t)
	if _, err := a.Execute(context.Background(), &AgentTask{}); err == nil {
		t.Fatal("expected error for empty task")
	}
}

func TestExecuteMailMergeRequiresRecords(t *testing.T) {
	a := newTestAgent(t)
	if _, err := a.Execute(context.Background(), &AgentTask{
		Task: "mail_merge", Title: "Letters", Template: "Hi {{name}}",
	}); err == nil {
		t.Fatal("expected error when records are missing")
	}
}
