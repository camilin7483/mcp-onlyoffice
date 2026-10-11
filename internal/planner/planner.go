package planner

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Capability struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type PlanStep struct {
	Capability string         `json:"capability"`
	Params     map[string]any `json:"params"`
	DependsOn  []string       `json:"depends_on,omitempty"`
	StepID     string         `json:"step_id"`
}

type Plan struct {
	Objective string     `json:"objective"`
	Intent    Intent     `json:"intent"`
	Steps     []PlanStep `json:"steps"`
	CreatedAt time.Time  `json:"created_at"`
}

type Planner struct {
	mu           sync.RWMutex
	capabilities map[string]string
	cache        *PlanCache
	memory       *OperationalMemory
	discovery    *CapabilityIndex
}

func New() *Planner {
	return &Planner{
		capabilities: make(map[string]string),
		cache:        NewPlanCache(),
		memory:       NewMemory(1000),
		discovery:    NewCapabilityIndex(),
	}
}

func (p *Planner) RegisterCapabilities(caps []Capability) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range caps {
		p.capabilities[c.Name] = c.Description
		p.discovery.Register(c.Name, c.Description)
	}
}

func (p *Planner) Memory() *OperationalMemory {
	return p.memory
}

func (p *Planner) Discovery() *CapabilityIndex {
	return p.discovery
}

func (p *Planner) CreatePlan(ctx context.Context, objective string, params map[string]any) (*Plan, error) {
	if cached := p.cache.Lookup(objective); cached != nil {
		return cached, nil
	}

	intent := AnalyzeIntent(objective)
	if intent.Primary == IntentUnknown {
		return nil, fmt.Errorf("unable to determine intent for: %s", objective)
	}

	steps, err := p.buildChain(intent, objective, params)
	if err != nil {
		return nil, err
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("unable to build capability chain for: %s", objective)
	}

	plan := &Plan{
		Objective: objective,
		Intent:    intent,
		Steps:     steps,
		CreatedAt: time.Now(),
	}

	p.cache.Store(objective, plan)
	return plan, nil
}

func (p *Planner) buildChain(intent Intent, objective string, params map[string]any) ([]PlanStep, error) {
	p.mu.RLock()
	available := make(map[string]string, len(p.capabilities))
	for k, v := range p.capabilities {
		available[k] = v
	}
	p.mu.RUnlock()

	caps := SelectCapabilities(intent, available)
	if len(caps) == 0 && !isCompositeIntent(intent.Primary) {
		return nil, nil
	}

	if isCompositeIntent(intent.Primary) {
		return p.expandComposite(nil, intent, params, available)
	}

	var steps []PlanStep
	stepID := 0

	for _, cap := range caps {
		stepID++
		step := PlanStep{
			StepID:     fmt.Sprintf("step_%02d", stepID),
			Capability: cap,
			Params:     copyParams(params),
		}
		if len(steps) > 0 {
			step.DependsOn = []string{steps[len(steps)-1].StepID}
		}
		steps = append(steps, step)
	}

	return steps, nil
}

func copyParams(params map[string]any) map[string]any {
	result := make(map[string]any, len(params))
	for k, v := range params {
		result[k] = v
	}
	return result
}

func isCompositeIntent(it IntentType) bool {
	return it == IntentGenerateReport || it == IntentBatchProcess || it == IntentCreateDashboard
}

func (p *Planner) expandComposite(steps []PlanStep, intent Intent, params map[string]any, available map[string]string) ([]PlanStep, error) {
	switch intent.Primary {
	case IntentGenerateReport:
		return p.expandReport(steps, params, available)
	case IntentCreateDashboard:
		return p.expandDashboard(steps, params), nil
	case IntentBatchProcess:
		return p.expandBatch(steps, intent, params, available), nil
	default:
		return steps, nil
	}
}

// expandReport builds a per-step param map instead of copying the same blob
// to every step. Each required capability is checked against available.
func (p *Planner) expandReport(base []PlanStep, params map[string]any, available map[string]string) ([]PlanStep, error) {
	for _, cap := range []string{"excel.create_workbook", "charts.bar_chart", "documents.create_report"} {
		if _, ok := available[cap]; !ok {
			return nil, fmt.Errorf("expandReport: capability %q not available (register it to generate reports)", cap)
		}
	}
	if params == nil {
		params = map[string]any{}
	}

	filename, _ := firstString(params, "filename", "path", "workbook")
	if filename == "" {
		return nil, fmt.Errorf("expandReport: missing required param 'filename' for excel.create_workbook (provide \"filename\")")
	}
	sheet, _ := firstString(params, "sheet", "worksheet")
	if sheet == "" {
		sheet = "Sheet1"
	}
	categories, ok := params["categories"]
	if !ok || categories == nil {
		return nil, fmt.Errorf("expandReport: missing required param 'categories' for charts.bar_chart (provide \"categories\" list)")
	}
	values, ok := params["values"]
	if !ok || values == nil {
		return nil, fmt.Errorf("expandReport: missing required param 'values' for charts.bar_chart (provide \"values\" list)")
	}
	title, _ := firstString(params, "title", "report_title")
	if title == "" {
		return nil, fmt.Errorf("expandReport: missing required param 'title' for documents.create_report (provide \"title\")")
	}
	reportPath, _ := firstString(params, "path", "output", "filename")
	if reportPath == "" {
		return nil, fmt.Errorf("expandReport: missing required param 'path' for documents.create_report (provide \"path\")")
	}

	return []PlanStep{
		{
			StepID:     "create_data",
			Capability: "excel.create_workbook",
			Params:     map[string]any{"filename": filename, "sheet": sheet},
		},
		{
			StepID:     "build_charts",
			Capability: "charts.bar_chart",
			Params:     map[string]any{"categories": categories, "values": values},
			DependsOn:  []string{"create_data"},
		},
		{
			StepID:     "write_report",
			Capability: "documents.create_report",
			Params:     map[string]any{"path": reportPath, "title": title},
			DependsOn:  []string{"build_charts"},
		},
	}, nil
}

func firstString(params map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := params[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s, true
			}
		}
	}
	return "", false
}

func (p *Planner) expandBatch(base []PlanStep, intent Intent, params map[string]any, available map[string]string) []PlanStep {
	if len(base) > 0 {
		return base
	}
	// Fallback chain when no capability matched the batch intent directly.
	fallback := []string{"excel.create_workbook", "documents.create_report", "presentations.create_presentation"}
	var steps []PlanStep
	for i, cap := range fallback {
		if _, ok := available[cap]; !ok {
			continue
		}
		step := PlanStep{
			StepID:     fmt.Sprintf("batch_%02d", i+1),
			Capability: cap,
			Params:     copyParams(params),
		}
		if len(steps) > 0 {
			step.DependsOn = []string{steps[len(steps)-1].StepID}
		}
		steps = append(steps, step)
	}
	return steps
}

func (p *Planner) expandDashboard(base []PlanStep, params map[string]any) []PlanStep {
	// prefer_single_call is a planner routing hint, not an engine param.
	engine := copyParams(params)
	delete(engine, "prefer_single_call")

	if params["prefer_single_call"] != nil {
		return []PlanStep{
			{
				StepID:     "generate_dashboard",
				Capability: "excel.kpi_sheet",
				Params:     copyParams(engine),
			},
		}
	}

	return []PlanStep{
		{
			StepID:     "build_kpis",
			Capability: "analytics.statistics",
			Params:     copyParams(engine),
		},
		{
			StepID:     "create_dashboard",
			Capability: "excel.kpi_sheet",
			Params:     copyParams(engine),
			DependsOn:  []string{"build_kpis"},
		},
	}
}

type StepResult struct {
	StepID     string      `json:"step_id"`
	Capability string      `json:"capability"`
	Status     string      `json:"status"`
	Data       any         `json:"data,omitempty"`
	Error      string      `json:"error,omitempty"`
	Duration   string      `json:"duration"`
}

type ExecutionResult struct {
	Objective string       `json:"objective"`
	Intent    string       `json:"intent"`
	Status    string       `json:"status"`
	Steps     []StepResult `json:"steps"`
	Duration  string       `json:"duration"`
	FailedAt  string       `json:"failed_at,omitempty"`
}

// StepExecutor runs a single capability; wired by the caller (engine bridge, tests).
type StepExecutor func(ctx context.Context, capability string, params map[string]any) (any, error)

// ExecutePlan runs the plan chain with one recovery attempt per step:
// on failure it retries with the first alternative capability known to ErrorRecovery.
func (p *Planner) ExecutePlan(ctx context.Context, plan *Plan, exec StepExecutor) *ExecutionResult {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	recovery := NewErrorRecovery(p.memory)
	res := &ExecutionResult{Objective: plan.Objective, Intent: fmt.Sprintf("%d", plan.Intent.Primary)}

	for _, step := range plan.Steps {
		if err := ctx.Err(); err != nil {
			res.Status = "failed"
			res.FailedAt = step.StepID
			break
		}
		sr := p.runStep(ctx, exec, recovery, step)
		res.Steps = append(res.Steps, sr)
		if sr.Status != "ok" {
			res.Status = "failed"
			res.FailedAt = step.StepID
			break
		}
	}
	if res.Status == "" {
		res.Status = "ok"
	}
	res.Duration = time.Since(start).Round(time.Millisecond).String()
	return res
}

func (p *Planner) runStep(ctx context.Context, exec StepExecutor, recovery *ErrorRecovery, step PlanStep) StepResult {
	start := time.Now()
	data, err := exec(ctx, step.Capability, step.Params)
	if err == nil {
		p.memory.RecordSuccess(step.Capability, "", time.Since(start))
		return StepResult{StepID: step.StepID, Capability: step.Capability, Status: "ok", Data: data, Duration: time.Since(start).Round(time.Millisecond).String()}
	}

	p.memory.RecordError(step.Capability, "", err.Error(), time.Since(start))
	for _, alt := range recovery.SuggestAlternative(step.Capability) {
		p.memory.RecordRetry(alt, "", 0)
		retryStart := time.Now()
		data, retryErr := exec(ctx, alt, step.Params)
		if retryErr == nil {
			p.memory.RecordSuccess(alt, "", time.Since(retryStart))
			return StepResult{StepID: step.StepID, Capability: alt, Status: "ok", Data: data, Duration: time.Since(retryStart).Round(time.Millisecond).String()}
		}
		p.memory.RecordError(alt, "", retryErr.Error(), time.Since(retryStart))
	}
	return StepResult{StepID: step.StepID, Capability: step.Capability, Status: "error", Error: err.Error(), Duration: time.Since(start).Round(time.Millisecond).String()}
}
