package server

import (
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolRegistry manages progressive tool discovery.
// Instead of registering ALL tools at startup, it groups them into tiers:
//   Tier 1: Essential (always loaded — file operations, basic CRUD)
//   Tier 2: Common (loaded on demand — most used tools)
//   Tier 3: Advanced (loaded only when explicitly needed)
//
// The model receives only Tier 1 descriptions by default.
// Tier 2/3 tools are registered via discovery requests.
type ToolRegistry struct {
	mu    sync.RWMutex
	all   map[string]toolDef
	tiers map[int][]string // tier -> tool names
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		all:   make(map[string]toolDef),
		tiers: map[int][]string{1: {}, 2: {}, 3: {}},
	}
}

func (r *ToolRegistry) Register(t toolDef, tier int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.all[t.Name] = t
	r.tiers[tier] = append(r.tiers[tier], t.Name)
}

// Tier1 returns only essential tools (always loaded).
func (r *ToolRegistry) Tier1() []toolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.getTier(1)
}

// Tier returns all tools up to and including the given tier.
func (r *ToolRegistry) Tier(n int) []toolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for tier := 1; tier <= n; tier++ {
		names = append(names, r.tiers[tier]...)
	}
	return r.byNames(names)
}

// Discover returns tools matching a capability keyword.
func (r *ToolRegistry) Discover(query string) []toolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	q := strings.ToLower(query)
	var found []toolDef
	for _, t := range r.all {
		if strings.Contains(strings.ToLower(t.Name), q) ||
			strings.Contains(strings.ToLower(t.Description), q) {
			found = append(found, t)
		}
	}
	if len(found) > 5 {
		found = found[:5] // max 5 results to save tokens
	}
	return found
}

func (r *ToolRegistry) byNames(names []string) []toolDef {
	var out []toolDef
	for _, n := range names {
		if t, ok := r.all[n]; ok {
			out = append(out, t)
		}
	}
	return out
}

func (r *ToolRegistry) getTier(n int) []toolDef {
	return r.byNames(r.tiers[n])
}

// ToMCPTools converts toolDefs to MCP Tool pointers.
func toMCPTools(defs []toolDef) []*mcp.Tool {
	out := make([]*mcp.Tool, len(defs))
	for i, t := range defs {
		out[i] = &mcp.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return out
}
