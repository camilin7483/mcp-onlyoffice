package audit

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestAuditWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	l.Audit("delete", "/tmp/x", "ok", map[string]any{"note": "gone"})

	data, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	line := strings.TrimSpace(string(data))
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("parse audit line: %v", err)
	}
	if entry["action"] != "delete" || entry["target"] != "/tmp/x" || entry["outcome"] != "ok" {
		t.Errorf("unexpected entry: %v", entry)
	}
}

func TestAuditFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses ACLs instead of POSIX permission bits")
	}
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	info, err := os.Stat(l.Path())
	if err != nil {
		t.Fatalf("stat audit: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("audit perm = %o, want 600", perm)
	}
}

func TestNilLoggerNoop(t *testing.T) {
	var l *Logger
	l.Audit("x", "y", "z", nil) // must not panic
	if l.Path() != "" {
		t.Errorf("nil Path = %q, want empty", l.Path())
	}
	if err := l.Close(); err != nil {
		t.Errorf("nil Close: %v", err)
	}
}
