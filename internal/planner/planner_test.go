package planner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAnalyzeIntent(t *testing.T) {
	tests := []struct {
		name      string
		objective string
		want      IntentType
	}{
		{"excel workbook", "Create a spreadsheet with sales data", IntentCreateWorkbook},
		{"bar chart", "Create a bar chart of monthly revenue", IntentCreateChart},
		{"document", "Write a report about project status", IntentCreateDocument},
		{"presentation", "Generate a slide deck for the meeting", IntentCreatePresentation},
		{"analysis", "Analyze the sales data and find trends", IntentAnalyzeData},
		{"dashboard", "Build a KPI dashboard for executives", IntentCreateDashboard},
		{"pdf", "Export the report as PDF", IntentCreatePDF},
		{"image", "Convert the image to PNG format", IntentProcessImage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := AnalyzeIntent(tt.objective)
			if intent.Primary != tt.want {
				t.Errorf("AnalyzeIntent(%q) = %v, want %v", tt.objective, intent.Primary, tt.want)
			}
			if intent.Confidence <= 0 {
				t.Errorf("AnalyzeIntent(%q) confidence = 0, want > 0", tt.objective)
			}
		})
	}
}

func TestAnalyzeIntentUnknown(t *testing.T) {
	intent := AnalyzeIntent("hello world")
	if intent.Primary != IntentUnknown {
		t.Errorf("AnalyzeIntent('hello world') = %v, want IntentUnknown", intent.Primary)
	}
}

func TestSelectCapabilities(t *testing.T) {
	available := map[string]string{
		"excel.create_workbook": "Create workbook",
		"excel.insert_data":     "Insert data",
		"excel.kpi_sheet":       "KPI sheet",
		"charts.bar_chart":      "Bar chart",
		"analytics.statistics":  "Statistics",
	}

	intent := Intent{Primary: IntentCreateDashboard}
	caps := SelectCapabilities(intent, available)
	if len(caps) == 0 {
		t.Fatal("SelectCapabilities returned empty")
	}

	found := false
	for _, c := range caps {
		if c == "excel.kpi_sheet" {
			found = true
		}
	}
	if !found {
		t.Error("SelectCapabilities should include excel.kpi_sheet")
	}
}

func TestCapabilityIndex(t *testing.T) {
	idx := NewCapabilityIndex()
	idx.Register("excel.create_workbook", "Create a new Excel workbook")
	idx.Register("charts.bar_chart", "Generate a bar chart")

	modules := idx.Modules()
	if len(modules) == 0 {
		t.Fatal("expected at least 1 module")
	}

	found := false
	for _, m := range modules {
		if m == "excel" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'excel' module")
	}

	results := idx.Search("chart")
	if len(results) == 0 {
		t.Error("expected search results for 'chart'")
	}
}

func TestPlanCache(t *testing.T) {
	cache := NewPlanCache()
	plan := &Plan{
		Objective: "test plan",
		Intent:    Intent{Primary: IntentCreateDocument},
		Steps: []PlanStep{
			{StepID: "step_01", Capability: "documents.create_report"},
		},
	}

	cache.Store("create a document", plan)
	cached := cache.Lookup("create a document")
	if cached == nil {
		t.Fatal("expected cached plan")
	}
	if cached.Objective != "test plan" {
		t.Errorf("cached plan objective = %q, want %q", cached.Objective, "test plan")
	}
}

func TestCreatePlan(t *testing.T) {
	p := New()
	p.RegisterCapabilities([]Capability{
		{Name: "excel.create_workbook", Description: "Create workbook"},
		{Name: "excel.insert_data", Description: "Insert data"},
		{Name: "charts.bar_chart", Description: "Bar chart"},
	})

	plan, err := p.CreatePlan(nil, "Create a spreadsheet", map[string]any{
		"filename": "/tmp/test.xlsx",
	})
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}
	if len(plan.Steps) == 0 {
		t.Fatal("expected at least 1 step")
	}
	if plan.Intent.Primary != IntentCreateWorkbook {
		t.Errorf("intent = %v, want IntentCreateWorkbook", plan.Intent.Primary)
	}
}

func TestCreatePlanBatch(t *testing.T) {
	p := New()
	p.RegisterCapabilities([]Capability{
		{Name: "excel.create_workbook", Description: "Create workbook"},
		{Name: "documents.create_report", Description: "Create report"},
	})

	plan, err := p.CreatePlan(nil, "convert all files in batch", map[string]any{})
	if err != nil {
		t.Fatalf("CreatePlan batch failed: %v", err)
	}
	if len(plan.Steps) == 0 {
		t.Fatal("expected batch fallback steps, got none")
	}
}

func TestExecutePlanRecovery(t *testing.T) {
	p := New()
	p.RegisterCapabilities([]Capability{
		{Name: "charts.bar_chart", Description: "Bar chart"},
		{Name: "charts.line_chart", Description: "Line chart"},
	})

	plan := &Plan{
		Objective: "chart test",
		Intent:    Intent{Primary: IntentCreateChart},
		Steps:     []PlanStep{{StepID: "step_01", Capability: "charts.bar_chart", Params: map[string]any{}}},
	}

	calls := 0
	res := p.ExecutePlan(nil, plan, func(ctx context.Context, cap string, params map[string]any) (any, error) {
		calls++
		if cap == "charts.bar_chart" {
			return nil, errors.New("boom")
		}
		return map[string]any{"ok": true}, nil
	})
	if res.Status != "ok" {
		t.Fatalf("expected recovery to succeed, got %q (%+v)", res.Status, res.Steps)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (fail + alternative retry), got %d", calls)
	}
}

func BenchmarkAnalyzeIntent(b *testing.B) {
	objectives := []string{
		"Create a spreadsheet with sales data and a chart",
		"Generate a report with statistics and export as PDF",
		"Build a KPI dashboard with monthly metrics",
		"Create a presentation with charts for the quarterly meeting",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, obj := range objectives {
			AnalyzeIntent(obj)
		}
	}
}

func BenchmarkCreatePlan(b *testing.B) {
	p := New()
	p.RegisterCapabilities([]Capability{
		{Name: "excel.create_workbook", Description: "Create workbook"},
		{Name: "excel.insert_data", Description: "Insert data"},
		{Name: "excel.kpi_sheet", Description: "KPI sheet"},
		{Name: "charts.bar_chart", Description: "Bar chart"},
		{Name: "charts.line_chart", Description: "Line chart"},
		{Name: "analytics.statistics", Description: "Statistics"},
		{Name: "documents.create_report", Description: "Create report"},
		{Name: "presentations.create_presentation", Description: "Create presentation"},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.CreatePlan(nil, "Generate a dashboard with charts", map[string]any{
			"path": "/tmp/test.xlsx",
			"kpis": []string{"revenue", "cost", "profit"},
		})
	}
}

func tempMemory(t *testing.T) *OperationalMemory {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test-memory.json")
	return NewMemoryWithPath(100, path)
}

func TestMemoryPersistence(t *testing.T) {
	m := tempMemory(t)

	m.RecordSuccess("test.capability", "test objective", 100*time.Millisecond)
	m.RecordError("test.error", "test objective", "something failed", 50*time.Millisecond)

	stats := m.Stats()
	total := stats["total_operations"].(int)
	if total != 2 {
		t.Errorf("expected 2 entries, got %d", total)
	}

	successes := stats["successes"].(int)
	if successes != 1 {
		t.Errorf("expected 1 success, got %d", successes)
	}

	errors := stats["errors"].(int)
	if errors != 1 {
		t.Errorf("expected 1 error, got %d", errors)
	}
}

func TestEfficiencyMetrics(t *testing.T) {
	m := tempMemory(t)
	eff := m.Efficiency()
	if eff.TotalTasks != 0 {
		t.Errorf("expected 0 tasks, got %d", eff.TotalTasks)
	}

	m.RecordTask("task1", "intent1", []string{"cap1"}, time.Second, 2, "")
	m.RecordTask("task2", "intent2", []string{"cap2"}, 2*time.Second, 3, "error")

	eff = m.Efficiency()
	if eff.TotalTasks != 2 {
		t.Errorf("expected 2 tasks, got %d", eff.TotalTasks)
	}
	if eff.SuccessTasks != 1 {
		t.Errorf("expected 1 success, got %d", eff.SuccessTasks)
	}
	if eff.FailedTasks != 1 {
		t.Errorf("expected 1 failure, got %d", eff.FailedTasks)
	}
	if eff.TotalCalls != 5 {
		t.Errorf("expected 5 calls, got %d", eff.TotalCalls)
	}
}

func TestSuggestImprovements(t *testing.T) {
	m := tempMemory(t)
	suggestions := m.SuggestImprovements()
	if len(suggestions) != 0 {
		t.Errorf("expected 0 suggestions for empty memory, got %d", len(suggestions))
	}

	for i := 0; i < 5; i++ {
		m.RecordError("failing.cap", "test", "error", time.Second)
	}

	suggestions = m.SuggestImprovements()
	if len(suggestions) == 0 {
		t.Error("expected suggestions for repeated failures")
	}
}

func reportPlanner() *Planner {
	p := New()
	p.RegisterCapabilities([]Capability{
		{Name: "excel.create_workbook", Description: "Create workbook"},
		{Name: "charts.bar_chart", Description: "Bar chart"},
		{Name: "documents.create_report", Description: "Create report"},
	})
	return p
}

func reportParams() map[string]any {
	return map[string]any{
		"filename":   "/tmp/report-data.xlsx",
		"sheet":      "Data",
		"categories": []string{"Jan", "Feb"},
		"values":     []float64{10, 20},
		"path":       "/tmp/monthly-report.docx",
		"title":      "Monthly Report",
	}
}

func TestExpandReportMapsParamsPerStep(t *testing.T) {
	p := reportPlanner()
	plan, err := p.CreatePlan(context.Background(), "generate monthly report for sales Q1", reportParams())
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(plan.Steps))
	}
	wb, ch, rep := plan.Steps[0], plan.Steps[1], plan.Steps[2]
	if wb.Capability != "excel.create_workbook" || ch.Capability != "charts.bar_chart" || rep.Capability != "documents.create_report" {
		t.Fatalf("unexpected chain: %v %v %v", wb.Capability, ch.Capability, rep.Capability)
	}
	if wb.Params["filename"] != "/tmp/report-data.xlsx" || wb.Params["sheet"] != "Data" {
		t.Errorf("workbook params = %v, want filename+sheet only", wb.Params)
	}
	if _, ok := wb.Params["values"]; ok {
		t.Errorf("workbook params must not contain chart keys: %v", wb.Params)
	}
	if _, ok := ch.Params["categories"]; !ok {
		t.Errorf("chart params missing categories: %v", ch.Params)
	}
	if _, ok := ch.Params["values"]; !ok {
		t.Errorf("chart params missing values: %v", ch.Params)
	}
	if _, ok := ch.Params["filename"]; ok {
		t.Errorf("chart params must not contain filename: %v", ch.Params)
	}
	if rep.Params["path"] != "/tmp/monthly-report.docx" || rep.Params["title"] != "Monthly Report" {
		t.Errorf("report params = %v, want path+title", rep.Params)
	}
}

func TestExpandReportMissingParamActionable(t *testing.T) {
	p := reportPlanner()
	params := reportParams()
	delete(params, "values")
	_, err := p.CreatePlan(context.Background(), "generate monthly report missing values Q2", params)
	if err == nil {
		t.Fatal("expected actionable error for missing values")
	}
	if !contains(err.Error(), "values") || !contains(err.Error(), "charts.bar_chart") {
		t.Errorf("error should name param and capability, got: %v", err)
	}
}

func TestExpandReportMissingCapability(t *testing.T) {
	p := New()
	p.RegisterCapabilities([]Capability{
		{Name: "excel.create_workbook", Description: "Create workbook"},
		{Name: "documents.create_report", Description: "Create report"},
	})
	_, err := p.CreatePlan(context.Background(), "generate monthly report no charts Q3", reportParams())
	if err == nil {
		t.Fatal("expected error for unavailable charts.bar_chart")
	}
	if !contains(err.Error(), "charts.bar_chart") {
		t.Errorf("error should name missing capability, got: %v", err)
	}
}

func TestExpandDashboardStripsPreferSingleCall(t *testing.T) {
	p := New()
	p.RegisterCapabilities([]Capability{
		{Name: "excel.kpi_sheet", Description: "KPI sheet"},
		{Name: "analytics.statistics", Description: "Statistics"},
	})

	single, err := p.CreatePlan(context.Background(), "build kpi dashboard single execs", map[string]any{
		"prefer_single_call": true, "kpis": []string{"revenue"},
	})
	if err != nil {
		t.Fatalf("CreatePlan single: %v", err)
	}
	for _, s := range single.Steps {
		if _, ok := s.Params["prefer_single_call"]; ok {
			t.Errorf("step %s leaked prefer_single_call: %v", s.StepID, s.Params)
		}
	}

	multi, err := p.CreatePlan(context.Background(), "build kpi dashboard multi execs", map[string]any{
		"prefer_single_call": true, "kpis": []string{"revenue"},
	})
	_ = multi
	// Force multi-step path directly.
	steps := p.expandDashboard(nil, map[string]any{"kpis": []string{"revenue"}})
	for _, s := range steps {
		if _, ok := s.Params["prefer_single_call"]; ok {
			t.Errorf("multi step %s leaked prefer_single_call", s.StepID)
		}
	}
}

func TestPlanCacheDeepCopyAndNormalize(t *testing.T) {
	c := NewPlanCache()
	plan := &Plan{
		Objective: "Case Test",
		Intent:    Intent{Primary: IntentCreateDocument},
		Steps: []PlanStep{
			{StepID: "step_01", Capability: "documents.create_report", Params: map[string]any{"title": "A"}},
		},
	}
	c.Store("  Cache Normalize KEY", plan)
	a := c.Lookup("cache normalize key")
	b := c.Lookup("CACHE   NORMALIZE key")
	if a == nil || b == nil {
		t.Fatal("expected case/space-insensitive cache hit")
	}
	a.Steps[0].Capability = "MUTATED"
	a.Steps[0].Params["title"] = "MUTATED"
	c2 := c.Lookup("cache normalize key")
	if c2.Steps[0].Capability == "MUTATED" || c2.Steps[0].Params["title"] == "MUTATED" {
		t.Error("Lookup must return a deep copy, shared pointer detected")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
