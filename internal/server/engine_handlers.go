package server

import (
	"context"
	"fmt"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/planner"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *ToolHandlers) handlePythonEngine(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	capability, _ := args["capability"].(string)
	if capability == "" {
		return textError("capability is required"), nil
	}

	params, _ := args["params"].(map[string]any)
	if params == nil {
		params = make(map[string]any)
	}

	start := time.Now()
	resp, err := h.pyBridge.Execute(ctx, capability, params)
	duration := time.Since(start)

	if err != nil {
		h.opMemory.RecordError(capability, "", err.Error(), duration)
		return textError("engine error: " + err.Error()), nil
	}

	if resp.Status == "error" {
		h.opMemory.RecordError(capability, "", resp.Error, duration)

		if h.opMemory.KnownFailure(capability) {
			alternatives := h.planExec.SuggestAlternative(capability)
			if len(alternatives) > 0 {
				return jsonResult(map[string]any{
					"status": "error", "error": resp.Error,
					"suggested_alternatives": alternatives,
				}), nil
			}
		}

		return textError("capability " + capability + " failed: " + resp.Error), nil
	}

	h.opMemory.RecordSuccess(capability, "", duration)

	return jsonResult(map[string]any{
		"status": resp.Status, "capability": capability,
		"data": resp.Data, "duration": duration.Round(time.Millisecond).String(),
		"warnings": resp.Warnings,
	}), nil
}

func (h *ToolHandlers) handlePlanner(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	objective, _ := args["objective"].(string)
	if objective == "" {
		return textError("objective is required"), nil
	}

	params, _ := args["params"].(map[string]any)
	if params == nil {
		params = make(map[string]any)
	}

	start := time.Now()

	caps := h.pyBridge.Capabilities()
	capDefs := make([]planner.Capability, len(caps))
	for i, c := range caps {
		capDefs[i] = planner.Capability{Name: c.Name, Description: c.Description}
	}
	h.planner.RegisterCapabilities(capDefs)

	plan, err := h.planner.CreatePlan(ctx, objective, params)
	if err != nil {
		return textError("plan creation failed: " + err.Error()), nil
	}

	intentName := fmt.Sprintf("%d", plan.Intent.Primary)

	stepTypes := make([]struct {
		StepID     string
		Capability string
		Params     map[string]any
		DependsOn  []string
	}, len(plan.Steps))

	for i, step := range plan.Steps {
		stepParams := make(map[string]any)
		for k, v := range step.Params {
			stepParams[k] = v
		}
		for k, v := range params {
			if _, exists := stepParams[k]; !exists {
				stepParams[k] = v
			}
		}
		stepTypes[i] = struct {
			StepID     string
			Capability string
			Params     map[string]any
			DependsOn  []string
		}{
			StepID:     step.StepID,
			Capability: step.Capability,
			Params:     stepParams,
			DependsOn:  step.DependsOn,
		}
	}

	results, execErr := h.pyBridge.BatchExecute(ctx, stepTypes)

	stepResults := make([]planner.StepResult, len(results))
	overallStatus := "ok"

	for i, r := range results {
		stepRes := planner.StepResult{
			StepID:     r.StepID,
			Capability: stepTypes[i].Capability,
		}
		if r.Error != nil {
			stepRes.Status = "error"
			stepRes.Error = r.Error.Error()
			overallStatus = "failed"
		} else if r.Result != nil && r.Result.Status == "error" {
			stepRes.Status = "error"
			stepRes.Error = r.Result.Error
			overallStatus = "failed"
		} else {
			stepRes.Status = "ok"
			if r.Result != nil {
				stepRes.Data = r.Result.Data
			}
		}
		stepResults[i] = stepRes
	}

	execResult := planner.ExecutionResult{
		Objective: objective,
		Intent:    intentName,
		Status:    overallStatus,
		Steps:     stepResults,
		Duration:  time.Since(start).Round(time.Millisecond).String(),
	}
	if execErr != nil {
		execResult.FailedAt = objective
	}

	duration := time.Since(start)
	var capsUsed []string
	for _, s := range plan.Steps {
		capsUsed = append(capsUsed, s.Capability)
	}

	errStr := ""
	if overallStatus == "failed" {
		errStr = "plan failed at step " + execResult.FailedAt
	}

	h.opMemory.Record("planner", overallStatus, objective, "", duration)
	h.opMemory.RecordTask(objective, intentName, capsUsed, duration, len(plan.Steps), errStr)

	return jsonResult(map[string]any{
		"result":     execResult,
		"intent":     plan.Intent,
		"evaluation": h.evaluatePlan(execResult),
	}), nil
}

func (h *ToolHandlers) evaluatePlan(result planner.ExecutionResult) map[string]any {
	if result.Status == "failed" {
		return map[string]any{
			"goal_met":   false,
			"evidence":   "Execution failed",
			"correct":    false,
			"consistent": false,
		}
	}

	allOK := true
	evidenceCount := 0
	for _, s := range result.Steps {
		if s.Status != "ok" {
			allOK = false
		}
		if s.Data != nil {
			evidenceCount++
		}
	}

	return map[string]any{
		"goal_met":   allOK,
		"evidence":   fmt.Sprintf("All %d steps completed successfully", len(result.Steps)),
		"correct":    allOK,
		"consistent": evidenceCount == len(result.Steps),
	}
}

func (h *ToolHandlers) handleListCapabilities(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	caps := h.pyBridge.Capabilities()
	byModule := make(map[string][]map[string]string)
	for _, c := range caps {
		module := "other"
		parts := splitCap(c.Name)
		if len(parts) > 1 {
			module = parts[0]
		}
		byModule[module] = append(byModule[module], map[string]string{
			"name": c.Name, "description": c.Description,
		})
	}
	return jsonResult(map[string]any{"total": len(caps), "modules": len(byModule), "by_module": byModule}), nil
}

func (h *ToolHandlers) handleOperationalMemory(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	return jsonResult(map[string]any{
		"stats":        h.opMemory.Stats(),
		"recent":       h.opMemory.RecentEntries(10),
		"recent_tasks": h.opMemory.RecentTasks(5),
		"improvements": h.opMemory.SuggestImprovements(),
		"efficiency":   h.opMemory.Efficiency(),
		"cache_stats":  h.planner.Discovery().Count(),
	}), nil
}

func (h *ToolHandlers) handleDiscover(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	query, _ := args["query"].(string)
	if query != "" {
		return jsonResult(map[string]any{
			"modules": h.planner.Discovery().Modules(),
			"total":   h.planner.Discovery().Count(),
			"results": h.planner.Discovery().Search(query),
		}), nil
	}
	modules := h.planner.Discovery().Modules()
	byModule := make(map[string]int)
	for _, m := range modules {
		byModule[m] = len(h.planner.Discovery().FindByModule(m))
	}
	return jsonResult(map[string]any{"modules": modules, "total": len(modules), "by_module": byModule}), nil
}

func (h *ToolHandlers) handleSelfImprove(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	suggestions := h.improvement.Analyze()
	summary := "No improvements needed"
	if len(suggestions) > 0 {
		summary = fmt.Sprintf("Found %d improvement suggestion(s)", len(suggestions))
	}
	return jsonResult(map[string]any{
		"suggestions":  suggestions,
		"history":      h.improvement.History(),
		"memory_stats": h.opMemory.Stats(),
		"summary":      summary,
	}), nil
}

func splitCap(name string) []string {
	var parts []string
	current := ""
	for _, c := range name {
		if c == '.' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
