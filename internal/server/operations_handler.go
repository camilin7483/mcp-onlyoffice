package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cam/mcp-onlyoffice/internal/ooxml"
	"github.com/cam/mcp-onlyoffice/internal/operations"
	"github.com/cam/mcp-onlyoffice/internal/plugin"
	"github.com/cam/mcp-onlyoffice/internal/storage"
	"github.com/cam/mcp-onlyoffice/internal/validator"
)

// document_operation wires the typed operations model to its backends:
// in-place OOXML file edits and the desktop editor bridge. Every mutation
// flows through DecodeOperation -> registry -> policy -> revision check ->
// atomic write with backup -> validator, and settles with a receipt.
func defaultOperationRegistry() *operations.Registry {
	r := operations.NewRegistry()
	for _, s := range []operations.Spec{
		{Name: "ooxml.docx.replace_paragraph_text", Backend: operations.BackendOOXML, Risk: operations.RiskSensitiveMutation, Capability: "ooxml.replace_paragraph_text", Reversible: true, SupportsDryRun: true},
		{Name: "ooxml.docx.insert_paragraph_after", Backend: operations.BackendOOXML, Risk: operations.RiskSafeMutation, Capability: "ooxml.insert_paragraph_after", Reversible: true, SupportsDryRun: true},
		{Name: "ooxml.docx.set_paragraph_style", Backend: operations.BackendOOXML, Risk: operations.RiskSafeMutation, Capability: "ooxml.set_paragraph_style", Reversible: true, SupportsDryRun: true},
		{Name: "ooxml.xlsx.set_cell", Backend: operations.BackendOOXML, Risk: operations.RiskSensitiveMutation, Capability: "ooxml.set_cell", Reversible: true, SupportsDryRun: true},
		{Name: "writer.replace_text", Backend: operations.BackendDesktop, Risk: operations.RiskSensitiveMutation, Capability: "writer.replace_text", Reversible: false, SupportsDryRun: false},
		{Name: "writer.insert_text", Backend: operations.BackendDesktop, Risk: operations.RiskSafeMutation, Capability: "writer.insert_text", Reversible: false, SupportsDryRun: false},
	} {
		_ = r.Register(s) // specs are static and valid; duplicates impossible here
	}
	return r
}

func (h *ToolHandlers) operationRegistry() *operations.Registry {
	if h.opRegistry != nil {
		return h.opRegistry
	}
	return defaultOperationRegistry()
}

func (h *ToolHandlers) operationPolicy() *operations.Policy {
	if h.opPolicy != nil {
		return h.opPolicy
	}
	return &operations.Policy{}
}

// opReceipt is the settlement record handed to the caller.
type opReceipt struct {
	TransactionID  string           `json:"transaction_id"`
	Status         string           `json:"status"`
	Operation      string           `json:"operation"`
	DocumentID     string           `json:"document_id,omitempty"`
	BeforeDigest   string           `json:"before_digest,omitempty"`
	AfterDigest    string           `json:"after_digest,omitempty"`
	Revision       string           `json:"revision,omitempty"`
	Diff           *operations.Diff `json:"diff,omitempty"`
	Validated      bool             `json:"validated"`
	UnknownOutcome bool             `json:"unknown_outcome,omitempty"`
	Matches        *int             `json:"matches,omitempty"`
}

func receiptResult(r opReceipt) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return textError("encode receipt: " + err.Error()), nil
	}
	return textResult(string(b)), nil
}

// memStore serves one in-memory file to validator.Validate so a
// filesystem result can be validated before it is written.
type memStore struct {
	filename string
	data     []byte
}

func (m memStore) Create(string, []byte) (*storage.Document, error) {
	return nil, fmt.Errorf("memStore: read-only")
}
func (m memStore) Get(string) (*storage.Document, error) {
	mime := "application/octet-stream"
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(m.filename), ".")) {
	case "docx":
		mime = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "xlsx":
		mime = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	return &storage.Document{ID: "op", Filename: filepath.Base(m.filename), Size: int64(len(m.data)), MimeType: mime}, nil
}
func (m memStore) GetData(string) ([]byte, error) { return m.data, nil }
func (m memStore) List() ([]storage.Document, error) {
	return nil, fmt.Errorf("memStore: read-only")
}
func (m memStore) Delete(string) error { return fmt.Errorf("memStore: read-only") }
func (m memStore) Update(string, []byte) (*storage.Document, error) {
	return nil, fmt.Errorf("memStore: read-only")
}
func (m memStore) AddRef(string, string) error { return fmt.Errorf("memStore: read-only") }
func (m memStore) GetRefs(string) ([]string, error) {
	return nil, fmt.Errorf("memStore: read-only")
}

// validateBytes runs the shared validator over in-memory package bytes.
func validateBytes(filename string, data []byte) bool {
	v := validator.New(memStore{filename: filename, data: data})
	res, err := v.Validate("op")
	return err == nil && res.Valid
}

func newTxID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("transaction id: %w", err)
	}
	return "op-" + hex.EncodeToString(b[:]), nil
}

func strParam(params map[string]any, key string, required bool) (string, error) {
	v, ok := params[key]
	if !ok {
		if required {
			return "", fmt.Errorf("params: missing %q", key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("params: %q must be a string", key)
	}
	return s, nil
}

func boolParam(params map[string]any, key string) (bool, error) {
	v, ok := params[key]
	if !ok {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("params: %q must be a boolean", key)
	}
	return b, nil
}

func (h *ToolHandlers) handleDocumentOperation(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	approve, err := boolParam(args, "approve")
	if err != nil {
		return textError(err.Error()), nil
	}
	// approve lives only in the tool schema; the typed operation model
	// does not know it, so keep it out of DecodeOperation input.
	decodeArgs := make(map[string]any, len(args))
	for k, v := range args {
		if k == "approve" {
			continue
		}
		decodeArgs[k] = v
	}
	raw, err := json.Marshal(decodeArgs)
	if err != nil {
		return textError("invalid operation: " + err.Error()), nil
	}
	op, err := operations.DecodeOperation(raw)
	if err != nil {
		return textError("invalid operation: " + err.Error()), nil
	}
	reg := h.operationRegistry()
	spec, ok := reg.Lookup(op.Name)
	if !ok {
		return textError(fmt.Sprintf("unknown operation %q (available: %s)", op.Name, strings.Join(reg.Names(), ", "))), nil
	}
	decision := h.operationPolicy().Decide(spec)
	if !decision.Allow {
		return textError("operation denied: " + decision.Reason), nil
	}
	if op.DryRun && !spec.SupportsDryRun {
		return textError(fmt.Sprintf("operation %q does not support dry_run", op.Name)), nil
	}
	txID, err := newTxID()
	if err != nil {
		return textError(err.Error()), nil
	}
	switch spec.Backend {
	case operations.BackendOOXML:
		return h.applyOOXMLOperation(op, spec, decision, approve, txID)
	case operations.BackendDesktop:
		return h.applyDesktopOperation(ctx, op, spec, decision, approve, txID)
	default:
		return textError(fmt.Sprintf("backend %s is not supported by document_operation", spec.Backend)), nil
	}
}

// opPathLocks serialises concurrent document_operation calls on the same
// file, so read -> revision check -> apply -> write is atomic per resolved
// path. Entries are removed once idle so the map does not grow.
var (
	opPathMu    sync.Mutex
	opPathLocks = make(map[string]*opPathEntry)
)

type opPathEntry struct {
	mu   sync.Mutex
	refs int
}

func acquireOpPathLock(path string) func() {
	opPathMu.Lock()
	e, ok := opPathLocks[path]
	if !ok {
		e = &opPathEntry{}
		opPathLocks[path] = e
	}
	e.refs++
	opPathMu.Unlock()
	e.mu.Lock()
	return func() {
		e.mu.Unlock()
		opPathMu.Lock()
		defer opPathMu.Unlock()
		e.refs--
		if e.refs <= 0 {
			delete(opPathLocks, path)
		}
	}
}

// writeAtomicFile swaps the new package bytes over the destination without
// ever truncating it in place: temp file in the same directory, Sync,
// Chmod to the original mode, Rename. The temp file is removed on every
// error path. Tests override it to simulate a failed write.
var writeAtomicFile = atomicWriteFile

func atomicWriteFile(resolved string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(resolved), ".mcp-edit-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	done := false
	defer func() {
		if !done {
			_ = os.Remove(name)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	if err := os.Rename(name, resolved); err != nil {
		return err
	}
	done = true
	return nil
}

// applyOOXMLOperation edits one file in place, serialised per resolved path:
// read -> revision check -> apply -> validate new bytes in memory (nothing
// is written when invalid) -> approval gate -> dry-run receipt or backup +
// atomic write -> receipt. The dry-run receipt carries the current on-disk
// digest as revision, so chaining it as expected_revision succeeds.
func (h *ToolHandlers) applyOOXMLOperation(op operations.Operation, spec operations.Spec, decision operations.Decision, approve bool, txID string) (*mcp.CallToolResult, error) {
	path := op.Target.Path
	if path == "" {
		return textError("target.path is required for ooxml operations"), nil
	}
	resolved, err := storage.ResolveDest(path, h.effectiveRoots())
	if err != nil {
		return textError(err.Error()), nil
	}
	unlock := acquireOpPathLock(resolved)
	defer unlock()
	info, err := os.Lstat(resolved)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return textError(fmt.Sprintf("refusing to follow symlink: %s", resolved)), nil
	}
	if !info.Mode().IsRegular() {
		return textError(fmt.Sprintf("refusing to read non-regular file: %s", resolved)), nil
	}
	if info.Size() > 64<<20 {
		return textError(fmt.Sprintf("file is %d bytes, over the 64 MiB limit", info.Size())), nil
	}
	mode := info.Mode().Perm()
	data, err := os.ReadFile(resolved)
	if err != nil {
		return textError("read file: " + err.Error()), nil
	}
	before := ooxml.Digest(data)
	if op.ExpectedRevision != "" {
		if err := operations.CheckRevision(op.ExpectedRevision, before); err != nil {
			return textError(err.Error()), nil
		}
	}
	newBytes, diff, err := applyOOXMLEdit(data, op)
	if err != nil {
		return textError(err.Error()), nil
	}
	after := ooxml.Digest(newBytes)
	validated := validateBytes(resolved, newBytes)
	tx := operations.NewTransaction(txID, op.Target.DocumentID, op.Name, before)
	if !validated {
		// Nothing was written yet, so the original is untouched; report
		// the failure instead of a receipt with validated:false.
		return textError("validation failed: edited package did not pass OOXML validation"), nil
	}
	if op.DryRun {
		return receiptResult(opReceipt{
			TransactionID: txID, Status: "dry_run", Operation: op.Name,
			DocumentID: op.Target.DocumentID, BeforeDigest: before,
			AfterDigest: after, Revision: before, Diff: diff, Validated: true,
		})
	}
	// Sensitive and destructive mutations need an explicit approve:true.
	// The error carries the diff summary so the caller can review it, and
	// nothing is written or backed up on this path. Destructive always
	// requires approval, even when pre-approved for the process.
	if spec.Risk == operations.RiskDestructive && !approve {
		return textError(fmt.Sprintf("operation %q requires approval (destructive): %s; re-run with approve:true", op.Name, diff.Summary)), nil
	}
	if decision.RequireApproval && !approve {
		return textError(fmt.Sprintf("operation %q requires approval (%s): %s; re-run with approve:true (or dry_run:true first)", op.Name, spec.Risk, diff.Summary)), nil
	}
	// Single backup of the original bytes (prepareWrite equivalent, kept
	// local so the backup path is available for the audit entry).
	bak := resolved + ".bak-" + storage.BackupTimestamp()
	if err := writeBackup(resolved, bak); err != nil {
		return textError("backup write: " + err.Error()), nil
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return textError("create directory: " + err.Error()), nil
	}
	if err := writeAtomicFile(resolved, newBytes, mode); err != nil {
		return textError("write file: " + err.Error()), nil
	}
	rc, err := tx.Commit(after)
	if err != nil {
		return textError(err.Error()), nil
	}
	detail := map[string]any{"operation": op.Name, "before": before, "after": after, "backup": bak}
	auditLog("document_operation", resolved, "ok", detail)
	return receiptResult(opReceipt{
		TransactionID: rc.TransactionID, Status: string(rc.Status), Operation: op.Name,
		DocumentID: op.Target.DocumentID, BeforeDigest: rc.BeforeDigest,
		AfterDigest: rc.AfterDigest, Revision: after, Diff: diff, Validated: true,
	})
}

// applyOOXMLEdit routes one decoded operation to the in-place editor and
// builds its reviewable diff. where values never carry absolute paths.
func applyOOXMLEdit(data []byte, op operations.Operation) ([]byte, *operations.Diff, error) {
	switch op.Name {
	case "ooxml.docx.replace_paragraph_text":
		if op.Target.Paragraph == nil {
			return nil, nil, fmt.Errorf("target.paragraph is required for %s", op.Name)
		}
		text, err := strParam(op.Params, "text", true)
		if err != nil {
			return nil, nil, err
		}
		idx := *op.Target.Paragraph
		beforeText, err := ooxml.ParagraphText(data, idx)
		if err != nil {
			return nil, nil, err
		}
		out, err := ooxml.ReplaceParagraphText(data, idx, text)
		if err != nil {
			return nil, nil, err
		}
		return out, &operations.Diff{
			Changes: []operations.Change{{Kind: "replace", Before: beforeText, After: text, Where: fmt.Sprintf("paragraph %d", idx)}},
			Summary: fmt.Sprintf("replace paragraph %d: %d chars -> %d chars", idx, len([]rune(beforeText)), len([]rune(text))),
		}, nil
	case "ooxml.docx.insert_paragraph_after":
		if op.Target.Paragraph == nil {
			return nil, nil, fmt.Errorf("target.paragraph is required for %s", op.Name)
		}
		text, err := strParam(op.Params, "text", true)
		if err != nil {
			return nil, nil, err
		}
		style, err := strParam(op.Params, "style", false)
		if err != nil {
			return nil, nil, err
		}
		idx := *op.Target.Paragraph
		out, err := ooxml.InsertParagraphAfter(data, idx, text, style)
		if err != nil {
			return nil, nil, err
		}
		return out, &operations.Diff{
			Changes: []operations.Change{{Kind: "insert", After: text, Where: fmt.Sprintf("after paragraph %d", idx)}},
			Summary: fmt.Sprintf("insert paragraph after %d: %d chars", idx, len([]rune(text))),
		}, nil
	case "ooxml.docx.set_paragraph_style":
		if op.Target.Paragraph == nil {
			return nil, nil, fmt.Errorf("target.paragraph is required for %s", op.Name)
		}
		style, err := strParam(op.Params, "style", true)
		if err != nil {
			return nil, nil, err
		}
		idx := *op.Target.Paragraph
		beforeText, err := ooxml.ParagraphText(data, idx)
		if err != nil {
			return nil, nil, err
		}
		out, err := ooxml.SetParagraphStyle(data, idx, style)
		if err != nil {
			return nil, nil, err
		}
		return out, &operations.Diff{
			Changes: []operations.Change{{Kind: "format", Before: beforeText, After: beforeText, Where: fmt.Sprintf("paragraph %d style -> %s", idx, style)}},
			Summary: fmt.Sprintf("set paragraph %d style to %s", idx, style),
		}, nil
	case "ooxml.xlsx.set_cell":
		sheet, err := strParam(op.Params, "sheet", false)
		if err != nil {
			return nil, nil, err
		}
		cell, err := strParam(op.Params, "cell", true)
		if err != nil {
			return nil, nil, err
		}
		value, err := strParam(op.Params, "value", true)
		if err != nil {
			return nil, nil, err
		}
		formula, err := boolParam(op.Params, "formula")
		if err != nil {
			return nil, nil, err
		}
		numberFormat, err := strParam(op.Params, "number_format", false)
		if err != nil {
			return nil, nil, err
		}
		out, err := ooxml.SetCellValue(data, sheet, cell, value, formula, numberFormat)
		if err != nil {
			return nil, nil, err
		}
		where := "cell " + strings.ToUpper(strings.TrimSpace(cell))
		if sheet != "" {
			where += " of sheet " + sheet
		}
		return out, &operations.Diff{
			Changes: []operations.Change{{Kind: "replace", Before: "", After: value, Where: where}},
			Summary: fmt.Sprintf("set %s to %q", where, value),
		}, nil
	default:
		return nil, nil, fmt.Errorf("operation %q is not an ooxml file operation", op.Name)
	}
}

// desktopPreviewDiff builds the reviewable diff for an approval refusal
// without touching the editor, from the operation params.
func desktopPreviewDiff(op operations.Operation) *operations.Diff {
	switch op.Name {
	case "writer.replace_text":
		oldT := fmt.Sprint(op.Params["old"])
		newT := fmt.Sprint(op.Params["new"])
		return &operations.Diff{
			Changes: []operations.Change{{Kind: "replace", Before: oldT, After: newT, Where: "open document"}},
			Summary: fmt.Sprintf("replace_text on open document: %q -> %q", oldT, newT),
		}
	default:
		text := fmt.Sprint(op.Params["text"])
		return &operations.Diff{
			Changes: []operations.Change{{Kind: "insert", After: text, Where: "open document"}},
			Summary: fmt.Sprintf("insert_text on open document: %d chars", len([]rune(text))),
		}
	}
}

// desktopOpReceipt builds the settlement receipt for a desktop mutation.
// revision is the session revision after the mutation and stays empty for
// unknown outcomes.
func desktopOpReceipt(txID, status, opName, docID, revision string, diff *operations.Diff, matches *int) opReceipt {
	return opReceipt{
		TransactionID: txID, Status: status, Operation: opName,
		DocumentID: docID, Revision: revision, Diff: diff, Validated: false, Matches: matches,
	}
}

// desktopRevision reports the session revision after a mutation: the named
// session when one was targeted, else the active session. Empty when unknown.
func (h *ToolHandlers) desktopRevision(sessionID string) string {
	if h.bridge == nil {
		return ""
	}
	for _, s := range h.bridge.Sessions() {
		if sessionID != "" {
			if s.ID == sessionID {
				return strconv.FormatUint(s.Revision, 10)
			}
		} else if s.Active {
			return strconv.FormatUint(s.Revision, 10)
		}
	}
	return ""
}

// applyDesktopOperation runs a writer.* operation on the active editor
// session. Sensitive and destructive mutations need approve:true before
// anything is sent. An unknown outcome settles the transaction as
// unknown_outcome with no after digest and never reports success.
func (h *ToolHandlers) applyDesktopOperation(ctx context.Context, op operations.Operation, spec operations.Spec, decision operations.Decision, approve bool, txID string) (*mcp.CallToolResult, error) {
	if h.bridge == nil || !h.bridge.IsConnected() {
		return textError("plugin not connected. Use open_document first and make sure ONLYOFFICE Desktop Editors is running with the document open."), nil
	}
	if spec.Risk == operations.RiskDestructive && !approve {
		return textError(fmt.Sprintf("operation %q requires approval (destructive); re-run with approve:true", op.Name)), nil
	}
	if decision.RequireApproval && !approve {
		preview := desktopPreviewDiff(op)
		msg := fmt.Sprintf("operation %q requires approval (%s); re-run with approve:true (or dry_run:true first)", op.Name, spec.Risk)
		if preview != nil && preview.Summary != "" {
			msg += ": " + preview.Summary
		}
		return textError(msg), nil
	}
	var method string
	var params map[string]any
	switch op.Name {
	case "writer.replace_text":
		oldT, err := strParam(op.Params, "old", true)
		if err != nil {
			return textError(err.Error()), nil
		}
		newT, err := strParam(op.Params, "new", true)
		if err != nil {
			return textError(err.Error()), nil
		}
		method, params = "replace_text", map[string]any{"old": oldT, "new": newT}
	case "writer.insert_text":
		text, err := strParam(op.Params, "text", true)
		if err != nil {
			return textError(err.Error()), nil
		}
		method, params = "insert_text", map[string]any{"text": text}
	default:
		return textError(fmt.Sprintf("operation %q is not a desktop operation", op.Name)), nil
	}
	tx := operations.NewTransaction(txID, op.Target.DocumentID, op.Name, "")
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var result any
	var err error
	if op.Target.SessionID != "" {
		result, err = h.bridge.SendTo(op.Target.SessionID, method, params, 10*time.Second)
	} else {
		result, err = h.bridge.SendCommand(callCtx, method, params, 10*time.Second)
	}
	if err != nil {
		var rpc *plugin.RPCError
		if errors.As(err, &rpc) && rpc.OutcomeUnknown {
			rc, mErr := tx.MarkUnknown(rpc.Error())
			if mErr != nil {
				return textError(mErr.Error()), nil
			}
			r := desktopOpReceipt(rc.TransactionID, string(rc.Status), op.Name, op.Target.DocumentID, "", &operations.Diff{
				Changes: []operations.Change{{Kind: "replace", Where: "open document"}},
				Summary: op.Name + ": outcome unknown, retry or inspect the open document",
			}, nil)
			r.UnknownOutcome = true
			return receiptResult(r)
		}
		return textError("plugin error: " + err.Error()), nil
	}
	var diff *operations.Diff
	var matches *int
	switch op.Name {
	case "writer.replace_text":
		n, mErr := plugin.ReplaceMatches(result)
		if mErr != nil {
			return textError("replace_text did not report a match count: " + mErr.Error()), nil
		}
		matches = &n
		diff = &operations.Diff{
			Changes: []operations.Change{{Kind: "replace", Before: fmt.Sprint(params["old"]), After: fmt.Sprint(params["new"]), Where: "open document"}},
			Summary: fmt.Sprintf("replace_text: %d matches", n),
		}
	default:
		diff = &operations.Diff{
			Changes: []operations.Change{{Kind: "insert", After: fmt.Sprint(params["text"]), Where: "open document"}},
			Summary: fmt.Sprintf("insert_text: %d chars", len([]rune(fmt.Sprint(params["text"])))),
		}
	}
	rc, err := tx.Commit("")
	if err != nil {
		return textError(err.Error()), nil
	}
	rev := h.desktopRevision(op.Target.SessionID)
	target := op.Target.SessionID
	if target == "" {
		target = op.Target.DocumentID
	}
	if target == "" {
		target = "desktop"
	}
	detail := map[string]any{"operation": op.Name, "before": "", "after": ""}
	if rev != "" {
		detail["revision"] = rev
	}
	auditLog("document_operation", target, "ok", detail)
	return receiptResult(desktopOpReceipt(rc.TransactionID, string(rc.Status), op.Name, op.Target.DocumentID, rev, diff, matches))
}
