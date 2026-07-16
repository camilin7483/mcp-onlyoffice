package planner

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Improvement struct {
	mu        sync.RWMutex
	memory    *OperationalMemory
	discovery *CapabilityIndex
	history   []ImprovementAction
}

type ImprovementAction struct {
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Result      string    `json:"result"`
}

type Suggestion struct {
	Category    string   `json:"category"`
	Severity    string   `json:"severity"`
	Description string   `json:"description"`
	Actions     []string `json:"actions"`
}

func NewImprovement(memory *OperationalMemory, discovery *CapabilityIndex) *Improvement {
	return &Improvement{
		memory:    memory,
		discovery: discovery,
	}
}

func (im *Improvement) Analyze() []Suggestion {
	im.mu.Lock()
	defer im.mu.Unlock()

	var suggestions []Suggestion

	suggestions = append(suggestions, im.analyzeFailures()...)
	suggestions = append(suggestions, im.analyzeCapabilityGaps()...)
	suggestions = append(suggestions, im.analyzeRedundancy()...)
	suggestions = append(suggestions, im.analyzeEfficiency()...)

	return suggestions
}

func (im *Improvement) analyzeFailures() []Suggestion {
	var suggestions []Suggestion

	caps := im.memory.RecentEntries(1000)
	failByCap := make(map[string]int)
	successByCap := make(map[string]int)

	for _, e := range caps {
		switch e.Status {
		case "error":
			failByCap[e.Capability]++
		case "success":
			successByCap[e.Capability]++
		}
	}

	for cap, fails := range failByCap {
		successes := successByCap[cap]
		total := fails + successes
		if total >= 3 {
			rate := float64(fails) / float64(total) * 100
			if rate > 40 {
				suggestions = append(suggestions, Suggestion{
					Category:    "reliability",
					Severity:    "high",
					Description: fmt.Sprintf("Capability %q has %.0f%% failure rate (%d/%d)", cap, rate, fails, total),
					Actions:     []string{"Check if inputs are valid", "Consider alternative capability", fmt.Sprintf("Review %s implementation", cap)},
				})
			}
		}
	}

	return suggestions
}

func (im *Improvement) analyzeCapabilityGaps() []Suggestion {
	var suggestions []Suggestion

	modules := im.discovery.Modules()
	tasks := im.memory.RecentTasks(100)

	usedCaps := make(map[string]bool)
	for _, t := range tasks {
		for _, c := range t.Capabilities {
			usedCaps[c] = true
		}
	}

	allCaps := im.discovery.FindByModule("")
	if len(usedCaps) > 0 && len(allCaps) > 0 {
		usageRate := float64(len(usedCaps)) / float64(len(allCaps)) * 100
		if usageRate < 30 {
			suggestions = append(suggestions, Suggestion{
				Category:    "coverage",
				Severity:    "medium",
				Description: fmt.Sprintf("Only %.0f%% of capabilities have been used (%d/%d)", usageRate, len(usedCaps), len(allCaps)),
				Actions:     []string{"Review unused modules: " + strings.Join(modules, ", "), "Update planner intent mapping for uncovered capabilities"},
			})
		}
	}

	return suggestions
}

func (im *Improvement) analyzeRedundancy() []Suggestion {
	var suggestions []Suggestion

	if im.discovery.Modules() == nil {
		return suggestions
	}

	return suggestions
}

func (im *Improvement) analyzeEfficiency() []Suggestion {
	var suggestions []Suggestion

	tasks := im.memory.RecentTasks(50)
	if len(tasks) < 3 {
		return suggestions
	}

	var totalDuration time.Duration
	totalCalls := 0
	for _, t := range tasks {
		totalDuration += t.Duration
		totalCalls += t.CallCount
	}

	avgCalls := float64(totalCalls) / float64(len(tasks))
	avgDuration := totalDuration / time.Duration(len(tasks))

	if avgCalls > 5 {
		suggestions = append(suggestions, Suggestion{
			Category:    "efficiency",
			Severity:    "medium",
			Description: fmt.Sprintf("Average %.1f calls per task — consider consolidating into fewer calls", avgCalls),
			Actions:     []string{"Use composite capabilities (excel.kpi_sheet vs multiple steps)", "Check if Python Engine has a combined capability for common chains"},
		})
	}

	if avgDuration > 10*time.Second {
		suggestions = append(suggestions, Suggestion{
			Category:    "performance",
			Severity:    "medium",
			Description: fmt.Sprintf("Average task duration is %v — above 10s threshold", avgDuration.Round(time.Millisecond)),
			Actions:     []string{"Check Python Engine for bottlenecks", "Consider parallel step execution"},
		})
	}

	return suggestions
}

func (im *Improvement) History() []ImprovementAction {
	im.mu.RLock()
	defer im.mu.RUnlock()
	result := make([]ImprovementAction, len(im.history))
	copy(result, im.history)
	return result
}

func (im *Improvement) Record(action ImprovementAction) {
	im.mu.Lock()
	defer im.mu.Unlock()
	im.history = append(im.history, action)
	sort.Slice(im.history, func(i, j int) bool {
		return im.history[i].Timestamp.After(im.history[j].Timestamp)
	})
	if len(im.history) > 50 {
		im.history = im.history[:50]
	}
}
