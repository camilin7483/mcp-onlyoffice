package planner

import (
	"context"
	"fmt"
	"strings"
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
	obj := strings.ToLower(objective)

	if cached := p.cache.Lookup(obj); cached != nil {
		return cached, nil
	}

	intent := AnalyzeIntent(objective)
	if intent.Primary == IntentUnknown {
		return nil, fmt.Errorf("unable to determine intent for: %s", objective)
	}

	steps := p.buildChain(intent, objective, params)
	if len(steps) == 0 {
		return nil, fmt.Errorf("unable to build capability chain for: %s", objective)
	}

	plan := &Plan{
		Objective: objective,
		Intent:    intent,
		Steps:     steps,
		CreatedAt: time.Now(),
	}

	p.cache.Store(obj, plan)
	return plan, nil
}

func (p *Planner) buildChain(intent Intent, objective string, params map[string]any) []PlanStep {
	p.mu.RLock()
	defer p.mu.RUnlock()

	caps := SelectCapabilities(intent, p.capabilities)
	if len(caps) == 0 {
		return nil
	}

	if isCompositeIntent(intent.Primary) {
		return p.expandComposite(nil, intent, params)
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

	return steps
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

func (p *Planner) expandComposite(steps []PlanStep, intent Intent, params map[string]any) []PlanStep {
	switch intent.Primary {
	case IntentGenerateReport:
		return p.expandReport(steps, params)
	case IntentCreateDashboard:
		return p.expandDashboard(steps, params)
	default:
		return steps
	}
}

func (p *Planner) expandReport(base []PlanStep, params map[string]any) []PlanStep {
	return []PlanStep{
		{
			StepID:     "create_data",
			Capability: "excel.create_workbook",
			Params:     copyParams(params),
		},
		{
			StepID:     "build_charts",
			Capability: "charts.bar_chart",
			Params:     copyParams(params),
			DependsOn:  []string{"create_data"},
		},
		{
			StepID:     "write_report",
			Capability: "documents.create_report",
			Params:     copyParams(params),
			DependsOn:  []string{"build_charts"},
		},
	}
}

func (p *Planner) expandDashboard(base []PlanStep, params map[string]any) []PlanStep {
	if params["prefer_single_call"] != nil {
		return []PlanStep{
			{
				StepID:     "generate_dashboard",
				Capability: "excel.kpi_sheet",
				Params:     copyParams(params),
			},
		}
	}

	return []PlanStep{
		{
			StepID:     "build_kpis",
			Capability: "analytics.statistics",
			Params:     copyParams(params),
		},
		{
			StepID:     "create_dashboard",
			Capability: "excel.kpi_sheet",
			Params:     copyParams(params),
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
