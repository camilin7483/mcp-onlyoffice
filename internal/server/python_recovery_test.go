package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/engine"
)

// timeoutRunnerScript sleeps when params.sleep is set, answers otherwise.
// __list__ and __version__ always answer immediately so Start succeeds.
const timeoutRunnerScript = `#!/usr/bin/env python3
import sys, json, time
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    rid = req.get("id")
    cap = req.get("capability", "")
    if cap == "__list__":
        print(json.dumps({"id": rid, "status": "ok", "data": []}), flush=True)
        continue
    if cap == "__version__":
        print(json.dumps({"id": rid, "status": "ok", "data": {"version": "1.0.0"}}), flush=True)
        continue
    if (req.get("params") or {}).get("sleep"):
        time.sleep(5)
    print(json.dumps({"id": rid, "status": "ok", "data": {"cap": cap}}), flush=True)
`

// TestPythonEngineGateSurvivesTimeout (finding 1): after a timed-out Execute
// the startup gate must stay open and the next Execute must restart and
// succeed, instead of every later call being refused.
func TestPythonEngineGateSurvivesTimeout(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "runner.py"), []byte(timeoutRunnerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	b := engine.NewBridge(dir)
	b.Timeout = 200 * time.Millisecond
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop() })

	h := &ToolHandlers{pyBridge: b, pythonAvailable: true}

	if _, err := b.Execute(ctx, "test.slow", map[string]any{"sleep": true}); err == nil ||
		!engine.IsOutcomeUnknown(err) {
		t.Fatalf("expected OutcomeUnknownError, got: %v", err)
	}
	if bad := h.requirePythonEngine(); bad != nil {
		t.Fatalf("gate must stay open after a timeout, got: %s", resultText(t, bad))
	}
	resp, err := b.Execute(ctx, "test.fast", map[string]any{})
	if err != nil {
		t.Fatalf("fast call after timeout: %v", err)
	}
	m, ok := resp.Data.(map[string]any)
	if !ok || m["cap"] != "test.fast" {
		t.Fatalf("stale/wrong answer delivered: %#v", resp.Data)
	}
	if bad := h.requirePythonEngine(); bad != nil {
		t.Fatalf("gate must stay open after restart, got: %s", resultText(t, bad))
	}
}

// TestPythonEngineGateClosedWithoutStartup (finding 1): a backend that never
// started stays refused.
func TestPythonEngineGateClosedWithoutStartup(t *testing.T) {
	h := &ToolHandlers{pyBridge: nil, pythonAvailable: false}
	if bad := h.requirePythonEngine(); bad == nil {
		t.Fatal("expected refusal without a backend")
	} else if txt := resultText(t, bad); !strings.Contains(txt, "python engine unavailable") {
		t.Fatalf("error must name the missing backend, got: %s", txt)
	}
}
