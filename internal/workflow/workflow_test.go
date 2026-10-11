package workflow

import (
	"context"
	"testing"

	"github.com/cam/mcp-onlyoffice/internal/agent"
	"github.com/cam/mcp-onlyoffice/internal/storage"
)

func TestBatchCreate(t *testing.T) {
	store, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	e := NewEngine(store, agent.New(store))

	res, err := e.Execute(context.Background(), "batch_create", map[string]any{
		"items": []any{
			map[string]any{"title": "Doc One", "content": "hello"},
			map[string]any{"title": "Doc Two", "content": "world"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.FailedAt != "" {
		t.Fatalf("workflow failed at %s: %+v", res.FailedAt, res.Steps)
	}
	out, _ := res.Steps[0].Output.(map[string]any)
	if out["count"] != 2 {
		t.Errorf("count = %v, want 2", out["count"])
	}
}

func TestBatchCreateEmpty(t *testing.T) {
	store, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	e := NewEngine(store, agent.New(store))

	res, err := e.Execute(context.Background(), "batch_create", map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.FailedAt == "" {
		t.Error("expected failure on empty items")
	}
}

func TestCreateReportTableSectionsKeepAllIDs(t *testing.T) {
	store, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	a := agent.New(store)

	res, err := a.Execute(context.Background(), &agent.AgentTask{
		Task:  "create_report",
		Title: "Sales Report",
		Sections: []agent.Section{
			{Type: agent.SectionText, Heading: "Intro", Content: "hello"},
			{Type: agent.SectionTable, Heading: "Q1", Headers: []string{"Month", "Revenue"}, Rows: [][]string{{"Jan", "10"}, {"Feb", "20"}}},
			{Type: agent.SectionTable, Heading: "Q2", Headers: []string{"Month", "Revenue"}, Rows: [][]string{{"Apr", "30"}}},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Documents) != 3 {
		t.Fatalf("expected 3 documents (2 tables + main), got %d", len(res.Documents))
	}
	seen := map[string]bool{}
	for _, d := range res.Documents {
		if d.ID == "" {
			t.Error("document with empty ID")
		}
		if seen[d.ID] {
			t.Errorf("duplicate document ID %s", d.ID)
		}
		seen[d.ID] = true
	}
}
