package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cam/mcp-onlyoffice/internal/builder"
)

// builderHandlerFixture wires a ToolHandlers scoped to a temp root plus a
// fake builder script that answers --version and writes "ok" to its last argv.
func builderHandlerFixture(t *testing.T) (*ToolHandlers, string) {
	t.Helper()
	h, dir := testFS(t)
	script := filepath.Join(t.TempDir(), "docbuilder")
	body := `#!/bin/sh
if [ "$1" = "--version" ]; then echo 'docbuilder-test 9.9'; exit 0; fi
eval last=\$$#
echo ok > "$last"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	b := builder.Detect(script)
	b.SetRoots(h.effectiveRoots())
	h.SetBuilder(b)
	return h, dir
}

func TestBuilderRenderConvertMode(t *testing.T) {
	h, dir := builderHandlerFixture(t)
	in := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.docx")
	r, err := h.handleBuilderRender(context.Background(), map[string]any{
		"input": in, "output": out,
	})
	if err != nil {
		t.Fatalf("handler returned Go error: %v", err)
	}
	if r.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(t, r))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(resultText(t, r)), &m); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if m["output"] != out {
		t.Errorf("output = %v, want %v", m["output"], out)
	}
	if m["bytes"] == nil || m["bytes"] == float64(0) {
		t.Errorf("bytes = %v, want non-zero", m["bytes"])
	}
	if v, _ := m["builder_version"].(string); !strings.Contains(v, "9.9") {
		t.Errorf("builder_version = %v, want it to contain 9.9", m["builder_version"])
	}
}

func TestBuilderRenderModeRequiresFormat(t *testing.T) {
	h, dir := builderHandlerFixture(t)
	in := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := h.handleBuilderRender(context.Background(), map[string]any{
		"input": in, "output": filepath.Join(dir, "o.pdf"), "mode": "render",
	})
	if err != nil {
		t.Fatalf("handler returned Go error: %v", err)
	}
	if !r.IsError || !strings.Contains(resultText(t, r), "format is required") {
		t.Errorf("want a format-required tool error, got: %s", resultText(t, r))
	}

	r, err = h.handleBuilderRender(context.Background(), map[string]any{
		"input": in, "output": filepath.Join(dir, "o.pdf"), "mode": "render", "format": "pdf",
	})
	if err != nil {
		t.Fatalf("handler returned Go error: %v", err)
	}
	if r.IsError {
		t.Errorf("unexpected tool error: %s", resultText(t, r))
	}
}

func TestBuilderRenderRejectsBadModeAndOutsideRoots(t *testing.T) {
	h, dir := builderHandlerFixture(t)
	in := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ := h.handleBuilderRender(context.Background(), map[string]any{
		"input": in, "output": filepath.Join(dir, "o"), "mode": "bogus",
	})
	if !r.IsError {
		t.Error("expected IsError for a bad mode")
	}
	r, _ = h.handleBuilderRender(context.Background(), map[string]any{
		"input": filepath.Join(t.TempDir(), "evil.txt"), "output": filepath.Join(dir, "o"),
	})
	if !r.IsError {
		t.Error("expected IsError for input outside the allowed roots")
	}
}
