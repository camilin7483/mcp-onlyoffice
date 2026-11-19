package server

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// Contract audit over ToolHandlers.Definitions(): pins the public MCP tool
// surface (count, naming, metadata, schemas, core tools). Registration onto
// the MCP server (Run/addToolToServer) is covered indirectly by server tests,
// so this file asserts Definitions() only and never starts a server.
var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)

func TestToolDefinitionsContract(t *testing.T) {
	h := &ToolHandlers{}
	defs := h.Definitions()

	if len(defs) != 71 {
		t.Fatalf("Definitions() len = %d, want exactly 71", len(defs))
	}

	seen := make(map[string]struct{}, len(defs))
	for _, d := range defs {
		if _, dup := seen[d.Name]; dup {
			t.Errorf("duplicate tool name %q", d.Name)
		}
		seen[d.Name] = struct{}{}
	}

	for _, want := range []string{"create_document", "write_docx", "list_documents", "get_document", "validate_document", "replace_text"} {
		if _, ok := seen[want]; !ok {
			t.Errorf("core tool %q missing from Definitions()", want)
		}
	}

	for _, d := range defs {
		t.Run(d.Name, func(t *testing.T) {
			if !toolNamePattern.MatchString(d.Name) {
				t.Errorf("name %q must match ^[a-z][a-z0-9_]{2,63}$", d.Name)
			}
			if strings.ContainsAny(d.Name, "ABCDEFGHIJKLMNOPQRSTUVWXYZ .") {
				t.Errorf("name %q must not contain uppercase/spaces/dots", d.Name)
			}
			if d.Description == "" {
				t.Error("Description must be non-empty")
			}
			if d.Handler == nil {
				t.Fatal("Handler must be non-nil")
			}
			if d.InputSchema == nil {
				t.Fatal("InputSchema must be non-nil")
			}
			raw, err := json.Marshal(d.InputSchema)
			if err != nil {
				t.Fatalf("InputSchema not JSON-marshalable: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("InputSchema not a JSON object: %v", err)
			}
			typ, _ := m["type"].(string)
			_, hasProps := m["properties"]
			if typ != "object" && !hasProps {
				t.Errorf("InputSchema must have type object or properties, got %s", raw)
			}
		})
	}
}
