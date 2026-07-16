package planner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Capability string    `json:"capability"`
	Status     string    `json:"status"`
	Duration   string    `json:"duration"`
	Objective  string    `json:"objective,omitempty"`
	Error      string    `json:"error,omitempty"`
	Intent     string    `json:"intent,omitempty"`
	StepsCount int       `json:"steps_count,omitempty"`
}

type TaskRecord struct {
	Objective    string        `json:"objective"`
	Intent       string        `json:"intent"`
	Capabilities []string      `json:"capabilities"`
	Duration     time.Duration `json:"duration"`
	CallCount    int           `json:"call_count"`
	Error        string        `json:"error,omitempty"`
	Success      bool          `json:"success"`
	Timestamp    time.Time     `json:"timestamp"`
}

type OperationalMemory struct {
	mu         sync.RWMutex
	entries    []MemoryEntry
	tasks      []TaskRecord
	maxSize    int
	persistPath string
}

var defaultMemoryPath = filepath.Join(os.TempDir(), "opencode-mcp-memory.json")

func NewMemory(maxSize int) *OperationalMemory {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return NewMemoryWithPath(maxSize, defaultMemoryPath)
}

func NewMemoryWithPath(maxSize int, path string) *OperationalMemory {
	if maxSize <= 0 {
		maxSize = 1000
	}
	m := &OperationalMemory{
		entries:     make([]MemoryEntry, 0, maxSize),
		tasks:       make([]TaskRecord, 0, 100),
		maxSize:     maxSize,
		persistPath: path,
	}
	m.load()
	return m
}

func (m *OperationalMemory) Record(capability, status, objective, errStr string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry := MemoryEntry{
		Timestamp:  time.Now(),
		Capability: capability,
		Status:     status,
		Duration:   duration.Round(time.Millisecond).String(),
		Objective:  objective,
		Error:      errStr,
	}

	if len(m.entries) >= m.maxSize {
		m.entries = m.entries[1:]
	}
	m.entries = append(m.entries, entry)
	m.persist()
}

func (m *OperationalMemory) RecordSuccess(capability, objective string, duration time.Duration) {
	m.Record(capability, "success", objective, "", duration)
}

func (m *OperationalMemory) RecordError(capability, objective, errStr string, duration time.Duration) {
	m.Record(capability, "error", objective, errStr, duration)
}

func (m *OperationalMemory) RecordRetry(capability, objective string, duration time.Duration) {
	m.Record(capability, "retry", objective, "", duration)
}

func (m *OperationalMemory) RecordTask(objective, intentName string, caps []string, duration time.Duration, calls int, err string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := TaskRecord{
		Objective:    objective,
		Intent:       intentName,
		Capabilities: caps,
		Duration:     duration,
		CallCount:    calls,
		Error:        err,
		Success:      err == "",
		Timestamp:    time.Now(),
	}
	m.tasks = append(m.tasks, record)
	if len(m.tasks) > 100 {
		m.tasks = m.tasks[len(m.tasks)-100:]
	}
	m.persist()
}

func (m *OperationalMemory) RecentEntries(n int) []MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if n <= 0 || n > len(m.entries) {
		n = len(m.entries)
	}
	result := make([]MemoryEntry, n)
	copy(result, m.entries[len(m.entries)-n:])
	return result
}

func (m *OperationalMemory) FailureRate(capability string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var total, failed int
	for _, e := range m.entries {
		if e.Capability == capability {
			total++
			if e.Status == "error" {
				failed++
			}
		}
	}
	if total == 0 {
		return 0, fmt.Errorf("no data for capability: %s", capability)
	}
	return float64(failed) / float64(total), nil
}

func (m *OperationalMemory) KnownFailure(capability string) bool {
	rate, err := m.FailureRate(capability)
	if err != nil {
		return false
	}
	return rate > 0.5
}

func (m *OperationalMemory) Stats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.entries)
	successes := 0
	errors := 0
	retries := 0
	capCount := make(map[string]int)

	for _, e := range m.entries {
		capCount[e.Capability]++
		switch e.Status {
		case "success":
			successes++
		case "error":
			errors++
		case "retry":
			retries++
		}
	}

	taskTotal := len(m.tasks)
	taskSuccesses := 0
	taskErrors := 0
	var totalDuration time.Duration
	totalCalls := 0

	for _, t := range m.tasks {
		if t.Success {
			taskSuccesses++
		} else {
			taskErrors++
		}
		totalDuration += t.Duration
		totalCalls += t.CallCount
	}

	metrics := EfficiencyMetrics{
		TotalTasks:    taskTotal,
		SuccessTasks:  taskSuccesses,
		FailedTasks:   taskErrors,
		TotalCalls:    totalCalls,
		TotalDuration: totalDuration,
	}

	return map[string]any{
		"total_operations":    total,
		"successes":           successes,
		"errors":              errors,
		"retries":             retries,
		"unique_capabilities": len(capCount),
		"most_used":           mostUsed(capCount),
		"task_stats": map[string]any{
			"total":  taskTotal,
			"ok":     taskSuccesses,
			"failed": taskErrors,
			"success_rate": fmt.Sprintf("%.1f%%", safePercent(taskSuccesses, taskTotal)),
		},
		"self_evaluation": m.evaluate(),
		"efficiency":      metrics,
	}
}

type EfficiencyMetrics struct {
	TotalTasks    int           `json:"total_tasks"`
	SuccessTasks  int           `json:"success_tasks"`
	FailedTasks   int           `json:"failed_tasks"`
	TotalCalls    int           `json:"total_calls"`
	TotalDuration time.Duration `json:"total_duration"`
}

func (m *OperationalMemory) Efficiency() EfficiencyMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var tm EfficiencyMetrics
	for _, t := range m.tasks {
		tm.TotalTasks++
		if t.Success {
			tm.SuccessTasks++
		} else {
			tm.FailedTasks++
		}
		tm.TotalCalls += t.CallCount
		tm.TotalDuration += t.Duration
	}
	return tm
}

func (m *OperationalMemory) RecentTasks(n int) []TaskRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if n <= 0 || n > len(m.tasks) {
		n = len(m.tasks)
	}
	result := make([]TaskRecord, n)
	copy(result, m.tasks[len(m.tasks)-n:])
	return result
}

func (m *OperationalMemory) evaluate() map[string]any {
	total := len(m.tasks)
	if total == 0 {
		return map[string]any{
			"status":  "insufficient_data",
			"message": "No task records to evaluate",
		}
	}

	successes := 0
	var avgDuration time.Duration
	for _, t := range m.tasks {
		if t.Success {
			successes++
		}
		avgDuration += t.Duration
	}
	avgDuration /= time.Duration(total)

	sr := float64(successes) / float64(total) * 100

	return map[string]any{
		"status":                  "evaluated",
		"success_rate":            fmt.Sprintf("%.1f%%", sr),
		"avg_duration":            avgDuration.Round(time.Millisecond).String(),
		"total_tasks":             total,
		"improvement_suggested":   sr < 80,
	}
}

func (m *OperationalMemory) SuggestImprovements() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var suggestions []string
	failCount := make(map[string]int)
	for _, e := range m.entries {
		if e.Status == "error" {
			failCount[e.Capability]++
		}
	}

	for cap, count := range failCount {
		if count >= 3 {
			suggestions = append(suggestions, fmt.Sprintf("Capability %s has failed %d times — consider alternative approach", cap, count))
		}
	}

	if len(m.entries) > 0 {
		total := len(m.entries)
		errors := 0
		for _, e := range m.entries {
			if e.Status == "error" {
				errors++
			}
		}
		if float64(errors)/float64(total) > 0.3 {
			suggestions = append(suggestions, "Overall error rate exceeds 30% — review capability selection logic")
		}
	}

	return suggestions
}

func mostUsed(capCount map[string]int) string {
	maxName := ""
	maxCount := 0
	for name, count := range capCount {
		if count > maxCount {
			maxCount = count
			maxName = name
		}
	}
	return maxName
}

func safePercent(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

func (m *OperationalMemory) GetPlansByIntent(intent string) []TaskRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []TaskRecord
	for _, t := range m.tasks {
		if t.Intent == intent {
			results = append(results, t)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})
	return results
}

func (m *OperationalMemory) persist() {
	data := struct {
		Entries []MemoryEntry `json:"entries"`
		Tasks   []TaskRecord  `json:"tasks"`
	}{
		Entries: m.entries,
		Tasks:   m.tasks,
	}
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	os.WriteFile(m.persistPath, b, 0644)
}

func (m *OperationalMemory) load() {
	data, err := os.ReadFile(m.persistPath)
	if err != nil {
		return
	}
	var loaded struct {
		Entries []MemoryEntry `json:"entries"`
		Tasks   []TaskRecord  `json:"tasks"`
	}
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}
	m.entries = loaded.Entries
	m.tasks = loaded.Tasks
}

type ErrorRecovery struct {
	memory *OperationalMemory
}

func NewErrorRecovery(memory *OperationalMemory) *ErrorRecovery {
	return &ErrorRecovery{memory: memory}
}

func (er *ErrorRecovery) SuggestAlternative(failedCapability string) []string {
	alt := map[string][]string{
		"excel.create_workbook":     {"excel.open_workbook", "excel.insert_data"},
		"charts.bar_chart":          {"charts.line_chart", "charts.pie_chart"},
		"charts.line_chart":         {"charts.bar_chart", "charts.financial_chart"},
		"documents.create_report":   {"documents.create_document", "documents.add_paragraph"},
		"presentations.create_presentation": {"presentations.add_slide"},
		"analytics.statistics":      {"analytics.aggregate", "analytics.frequency"},
	}

	if alts, ok := alt[filterCapability(failedCapability)]; ok {
		return alts
	}
	return nil
}

func filterCapability(cap string) string {
	parts := strings.SplitN(cap, ".", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return cap
}
