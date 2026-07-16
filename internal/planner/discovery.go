package planner

import (
	"sort"
	"strings"
	"sync"
)

type CapabilityIndex struct {
	mu         sync.RWMutex
	capabilities map[string]*CapabilityEntry
}

type CapabilityEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Module      string   `json:"module"`
	Tags        []string `json:"tags"`
}

func NewCapabilityIndex() *CapabilityIndex {
	return &CapabilityIndex{
		capabilities: make(map[string]*CapabilityEntry),
	}
}

func (ci *CapabilityIndex) Register(name, description string) {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	parts := strings.SplitN(name, ".", 2)
	module := ""
	if len(parts) == 2 {
		module = parts[0]
	}
	ci.capabilities[name] = &CapabilityEntry{
		Name:        name,
		Description: description,
		Module:      module,
		Tags:        extractTags(name, description),
	}
}

func (ci *CapabilityIndex) FindByModule(module string) []CapabilityEntry {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	var result []CapabilityEntry
	for _, entry := range ci.capabilities {
		if entry.Module == module {
			result = append(result, *entry)
		}
	}
	return result
}

func (ci *CapabilityIndex) FindByTag(tag string) []CapabilityEntry {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	tag = strings.ToLower(tag)
	var result []CapabilityEntry
	for _, entry := range ci.capabilities {
		for _, t := range entry.Tags {
			if t == tag {
				result = append(result, *entry)
				break
			}
		}
	}
	return result
}

func (ci *CapabilityIndex) Search(query string) []CapabilityEntry {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	q := strings.ToLower(query)
	var result []CapabilityEntry
	for _, entry := range ci.capabilities {
		if strings.Contains(entry.Name, q) || strings.Contains(strings.ToLower(entry.Description), q) {
			result = append(result, *entry)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func (ci *CapabilityIndex) Modules() []string {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	seen := make(map[string]bool)
	var modules []string
	for _, entry := range ci.capabilities {
		if entry.Module != "" && !seen[entry.Module] {
			seen[entry.Module] = true
			modules = append(modules, entry.Module)
		}
	}
	sort.Strings(modules)
	return modules
}

func (ci *CapabilityIndex) Count() int {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	return len(ci.capabilities)
}

func extractTags(name, description string) []string {
	var tags []string
	parts := strings.Split(name, ".")
	for _, p := range parts {
		tags = append(tags, strings.ToLower(p))
	}
	keywords := []string{"create", "read", "update", "delete", "convert", "analyze", "generate", "export", "import", "format", "style"}
	for _, kw := range keywords {
		if strings.Contains(strings.ToLower(description), kw) {
			tags = append(tags, kw)
		}
	}
	return tags
}
