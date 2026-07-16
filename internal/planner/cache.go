package planner

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	c.mu.RLock()
	defer c.mu.RUnlock()

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
	return cached.Plan
}

func (c *PlanCache) Store(objective string, plan *Plan) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := normalize(objective)
	c.plans[key] = &CachedPlan{
		Plan:      plan,
		CreatedAt: time.Now(),
	}
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

func normalize(s string) string {
	b := []byte(s)
	space := false
	j := 0
	for i := 0; i < len(b); i++ {
		if b[i] == ' ' || b[i] == '\t' || b[i] == '\n' {
			if !space {
				b[j] = ' '
				j++
				space = true
			}
		} else {
			b[j] = b[i]
			j++
			space = false
		}
	}
	return string(b[:j])
}
