package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBenchmarkEndToEnd(t *testing.T) {
	out := t.TempDir()
	path, err := runBench(out, 1, true)
	if err != nil {
		t.Fatalf("runBench: %v", err)
	}
	if path == "" || !strings.HasSuffix(path, ".json") {
		t.Fatalf("expected a JSON result path, got %q", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	suites, ok := result["suites"].(map[string]any)
	if !ok {
		t.Fatal("missing suites object")
	}
	for _, key := range []string{"validation", "noop_preservation", "targeted_change", "conflict_detection", "rollback", "latency", "security_negative", "crash", "timeout"} {
		if _, ok := suites[key]; !ok {
			t.Errorf("missing suite key %q", key)
		}
	}
	val, ok := suites["validation"].(map[string]any)
	if !ok {
		t.Fatal("validation suite is not an object")
	}
	errs, ok := val["validation_errors"].(float64)
	if !ok || errs != 0 {
		t.Errorf("validation_errors = %v, want 0", val["validation_errors"])
	}
	if _, err := os.Stat(filepath.Join(out, "manifest.json")); err != nil {
		t.Errorf("missing out manifest.json: %v", err)
	}
}
