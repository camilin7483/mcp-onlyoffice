package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger appends JSONL audit entries to <storageDir>/audit.jsonl.
// The file is created with 0600 permissions; writes are mutex-guarded.
// A nil *Logger is a valid no-op receiver.
type Logger struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

// New opens (creating with 0600) the audit log inside storageDir.
func New(storageDir string) (*Logger, error) {
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	p := filepath.Join(storageDir, "audit.jsonl")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return &Logger{f: f, path: p}, nil
}

// NewSilent is New but returns nil instead of an error, for minimal wiring.
func NewSilent(storageDir string) *Logger {
	l, err := New(storageDir)
	if err != nil {
		return nil
	}
	return l
}

// Audit records one JSONL line: action, target, outcome and optional detail.
func (l *Logger) Audit(action, target, outcome string, detail map[string]any) {
	if l == nil {
		return
	}
	entry := map[string]any{
		"time":    time.Now().UTC().Format(time.RFC3339),
		"action":  action,
		"target":  target,
		"outcome": outcome,
	}
	if len(detail) > 0 {
		entry["detail"] = detail
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		fmt.Fprintln(l.f, string(line))
	}
}

// Path returns the audit file path ("" for a nil logger).
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Close flushes and closes the audit file.
func (l *Logger) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.f.Close()
	l.f = nil
	return err
}
