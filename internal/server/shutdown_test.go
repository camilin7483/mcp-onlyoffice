package server

import (
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/config"
)

// TestRunReturnsOnStdinEOF re-executes this test binary as a child that
// boots the real server and serves stdio. The parent closes stdin at once:
// the child must exit 0 by itself within 10 seconds (no kill needed).
func TestRunReturnsOnStdinEOF(t *testing.T) {
	if os.Getenv("GO_TEST_SHUTDOWN_CHILD") == "1" {
		runShutdownChild(t)
		return
	}

	home := t.TempDir()
	storage := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestRunReturnsOnStdinEOF")
	cmd.Env = append(os.Environ(),
		"GO_TEST_SHUTDOWN_CHILD=1",
		"HOME="+home,
		"MCP_ONLYOFFICE_STORAGE_PATH="+storage,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	// Client closes stdin immediately (MCP client disconnect).
	if err := stdin.Close(); err != nil {
		t.Fatalf("close stdin: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("child did not exit within 10s of stdin EOF")
	}
}

func runShutdownChild(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.ApplyEnv()
	cfg.PluginPort = 0
	cfg.DesktopPath = ""
	app, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := app.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
