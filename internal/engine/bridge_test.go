package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const echoScript = `#!/usr/bin/env python3
import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except Exception as e:
        print(json.dumps({"status": "error", "error": f"bad json: {e}"}), flush=True)
        continue
    rid = req.get("id")
    cap = req.get("capability", "")
    if cap == "__list__":
        print(json.dumps({"id": rid, "status": "ok", "data": []}), flush=True)
    elif cap == "__version__":
        print(json.dumps({"id": rid, "status": "ok", "data": {}}), flush=True)
    else:
        print(json.dumps({"id": rid, "status": "ok", "data": {"echo": cap}}), flush=True)
`

const sleepScript = `#!/usr/bin/env python3
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
    if (req.get("params") or {}).get("sleep"):
        time.sleep(5)
    print(json.dumps({"id": rid, "status": "ok", "data": {"cap": cap}}), flush=True)
`

const mismatchScript = `#!/usr/bin/env python3
import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    rid = req.get("id")
    if req.get("capability") in ("__list__", "__version__"):
        print(json.dumps({"id": rid, "status": "ok", "data": []}), flush=True)
        continue
    bad = rid + 1 if isinstance(rid, int) else 999999
    print(json.dumps({"id": bad, "status": "ok", "data": {}}), flush=True)
`

const hugeScript = `#!/usr/bin/env python3
import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    if req.get("capability") == "__list__":
        print(json.dumps({"id": req.get("id"), "status": "ok", "data": []}), flush=True)
        continue
    print('{"id": %s, "status": "ok", "data": "%s"}' % (json.dumps(req.get("id")), "A" * (65 * 1024 * 1024)), flush=True)
`

func writeFakeRunner(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "runner.py"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newTestBridge(dir string, timeout time.Duration) *Bridge {
	b := NewBridge(dir)
	b.Timeout = timeout
	return b
}

func TestExecuteEcho(t *testing.T) {
	b := newTestBridge(writeFakeRunner(t, echoScript), 10*time.Second)
	defer b.Stop()
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	resp, err := b.Execute(ctx, "test.echo", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q", resp.Status)
	}
	if resp.ID == nil {
		t.Fatal("response id missing")
	}
	m, ok := resp.Data.(map[string]any)
	if !ok || m["echo"] != "test.echo" {
		t.Fatalf("unexpected data: %#v", resp.Data)
	}
}

func TestCapabilityRejectsNested(t *testing.T) {
	b := newTestBridge(writeFakeRunner(t, echoScript), 10*time.Second)
	defer b.Stop()
	if _, err := b.Execute(context.Background(), "a.b.c", nil); err == nil ||
		!strings.Contains(err.Error(), "module.method") {
		t.Fatalf("expected module.method rejection, got: %v", err)
	}
	if b.cmd != nil {
		t.Fatal("rejected capability must not spawn a process")
	}
}

func TestTimeoutKillsProcess(t *testing.T) {
	b := newTestBridge(writeFakeRunner(t, sleepScript), 200*time.Millisecond)
	defer b.Stop()
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid1 := b.cmd.Process.Pid

	_, err := b.Execute(ctx, "test.slow", map[string]any{"sleep": true})
	if err == nil || !IsOutcomeUnknown(err) {
		t.Fatalf("expected OutcomeUnknownError, got: %v", err)
	}
	if b.cmd != nil {
		t.Fatal("engine must be dead after timeout")
	}

	// Next call restarts with a fresh process; the late answer from the
	// killed process must not leak into it.
	resp, err := b.Execute(ctx, "test.fast", map[string]any{})
	if err != nil {
		t.Fatalf("fast call after timeout: %v", err)
	}
	pid2 := b.cmd.Process.Pid
	if pid2 == pid1 {
		t.Fatalf("expected a new PID after kill, got same %d", pid1)
	}
	m, ok := resp.Data.(map[string]any)
	if !ok || m["cap"] != "test.fast" {
		t.Fatalf("stale/wrong answer delivered: %#v", resp.Data)
	}
}

func TestMismatchedIDRejected(t *testing.T) {
	b := newTestBridge(writeFakeRunner(t, mismatchScript), 10*time.Second)
	defer b.Stop()
	// Start handshakes via __list__, which also gets a bad id.
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	_, err := b.Execute(context.Background(), "test.echo", nil)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "mismatch") {
		t.Fatalf("expected id mismatch rejection, got: %v", err)
	}
}

func TestHugeLineRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 65MiB line test in short mode")
	}
	b := newTestBridge(writeFakeRunner(t, hugeScript), 30*time.Second)
	defer b.Stop()
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	_, err := b.Execute(context.Background(), "test.big", nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized rejection, got: %v", err)
	}
	if b.cmd != nil {
		t.Fatal("engine must be dead after oversized line (stream resync)")
	}
}

func TestCancelBeforeWriteNoSpawn(t *testing.T) {
	// Fresh bridge, cancelled ctx: must not spawn anything.
	b := newTestBridge(writeFakeRunner(t, echoScript), 10*time.Second)
	defer b.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Execute(ctx, "test.echo", nil); err == nil {
		t.Fatal("expected cancellation error")
	}
	if b.cmd != nil {
		t.Fatal("cancelled call must not spawn a process")
	}

	// Started bridge, cancelled ctx: must not kill the live process.
	b2 := newTestBridge(writeFakeRunner(t, echoScript), 10*time.Second)
	defer b2.Stop()
	if err := b2.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := b2.cmd.Process.Pid
	if _, err := b2.Execute(ctx, "test.echo", nil); err == nil {
		t.Fatal("expected cancellation error")
	}
	if b2.cmd == nil || b2.cmd.Process.Pid != pid {
		t.Fatal("live process must survive a pre-write cancellation")
	}
	if !b2.processAlive() {
		t.Fatal("engine should still be alive")
	}
}

func TestConcurrentExecute(t *testing.T) {
	b := newTestBridge(writeFakeRunner(t, echoScript), 10*time.Second)
	defer b.Stop()
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				resp, err := b.Execute(ctx, "test.echo", nil)
				if err != nil {
					errs <- err
					return
				}
				if resp.Status != "ok" || resp.ID == nil {
					errs <- errBadResponse
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent execute: %v", err)
	}
}

var errBadResponse = errTestHelper("bad response")

type errTestHelper string

func (e errTestHelper) Error() string { return string(e) }

func TestStartFailureUnavailable(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "missing-dir")
	b := NewBridge(missingDir)
	defer b.Stop()
	if b.Available() {
		t.Fatal("fresh bridge must not report Available()")
	}
	startErr := b.Start(context.Background())
	if startErr == nil {
		t.Fatal("expected Start to fail against a nonexistent runner")
	}
	if !strings.Contains(startErr.Error(), "python engine unavailable") ||
		!strings.Contains(startErr.Error(), "runner_missing") {
		t.Fatalf("expected stable reason-coded error, got: %v", startErr)
	}
	if strings.Contains(startErr.Error(), missingDir) {
		t.Fatalf("start error must not leak paths, got: %v", startErr)
	}
	if b.Available() {
		t.Fatal("failed bridge must report Available() == false")
	}
	if caps := b.Capabilities(); caps != nil {
		t.Fatalf("failed bridge Capabilities() = %v, want nil", caps)
	}
	_, err := b.Execute(context.Background(), "test.echo", nil)
	if err == nil || !strings.Contains(err.Error(), "python engine unavailable") {
		t.Fatalf("expected unavailable error, got: %v", err)
	}
	if strings.Contains(err.Error(), missingDir) {
		t.Fatalf("execute error must not leak paths, got: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop must stay safe after failed start: %v", err)
	}
}
