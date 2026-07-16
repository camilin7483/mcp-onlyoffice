package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func textError(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}
}

// jsonResult serializes v to JSON and returns a text result.
// Automatically minimizes responses: removes nil/empty fields.
func jsonResult(v any) *mcp.CallToolResult {
	b, _ := json.Marshal(compact(v))
	return textResult(string(b))
}

// okResult returns a minimal success response.
func okResult() *mcp.CallToolResult {
	return textResult(`{"ok":true}`)
}

// compact recursively removes nil/empty/default fields to minimize token usage.
func compact(v any) any {
	switch val := v.(type) {
	case map[string]any:
		cleaned := make(map[string]any)
		for k, vv := range val {
			if cv := compact(vv); cv != nil {
				cleaned[k] = cv
			}
		}
		if len(cleaned) == 0 {
			return nil
		}
		return cleaned
	case []any:
		if len(val) == 0 {
			return nil
		}
		cleaned := make([]any, 0, len(val))
		for _, vv := range val {
			if cv := compact(vv); cv != nil {
				cleaned = append(cleaned, cv)
			}
		}
		if len(cleaned) == 0 {
			return nil
		}
		return cleaned
	case string:
		if val == "" {
			return nil
		}
		return val
	case float64:
		if val == 0 {
			return nil
		}
		return val
	case bool:
		if !val {
			return nil
		}
		return val
	case nil:
		return nil
	default:
		return val
	}
}

// toMap converts a string-keyed map from JSON unmarshaling, handling both map[string]any and other types.
func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	// Try JSON round-trip for typed structs
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
}

// toStrSlice converts []any to []string (repeated pattern consolidation).
func toStrSlice(v any) []string {
	if v == nil {
		return nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, len(raw))
	for i, item := range raw {
		out[i] = fmt.Sprint(item)
	}
	return out
}

// toStrMatrix converts []any of []any to [][]string (repeated pattern consolidation).
func toStrMatrix(v any) [][]string {
	if v == nil {
		return nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	var out [][]string
	for _, ri := range raw {
		if row, ok := ri.([]any); ok {
			r2 := make([]string, len(row))
			for j, c := range row {
				r2[j] = fmt.Sprint(c)
			}
			out = append(out, r2)
		}
	}
	return out
}

// trimError is a helper to return a short error message.
func trimError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 120 {
		msg = msg[:117] + "..."
	}
	return msg
}

// joinErrors compactly joins multiple errors.
func joinErrors(errs []string) string {
	if len(errs) == 0 {
		return ""
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return fmt.Sprintf("[%d] %s", len(errs), strings.Join(errs, "; "))
}
