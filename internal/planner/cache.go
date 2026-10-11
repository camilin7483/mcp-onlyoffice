package planner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type CachedPlan struct {
	Plan      *Plan     `json:"plan"`
	CreatedAt time.Time `json:"created_at"`
	Hits      int       `json:"hits"`
}

type PlanCache struct {
	mu     sync.RWMutex
	plans  map[string]*CachedPlan
	path   string
	maxAge time.Duration
}

func NewPlanCache() *PlanCache {
	c := &PlanCache{
		plans:  make(map[string]*CachedPlan),
		path:   filepath.Join(os.TempDir(), "opencode-mcp-plan-cache.json"),
		maxAge: 24 * time.Hour,
	}
	c.load()
	return c
}

func (c *PlanCache) Lookup(objective string) *Plan {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := normalize(objective)
	cached, ok := c.plans[key]
	if !ok {
		return nil
	}
	if time.Since(cached.CreatedAt) > c.maxAge {
		delete(c.plans, key)
		return nil
	}
	cached.Hits++
	return clonePlan(cached.Plan)
}

func (c *PlanCache) Store(objective string, plan *Plan) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := normalize(objective)
	c.plans[key] = &CachedPlan{
		Plan:      clonePlan(plan),
		CreatedAt: time.Now(),
	}
	c.save()
}

func (c *PlanCache) save() {
	b, err := json.Marshal(c.plans)
	if err != nil {
		return
	}
	os.WriteFile(c.path, b, 0644)
}

func (c *PlanCache) Stats() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	totalHits := 0
	for _, cp := range c.plans {
		totalHits += cp.Hits
	}
	return map[string]any{
		"cached_plans": len(c.plans),
		"total_hits":   totalHits,
	}
}

func (c *PlanCache) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var plans map[string]*CachedPlan
	if err := json.Unmarshal(data, &plans); err != nil {
		return
	}
	c.plans = plans
}

// normalize is the single place for key normalization: lower + whitespace fold.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

func clonePlan(p *Plan) *Plan {
	if p == nil {
		return nil
	}
	out := &Plan{
		Objective: p.Objective,
		Intent:    cloneIntent(p.Intent),
		CreatedAt: p.CreatedAt,
	}
	out.Steps = make([]PlanStep, len(p.Steps))
	for i, s := range p.Steps {
		out.Steps[i] = PlanStep{
			Capability: s.Capability,
			StepID:     s.StepID,
			Params:     cloneParams(s.Params),
		}
		if s.DependsOn != nil {
			out.Steps[i].DependsOn = append([]string(nil), s.DependsOn...)
		}
	}
	return out
}

func cloneIntent(in Intent) Intent {
	out := Intent{
		Primary:    in.Primary,
		Confidence: in.Confidence,
	}
	out.Secondary = append([]IntentType(nil), in.Secondary...)
	out.Outputs = append([]string(nil), in.Outputs...)
	if in.Entities != nil {
		out.Entities = make(map[string]string, len(in.Entities))
		for k, v := range in.Entities {
			out.Entities[k] = v
		}
	}
	return out
}

func cloneParams(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
