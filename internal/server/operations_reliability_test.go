package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	auditlog "github.com/cam/mcp-onlyoffice/internal/audit"
	"github.com/cam/mcp-onlyoffice/internal/ooxml"
	"github.com/cam/mcp-onlyoffice/internal/operations"
)

// TestDocumentOperationAtomicWriteFailurePreservesOriginal: a failed write
// must leave the original bytes intact and no temp file behind. The writer
// hook simulates a mid-write failure (e.g. ENOSPC after backup).
func TestDocumentOperationAtomicWriteFailurePreservesOriginal(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "atomic.docx")
	before, _ := os.ReadFile(p)

	oldWrite := writeAtomicFile
	writeAtomicFile = func(string, []byte, os.FileMode) error {
		return errors.New("no space left on device")
	}
	t.Cleanup(func() { writeAtomicFile = oldWrite })

	args := opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(1)})
	args["params"] = map[string]any{"text": "Beta-atomic"}
	args["approve"] = true
	raw, isErr := callOp(t, h, args)
	if !isErr {
		t.Fatalf("expected write failure, got: %s", raw)
	}
	if !strings.Contains(raw, "write file") {
		t.Errorf("error must name the write: %s", raw)
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(before) {
		t.Error("failed write changed the original file")
	}
	if matches, _ := filepath.Glob(filepath.Join(root, ".mcp-edit-*")); len(matches) != 0 {
		t.Errorf("temp files left behind: %v", matches)
	}
}

// TestDocumentOperationAtomicWriteLeavesNoTemp: a successful commit must not
// leave sibling temp files behind.
func TestDocumentOperationAtomicWriteLeavesNoTemp(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "clean.docx")
	args := opArgs("ooxml.docx.set_paragraph_style", p, map[string]any{"paragraph": float64(0)})
	args["params"] = map[string]any{"style": "Heading1"}
	if raw, isErr := callOp(t, h, args); isErr {
		t.Fatalf("style error: %s", raw)
	}
	if matches, _ := filepath.Glob(filepath.Join(root, ".mcp-edit-*")); len(matches) != 0 {
		t.Errorf("temp files left behind: %v", matches)
	}
}

// TestDocumentOperationConcurrentRevisionConflict: two simultaneous
// revision-checked operations on the same file serialise on the resolved
// path, so exactly one commits and the other reports a conflict.
func TestDocumentOperationConcurrentRevisionConflict(t *testing.T) {
	for round := 0; round < 20; round++ {
		h, root := testFS(t)
		p := opFixture(t, root, "race.docx")
		before, _ := os.ReadFile(p)
		rev := ooxml.Digest(before)

		mkArgs := func(text string) map[string]any {
			a := opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(0)})
			a["params"] = map[string]any{"text": text}
			a["expected_revision"] = rev
			a["approve"] = true
			return a
		}

		start := make(chan struct{})
		type outcome struct {
			raw   string
			isErr bool
		}
		out := make([]outcome, 2)
		var wg sync.WaitGroup
		for i := range 2 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				r, err := h.handleDocumentOperation(context.Background(), mkArgs("racer"))
				if err != nil {
					out[i] = outcome{raw: "go error: " + err.Error(), isErr: true}
					return
				}
				out[i] = outcome{raw: resultText(t, r), isErr: r.IsError}
			}(i)
		}
		close(start)
		wg.Wait()

		committed := 0
		conflicts := 0
		for _, o := range out {
			if !o.isErr && strings.Contains(o.raw, `"status":"committed"`) {
				committed++
			} else if o.isErr && strings.Contains(o.raw, "revision conflict") {
				conflicts++
			} else {
				t.Fatalf("round %d: unexpected outcome err=%v: %s", round, o.isErr, o.raw)
			}
		}
		if committed != 1 || conflicts != 1 {
			t.Fatalf("round %d: want exactly 1 commit + 1 conflict, got %d/%d: %+v", round, committed, conflicts, out)
		}
	}
}

// TestDocumentOperationRejectsNonRegularFile: a FIFO/socket inside the roots
// is refused before reading, so the handler can never block on it.
func TestDocumentOperationRejectsNonRegularFile(t *testing.T) {
	h, root := testFS(t)
	sock := filepath.Join(root, "op.sock")
	l, err := listenUnix(t, sock)
	if err != nil {
		t.Skipf("unix socket unavailable: %v", err)
	}
	defer l.Close()
	args := opArgs("ooxml.docx.replace_paragraph_text", sock, map[string]any{"paragraph": float64(0)})
	args["params"] = map[string]any{"text": "x"}
	args["approve"] = true
	raw, isErr := callOp(t, h, args)
	if !isErr {
		t.Fatalf("expected non-regular refusal, got: %s", raw)
	}
	if !strings.Contains(raw, "non-regular") {
		t.Fatalf("refusal must name non-regular: %s", raw)
	}
}

// TestDocumentOperationDryRunRevisionIsCurrent: a dry-run receipt carries the
// current on-disk digest as revision, so chaining it as expected_revision
// succeeds on the next real operation.
func TestDocumentOperationDryRunRevisionIsCurrent(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "dry.docx")
	args := opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(1)})
	args["params"] = map[string]any{"text": "Beta-dry-chain"}
	args["dry_run"] = true
	raw, isErr := callOp(t, h, args)
	if isErr {
		t.Fatalf("dry-run error: %s", raw)
	}
	m := decodeReceipt(t, raw)
	before, _ := m["before_digest"].(string)
	rev, _ := m["revision"].(string)
	after, _ := m["after_digest"].(string)
	if rev == "" || rev != before {
		t.Fatalf("dry-run revision %q must equal before digest %q (after %q)", rev, before, after)
	}
	if rev == after {
		t.Fatalf("dry-run revision must not be the simulated after digest")
	}
	live := opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(1)})
	live["params"] = map[string]any{"text": "Beta-dry-chain"}
	live["expected_revision"] = rev
	live["approve"] = true
	if raw, isErr := callOp(t, h, live); isErr {
		t.Fatalf("chained operation with dry-run revision failed: %s", raw)
	}
}

// TestDocumentOperationSensitiveRequiresApproval: a sensitive mutation without
// approve:true returns the diff summary with re-run instructions and writes
// nothing; with approve:true it commits.
func TestDocumentOperationSensitiveRequiresApproval(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "sens.docx")
	before, _ := os.ReadFile(p)

	args := opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(1)})
	args["params"] = map[string]any{"text": "Beta-sneaky"}
	raw, isErr := callOp(t, h, args)
	if !isErr {
		t.Fatalf("expected approval refusal, got: %s", raw)
	}
	if !strings.Contains(raw, "approve:true") {
		t.Errorf("refusal must instruct approve:true: %s", raw)
	}
	if !strings.Contains(raw, "paragraph 1") {
		t.Errorf("refusal must carry the diff summary: %s", raw)
	}
	if after, _ := os.ReadFile(p); string(after) != string(before) {
		t.Error("unapproved operation wrote to the file")
	}
	if matches, _ := filepath.Glob(p + ".bak-*"); len(matches) != 0 {
		t.Errorf("unapproved operation created a backup: %v", matches)
	}

	args["approve"] = true
	raw, isErr = callOp(t, h, args)
	if isErr {
		t.Fatalf("approved operation failed: %s", raw)
	}
	if !strings.Contains(raw, `"status":"committed"`) {
		t.Errorf("approved operation must commit: %s", raw)
	}
}

// TestDocumentOperationDestructiveRequiresApprovalEvenWhenPreApproved: a
// destructive operation is refused without approve:true even when the policy
// pre-approves it for the process; with approve:true it commits.
func TestDocumentOperationDestructiveRequiresApprovalEvenWhenPreApproved(t *testing.T) {
	h, root := testFS(t)
	p := opFixture(t, root, "destr.docx")
	before, _ := os.ReadFile(p)

	r := operations.NewRegistry()
	if err := r.Register(operations.Spec{
		Name: "ooxml.docx.replace_paragraph_text", Backend: operations.BackendOOXML,
		Risk: operations.RiskDestructive, Capability: "ooxml.replace_paragraph_text",
		Reversible: true, SupportsDryRun: true,
	}); err != nil {
		t.Fatalf("register test spec: %v", err)
	}
	h.opRegistry = r
	h.opPolicy = &operations.Policy{Approved: map[string]bool{"ooxml.docx.replace_paragraph_text": true}}

	args := opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(0)})
	args["params"] = map[string]any{"text": "Alpha-gone"}
	raw, isErr := callOp(t, h, args)
	if !isErr {
		t.Fatalf("expected destructive refusal despite pre-approval, got: %s", raw)
	}
	if !strings.Contains(raw, "approve:true") {
		t.Errorf("refusal must instruct approve:true: %s", raw)
	}
	if after, _ := os.ReadFile(p); string(after) != string(before) {
		t.Error("unapproved destructive operation wrote to the file")
	}

	args["approve"] = true
	if raw, isErr := callOp(t, h, args); isErr {
		t.Fatalf("approved destructive operation failed: %s", raw)
	}
}

// TestDocumentOperationDesktopReceiptRevision: the success receipt carries
// the session revision after the mutation; unknown outcomes carry none. A
// fake bridge is not possible (concrete *plugin.Bridge), so the receipt
// builder is covered directly.
func TestDocumentOperationDesktopReceiptRevision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		revision string
		wantRev  string
	}{
		{name: "success carries session revision", revision: "12", wantRev: "12"},
		{name: "unknown outcome carries none", revision: "", wantRev: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := desktopOpReceipt("op-1", "committed", "writer.replace_text", "d1", tc.revision, nil, nil)
			if r.Revision != tc.wantRev {
				t.Errorf("revision = %q, want %q", r.Revision, tc.wantRev)
			}
		})
	}
	r := desktopOpReceipt("op-2", "unknown_outcome", "writer.replace_text", "d1", "", nil, nil)
	r.UnknownOutcome = true
	if r.Revision != "" {
		t.Errorf("unknown_outcome revision = %q, want empty", r.Revision)
	}
}

// TestDocumentOperationWritesAuditEntry: a committed operation writes one
// audit entry with operation, digests and backup path, and no document
// content.
func TestDocumentOperationWritesAuditEntry(t *testing.T) {
	h, root := testFS(t)
	auditDir := t.TempDir()
	al, err := auditlog.New(auditDir)
	if err != nil {
		t.Fatalf("audit logger: %v", err)
	}
	defer al.Close()
	fsAudit = al

	p := opFixture(t, root, "aud.docx")
	probe := "ZxqAuditProbe9"
	args := opArgs("ooxml.docx.replace_paragraph_text", p, map[string]any{"paragraph": float64(1)})
	args["params"] = map[string]any{"text": probe}
	args["approve"] = true
	raw, isErr := callOp(t, h, args)
	if isErr {
		t.Fatalf("replace error: %s", raw)
	}
	m := decodeReceipt(t, raw)

	data, err := os.ReadFile(al.Path())
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	entry := string(data)
	for _, want := range []string{`"action":"document_operation"`, `"outcome":"ok"`, "ooxml.docx.replace_paragraph_text"} {
		if !strings.Contains(entry, want) {
			t.Errorf("audit entry missing %q: %s", want, entry)
		}
	}
	for _, want := range []string{m["before_digest"].(string), m["after_digest"].(string), ".bak-"} {
		if want != "" && !strings.Contains(entry, want) {
			t.Errorf("audit entry missing %q: %s", want, entry)
		}
	}
	if strings.Contains(entry, probe) {
		t.Errorf("audit entry must not contain document content: %s", entry)
	}
}
