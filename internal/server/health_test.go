package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cam/mcp-onlyoffice/internal/engine"
	"github.com/cam/mcp-onlyoffice/internal/plugin"
	"github.com/cam/mcp-onlyoffice/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// failedPythonBridge returns a bridge whose Start failed (nonexistent
// runner), mirroring a core boot without the python backend.
func failedPythonBridge(t *testing.T) *engine.Bridge {
	t.Helper()
	b := engine.NewBridge(filepath.Join(t.TempDir(), "no-engine-here"))
	if err := b.Start(context.Background()); err == nil {
		t.Fatal("expected Start to fail against a nonexistent runner")
	}
	t.Cleanup(func() { _ = b.Stop() })
	return b
}

// healthFixture wires real storage in a temp dir with an isolated HOME so
// the plugin bridge token never touches the real user home.
func healthFixture(t *testing.T, pyBridge *engine.Bridge) (*ToolHandlers, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	s, err := storage.NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	pb := plugin.NewBridge(0)
	h := NewToolHandlers(s, "", pb, pyBridge, NewSessionCache(), NewToolRegistry())

	oldRoots := fsRoots
	t.Cleanup(func() { fsRoots = oldRoots })
	h.SetAllowedRoots(dir)
	return h, pb.Token()
}

func healthMap(t *testing.T, h *ToolHandlers) (map[string]any, string) {
	t.Helper()
	r, err := h.handleHealth(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handleHealth error: %v", err)
	}
	if r.IsError {
		t.Fatalf("handleHealth IsError: %s", resultText(t, r))
	}
	raw := resultText(t, r)
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("health is not JSON: %v\n%s", err, raw)
	}
	return m, raw
}

func TestHealthDegradedPython(t *testing.T) {
	h, _ := healthFixture(t, failedPythonBridge(t))
	m, _ := healthMap(t, h)

	if m["core_ready"] != true {
		t.Errorf("core_ready = %v, want true", m["core_ready"])
	}
	if m["builder_available"] != false {
		t.Errorf("builder_available = %v, want false", m["builder_available"])
	}
	if m["python_available"] != false {
		t.Errorf("python_available = %v, want false", m["python_available"])
	}
	if m["python_capabilities"] != nil && m["python_capabilities"] != float64(0) {
		t.Errorf("python_capabilities = %v, want absent/0", m["python_capabilities"])
	}
	if got, want := m["tools_registered"], float64(len(h.Definitions())); got != want {
		t.Errorf("tools_registered = %v, want %v", got, want)
	}
	if m["storage_writable"] != true {
		t.Errorf("storage_writable = %v, want true", m["storage_writable"])
	}
	if got, want := m["allowed_roots"], float64(1); got != want {
		t.Errorf("allowed_roots = %v, want %v", got, want)
	}
	if m["document_server_available"] != false {
		t.Errorf("document_server_available = %v, want false", m["document_server_available"])
	}
	if m["toolset"] != "legacy" {
		t.Errorf("toolset = %v, want legacy", m["toolset"])
	}
	if m["version"] == nil || m["version"] == "" {
		t.Error("version must be non-empty")
	}
}

func TestHealthLeaksNoSecrets(t *testing.T) {
	h, token := healthFixture(t, failedPythonBridge(t))
	if token == "" {
		t.Fatal("fixture must expose a known bridge token")
	}
	_, raw := healthMap(t, h)
	if strings.Contains(raw, token) {
		t.Error("health JSON must not contain the bridge token")
	}
}

func TestPythonEngineUnavailableNoPanic(t *testing.T) {
	h, _ := healthFixture(t, failedPythonBridge(t))
	ctx := context.Background()

	r, err := h.handlePythonEngine(ctx, map[string]any{
		"capability": "excel.create_workbook", "params": map[string]any{},
	})
	if err != nil {
		t.Fatalf("handler returned Go error (must return tool error): %v", err)
	}
	if !r.IsError {
		t.Fatal("expected IsError when the engine is unavailable")
	}
	if txt := resultText(t, r); !strings.Contains(txt, "python engine unavailable") {
		t.Fatalf("error must name the missing dependency, got: %s", txt)
	}

	for name, args := range map[string]map[string]any{
		"planner":           {"objective": "x"},
		"list_capabilities": {},
		"create_report":     {"title": "x", "output_dir": "x"},
	} {
		var r2 *mcp.CallToolResult
		switch name {
		case "planner":
			r2, err = h.handlePlanner(ctx, args)
		case "list_capabilities":
			r2, err = h.handleListCapabilities(ctx, args)
		case "create_report":
			r2, err = h.handleCreateReport(ctx, args)
		}
		if err != nil {
			t.Fatalf("%s: Go error (must return tool error): %v", name, err)
		}
		if !r2.IsError {
			t.Fatalf("%s: expected IsError when the engine is unavailable", name)
		}
		if txt := resultText(t, r2); !strings.Contains(txt, "python engine unavailable") {
			t.Fatalf("%s: error must name the missing dependency, got: %s", name, txt)
		}
	}
}
