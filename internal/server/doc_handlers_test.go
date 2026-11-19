package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// listenUnix binds a unix socket for non-regular-file tests. Callers skip
// when it fails (unsupported platform or restricted sandbox).
func listenUnix(t *testing.T, path string) (net.Listener, error) {
	t.Helper()
	return net.Listen("unix", path)
}

// TestPrepareWriteRefusesNonRegular (finding 5): prepareWrite must refuse a
// non-regular file instead of writing through it.
func TestPrepareWriteRefusesNonRegular(t *testing.T) {
	h, root := testFS(t)
	sock := filepath.Join(root, "sock")
	l, err := listenUnix(t, sock)
	if err != nil {
		t.Skipf("unix socket unavailable: %v", err)
	}
	defer l.Close()
	if _, bad := h.prepareWrite(sock, true); bad == nil {
		t.Fatal("expected non-regular refusal, got success")
	} else if txt := resultText(t, bad); !strings.Contains(txt, "non-regular") {
		t.Fatalf("expected non-regular refusal, got: %s", txt)
	}
}

// TestHandleAppendTextRefusesNonRegular (finding 5): append must refuse a
// non-regular file; opening a FIFO/socket with O_APPEND could block forever.
func TestHandleAppendTextRefusesNonRegular(t *testing.T) {
	h, root := testFS(t)
	sock := filepath.Join(root, "sock")
	l, err := listenUnix(t, sock)
	if err != nil {
		t.Skipf("unix socket unavailable: %v", err)
	}
	defer l.Close()
	r, _ := h.handleAppendText(context.Background(), map[string]any{
		"path": sock, "content": "x", "overwrite": true,
	})
	if !r.IsError {
		t.Fatal("expected non-regular refusal, got success")
	}
	if txt := resultText(t, r); !strings.Contains(txt, "non-regular") {
		t.Fatalf("expected non-regular refusal, got: %s", txt)
	}
}

// TestWritePresentationNoBackupWhenEngineDown (finding 4): with the engine
// unavailable the handler must fail before prepareWrite, leaving no .bak.
func TestWritePresentationNoBackupWhenEngineDown(t *testing.T) {
	h, root := testFS(t)
	h.pyBridge = failedPythonBridge(t) // pythonAvailable stays false
	p := filepath.Join(root, "deck.pptx")
	if err := os.WriteFile(p, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	r, _ := h.handleWritePresentation(context.Background(), map[string]any{
		"path": p, "overwrite": true,
		"slides": []any{map[string]any{"title": "T", "content": "C"}},
	})
	if !r.IsError {
		t.Fatal("expected engine-unavailable error, got success")
	}
	if txt := resultText(t, r); !strings.Contains(txt, "python engine unavailable") {
		t.Fatalf("error must name the missing backend, got: %s", txt)
	}
	matches, _ := filepath.Glob(p + ".bak-*")
	if len(matches) != 0 {
		t.Fatalf("engine-down attempt must not create a backup, got: %v", matches)
	}
	if data, _ := os.ReadFile(p); string(data) != "old" {
		t.Fatalf("original clobbered: %q", data)
	}
}
