package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cam/mcp-onlyoffice/internal/ooxml"
	"github.com/cam/mcp-onlyoffice/internal/operations"
)

// opFixture writes a 3-paragraph docx and returns its path.
func opFixture(t *testing.T, root, name string) string {
	t.Helper()
	data, err := ooxml.CreateDOCXFromParagraphs([]ooxml.DocxParagraph{
		{Text: "Alpha", Size: 22},
		{Text: "Beta", Size: 22},
		{Text: "Gamma", Size: 22},
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	p := filepath.Join(root, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

func callOp(t *testing.T, h *ToolHandlers, args map[string]any) (string, bool) {
	t.Helper()
	r, err := h.handleDocumentOperation(context.Background(), args)
	if err != nil {
		t.Fatalf("handler Go error (must return tool error): %v", err)
	}
	return resultText(t, r), r.IsError
}

func decodeReceipt(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("receipt is not JSON: %v\n%s", err, raw)
	}
	return m
}

func opArgs(name, path string, extra map[string]any) map[string]any {
	target := map[string]any{"path": path}
	for k, v := range extra {
		target[k] = v
	}
	return map[string]any{"operation": name, "target": target}
}

func TestDocumentOperationUnknown(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "u.docx")
	raw, isErr := callOp(t, h, opArgs("nope.unknown", p, nil))
	if !isErr {
		t.Fatalf("expected unknown-operation error, got: %s", raw)
	}
	if !strings.Contains(raw, "ooxml.docx.replace_paragraph_text") {
		t.Errorf("error must list available names: %s", raw)
	}
}

func TestDocumentOperationPolicyDenial(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "p.docx")
	r := operations.NewRegistry()
	if err := r.Register(operations.Spec{
		Name: "net.fetch", Backend: operations.BackendPython,
		Risk: operations.RiskNetwork, Capability: "net.fetch",
	}); err != nil {
		t.Fatalf("register test spec: %v", err)
	}
	h.opRegistry = r
	raw, isErr := callOp(t, h, opArgs("net.fetch", p, nil))
	if !isErr {
		t.Fatalf("expected policy denial, got: %s", raw)
	}
	if !strings.Contains(raw, "disabled by default") {
		t.Errorf("denial must carry the reason: %s", raw)
	}
}

func TestDocumentOperationPathOutsideRoots(t *testing.T) {
	h, _ := testFS(t)
	raw, isErr := callOp(t, h, opArgs("ooxml.docx.replace_paragraph_text",
		filepath.Join(os.TempDir(), "escape.docx"), map[string]any{"paragraph": float64(0)}))
	if !isErr {
		t.Fatalf("expected roots refusal, got: %s", raw)
	}
	if !strings.Contains(raw, "path outside allowed roots") {
		t.Errorf("refusal must name the cause: %s", raw)
	}
}

func TestDocumentOperationDryRunWritesNothing(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "d.docx")
	before, _ := os.ReadFile(p)
	args := opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(1)})
	args["params"] = map[string]any{"text": "Beta-dry"}
	args["dry_run"] = true
	raw, isErr := callOp(t, h, args)
	if isErr {
		t.Fatalf("dry-run error: %s", raw)
	}
	m := decodeReceipt(t, raw)
	if m["status"] != "dry_run" {
		t.Errorf("status = %v, want dry_run", m["status"])
	}
	if m["before_digest"] == nil || m["after_digest"] == nil {
		t.Errorf("dry-run receipt must carry both digests: %s", raw)
	}
	if d, ok := m["diff"].(map[string]any); !ok || d["summary"] == nil {
		t.Errorf("dry-run receipt must carry a diff: %s", raw)
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(before) {
		t.Error("dry-run wrote to the file")
	}
	if matches, _ := filepath.Glob(p + ".bak-*"); len(matches) != 0 {
		t.Errorf("dry-run created a backup: %v", matches)
	}
}

func TestDocumentOperationRevisionMismatch(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "r.docx")
	before, _ := os.ReadFile(p)
	args := opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(0)})
	args["params"] = map[string]any{"text": "Alpha2"}
	args["expected_revision"] = "deadbeefdeadbeef"
	raw, isErr := callOp(t, h, args)
	if !isErr {
		t.Fatalf("expected conflict error, got: %s", raw)
	}
	if !strings.Contains(raw, "revision conflict") {
		t.Errorf("error must name the conflict: %s", raw)
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(before) {
		t.Error("conflict attempt wrote to the file")
	}
}

func TestDocumentOperationReplaceReceipt(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "ok.docx")
	before, _ := os.ReadFile(p)
	args := opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(1)})
	args["params"] = map[string]any{"text": "Beta-live"}
	raw, isErr := callOp(t, h, args)
	if isErr {
		t.Fatalf("replace error: %s", raw)
	}
	m := decodeReceipt(t, raw)
	t.Logf("end-to-end receipt: %s", raw)
	if m["transaction_id"] == nil || m["transaction_id"] == "" {
		t.Errorf("receipt missing transaction_id: %s", raw)
	}
	if m["status"] != "committed" {
		t.Errorf("status = %v, want committed", m["status"])
	}
	if m["operation"] != "ooxml.docx.replace_paragraph_text" {
		t.Errorf("operation = %v", m["operation"])
	}
	bd, _ := m["before_digest"].(string)
	ad, _ := m["after_digest"].(string)
	if len(bd) != 16 || len(ad) != 16 || bd == ad {
		t.Errorf("bad digests before=%q after=%q", bd, ad)
	}
	if m["revision"] != ad {
		t.Errorf("revision = %v, want after digest %q", m["revision"], ad)
	}
	if m["validated"] != true {
		t.Errorf("validated = %v, want true", m["validated"])
	}
	if d, ok := m["diff"].(map[string]any); !ok || !strings.Contains(d["summary"].(string), "paragraph 1") {
		t.Errorf("receipt must carry a paragraph diff: %s", raw)
	}
	if got := ooxml.Digest(before); got != bd {
		t.Errorf("before digest %q != file digest %q", bd, got)
	}
	data, _ := os.ReadFile(p)
	got, err := ooxml.ParagraphText(data, 1)
	if err != nil || got != "Beta-live" {
		t.Errorf("paragraph 1 = %q, err=%v", got, err)
	}
}

func TestDocumentOperationBackupCreated(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "b.docx")
	before, _ := os.ReadFile(p)
	args := opArgs("ooxml.docx.set_paragraph_style", p, map[string]any{"paragraph": float64(0)})
	args["params"] = map[string]any{"style": "Heading1"}
	if raw, isErr := callOp(t, h, args); isErr {
		t.Fatalf("style error: %s", raw)
	}
	matches, _ := filepath.Glob(p + ".bak-*")
	if len(matches) != 1 {
		t.Fatalf("want 1 backup, got %v", matches)
	}
	bak, _ := os.ReadFile(matches[0])
	if string(bak) != string(before) {
		t.Error("backup does not hold the original bytes")
	}
}

func TestDocumentOperationMalformedUntouched(t *testing.T) {
	h, root := testFS(t)
	p := filepath.Join(root, "bad.docx")
	garbage := []byte("not a zip package at all")
	if err := os.WriteFile(p, garbage, 0o644); err != nil {
		t.Fatal(err)
	}
	args := opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(0)})
	args["params"] = map[string]any{"text": "x"}
	raw, isErr := callOp(t, h, args)
	if !isErr {
		t.Fatalf("expected malformed error, got: %s", raw)
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(garbage) {
		t.Error("malformed file was touched")
	}
	if matches, _ := filepath.Glob(p + ".bak-*"); len(matches) != 0 {
		t.Errorf("failed edit created a backup: %v", matches)
	}
}

func TestDocumentOperationEditFailureUnchanged(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "f.docx")
	before, _ := os.ReadFile(p)
	args := opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(99)})
	args["params"] = map[string]any{"text": "x"}
	raw, isErr := callOp(t, h, args)
	if !isErr {
		t.Fatalf("expected out-of-range error, got: %s", raw)
	}
	if !strings.Contains(raw, "out of range") {
		t.Errorf("error must name the cause: %s", raw)
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(before) {
		t.Error("failed edit changed the file")
	}
	if matches, _ := filepath.Glob(p + ".bak-*"); len(matches) != 0 {
		t.Errorf("failed edit created a backup: %v", matches)
	}
}

func TestDocumentOperationMissingParam(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "m.docx")
	raw, isErr := callOp(t, h, opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(0)}))
	if !isErr {
		t.Fatalf("expected missing-param error, got: %s", raw)
	}
	if !strings.Contains(raw, "params") {
		t.Errorf("error must name params: %s", raw)
	}
}

func TestDocumentOperationDesktopNoSession(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "w.docx")
	args := opArgs("writer.replace_text", p, map[string]any{"document_id": "d"})
	args["params"] = map[string]any{"old": "a", "new": "b"}
	raw, isErr := callOp(t, h, args)
	if !isErr {
		t.Fatalf("expected no-session error, got: %s", raw)
	}
	if !strings.Contains(raw, "plugin not connected") {
		t.Errorf("error must name the missing backend: %s", raw)
	}
}

func TestDocumentOperationCellSet(t *testing.T) {
	h, root := testFS(t)
	data, err := ooxml.CreateXLSXFromData("Data", []string{"Name"}, [][]string{{"Ann"}})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	p := filepath.Join(root, "c.xlsx")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	args := opArgs("ooxml.xlsx.set_cell", p, nil)
	args["params"] = map[string]any{"sheet": "Data", "cell": "B2", "value": "9"}
	raw, isErr := callOp(t, h, args)
	if isErr {
		t.Fatalf("set_cell error: %s", raw)
	}
	m := decodeReceipt(t, raw)
	if m["status"] != "committed" || m["validated"] != true {
		t.Errorf("bad receipt: %s", raw)
	}
}
