package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/version"
)

// DefaultEngineTimeout bounds every request to the Python engine.
// Override per-bridge via the Timeout field.
//
// NOTE: a timeout means the request outcome is UNKNOWN — the Python side
// may or may not have executed the operation. The bridge SIGKILLs the
// subprocess on timeout, so callers must NOT blindly retry side-effecting
// capabilities; surface the OutcomeUnknownError to the tool layer instead.
const DefaultEngineTimeout = 120 * time.Second

// MaxResponseSize caps a single response line from the engine (64 MiB).
// Oversized lines are rejected and the engine is killed to resync the
// JSON-lines stream.
const MaxResponseSize = 64 << 20

// killWaitTimeout bounds how long we wait for a killed process to be reaped.
const killWaitTimeout = 5 * time.Second

// capabilityPattern is the allowlist: exactly "module.method" in dot notation,
// plus the internal "__list__" and "__version__" commands. Nested paths like
// "a.b.c", filesystem paths, shell metachars and spaces are rejected before
// IPC. This mirrors the Python engine, which accepts exactly two segments.
var capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)

// OutcomeUnknownError reports that a request may or may not have executed:
// the bridge killed the subprocess on timeout/cancellation instead of
// waiting for the answer. Do NOT automatically retry side-effecting
// operations; let the caller/operator decide.
type OutcomeUnknownError struct {
	Capability string
	Err        error
}

func (e *OutcomeUnknownError) Error() string {
	return fmt.Sprintf("capability %q outcome unknown (engine killed, it may or may not have executed): %v", e.Capability, e.Err)
}

func (e *OutcomeUnknownError) Unwrap() error { return e.Err }

// IsOutcomeUnknown reports whether err signals an unknown request outcome.
func IsOutcomeUnknown(err error) bool {
	var unknown *OutcomeUnknownError
	return errors.As(err, &unknown)
}

// Request is sent to the Python Engine via stdin.
// The id correlates responses; the runner echoes it back verbatim.
type Request struct {
	ID         uint64         `json:"id"`
	Capability string         `json:"capability"`
	Params     map[string]any `json:"params"`
}

// Response is received from the Python Engine via stdout.
type Response struct {
	ID        *uint64 `json:"id,omitempty"`
	Status    string  `json:"status,omitempty"`
	Data      any     `json:"data,omitempty"`
	Error     string  `json:"error,omitempty"`
	Traceback string  `json:"traceback,omitempty"`
	Warnings  any     `json:"warnings,omitempty"`
}

// CapabilityInfo describes a capability from the engine.
type CapabilityInfo struct {
	Name        string `json:"capability"`
	Description string `json:"description"`
}

// Bridge connects the Go MCP server to the Python Engine via subprocess.
// It follows the principle: MCP coordinates, Python Engine executes.
//
// The Bridge maintains a persistent Python subprocess and communicates
// via newline-delimited JSON over stdin/stdout. On timeout, cancellation
// after write, or protocol errors the subprocess is SIGKILLed and marked
// dead; the next Execute lazily restarts it with a fresh process and a
// fresh reader, so a late answer from the old process can never be matched
// to a later request. The same request is never re-sent automatically.
type Bridge struct {
	mu           sync.Mutex
	enginePath   string
	pythonCmd    string
	initialized  bool
	capabilities []CapabilityInfo
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       *bufio.Reader
	stdoutPipe   io.ReadCloser
	waitCh       chan struct{} // closed when the subprocess exits
	cancel       context.CancelFunc
	nextID       uint64 // monotonic request ids, never reset (survives restarts)
	// OutputDir confines Python-side file writes (empty keeps the default cwd).
	OutputDir string
	// Timeout bounds each Execute call. Zero means DefaultEngineTimeout.
	// A timeout kills the engine and returns OutcomeUnknownError.
	Timeout time.Duration
	// startErr records the last Start (or restart) outcome. A non-nil value
	// means the engine is unavailable: Execute refuses to run and
	// Capabilities stays nil until a later start succeeds.
	startErr error
}

// NewBridge creates a new Python Engine bridge.
func NewBridge(enginePath string) *Bridge {
	pythonCmd := "python3"
	if _, err := exec.LookPath("python3"); err != nil {
		pythonCmd = "python"
	}

	return &Bridge{
		enginePath: enginePath,
		pythonCmd:  pythonCmd,
	}
}

// runnerPath resolves the full path to runner.py.
func (b *Bridge) runnerPath() string {
	p := b.enginePath
	if !strings.HasSuffix(p, "runner.py") {
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		p += "runner.py"
	}
	return p
}

// Available reports whether the engine started successfully and has not
// failed since. A bridge whose Start failed (missing runner.py, missing
// interpreter, spawn error) reports false until a later start succeeds.
func (b *Bridge) Available() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.availableLocked()
}

// availableLocked reports start health. Caller must hold b.mu.
func (b *Bridge) availableLocked() bool {
	return b.startErr == nil && b.initialized
}

// Start initializes the Python Engine subprocess.
func (b *Bridge) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.startLocked(ctx)
}

// startLocked starts or restarts the Python subprocess and records the
// outcome for Available. Caller must hold b.mu.
func (b *Bridge) startLocked(ctx context.Context) error {
	err := b.doStartLocked(ctx)
	b.startErr = err
	return err
}

// doStartLocked starts or restarts the Python subprocess. Caller must hold b.mu.
func (b *Bridge) doStartLocked(ctx context.Context) error {
	return b.doStartWithProtocolCheckLocked(ctx, true)
}

// doStartWithProtocolCheckLocked spawns the engine and verifies it. When
// check is true it also compares the runner's __version__ against
// version.EngineProtocol, logging drift without failing. A failed version
// probe kills the fresh process (executeLocked always resyncs on transport
// errors), so it restarts once without the probe: startup still leaves a
// live backend. Caller must hold b.mu.
func (b *Bridge) doStartWithProtocolCheckLocked(ctx context.Context, check bool) error {
	// If already initialized and the process is alive, do nothing.
	if b.initialized && b.processAlive() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("not starting python engine (context done): %w", err)
	}

	// Clean up any previous state.
	b.killLocked()

	runnerPath := b.runnerPath()
	if runnerPath == "" {
		return fmt.Errorf("empty engine path: set Bridge enginePath to the python_engine directory or runner.py")
	}
	if _, err := os.Stat(runnerPath); err != nil {
		slog.Debug("python engine runner missing", "runner", runnerPath, "error", err)
		return errors.New("python engine unavailable (runner_missing)")
	}

	// The engine lifetime is managed solely by killLocked/Stop, never by a
	// caller request context: deriving from ctx would kill the shared
	// process when an unrelated request context is cancelled.
	engCtx, cancel := context.WithCancel(context.Background())
	// No shell is involved: argv is [python, runner.py] executed directly.
	cmd := exec.CommandContext(engCtx, b.pythonCmd, runnerPath)
	if b.OutputDir != "" {
		cmd.Env = append(os.Environ(), "MCP_ONLYOFFICE_ENGINE_OUTPUT_DIR="+b.OutputDir)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		slog.Debug("python engine start failed", "python", b.pythonCmd, "runner", runnerPath, "error", err)
		return errors.New("python engine unavailable (start_failed)")
	}

	waitCh := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waitCh)
	}()

	b.cmd = cmd
	b.stdin = stdin
	b.stdoutPipe = stdout
	b.stdout = bufio.NewReaderSize(stdout, 1024*1024) // 1MB read buffer
	b.waitCh = waitCh
	b.cancel = cancel

	// Log stderr line by line in the background (non-blocking).
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" {
				slog.Debug("python engine stderr", "line", line)
			}
		}
	}()

	b.initialized = true

	// List capabilities to verify the engine is responsive.
	caps, err := b.listCapabilitiesLocked(engCtx)
	if err != nil {
		slog.Warn("list capabilities on startup", "error", err)
	} else {
		b.capabilities = caps
	}

	// EngineProtocol drift check: the runner reports its NDJSON protocol
	// version via __version__. A mismatch is logged, never fatal, so an
	// older or newer runner keeps working while the drift is visible.
	if check {
		if vresp, verr := b.executeLocked(engCtx, "__version__", map[string]any{}); verr != nil {
			slog.Debug("engine __version__ query failed, restarting without the probe", "error", verr)
			return b.doStartWithProtocolCheckLocked(ctx, false)
		} else if vresp != nil && vresp.Status == "ok" {
			if m, ok := vresp.Data.(map[string]any); ok {
				if v, _ := m["version"].(string); v != "" && v != version.EngineProtocol {
					slog.Warn("python engine protocol drift",
						"engine", v, "want", version.EngineProtocol)
				}
			}
		}
	}

	slog.Info("python engine bridge started",
		"python", b.pythonCmd,
		"runner", runnerPath,
		"capabilities", len(b.capabilities),
	)

	return nil
}

// validateCapability enforces the allowlist and rejects shell metachars.
// Internal commands "__list__" and "__version__" are always allowed.
func validateCapability(capability string) error {
	if capability == "__list__" || capability == "__version__" {
		return nil
	}
	if capability == "" {
		return fmt.Errorf("empty capability: must be \"module.method\" (e.g. \"excel.create_workbook\")")
	}
	if capabilityPattern.MatchString(capability) {
		return nil
	}
	return fmt.Errorf("rejected capability %q: must be exactly \"module.method\" with lowercase letters, digits, underscores (e.g. \"excel.create_workbook\")", capability)
}

// timeout returns the effective per-request timeout.
func (b *Bridge) timeout() time.Duration {
	if b.Timeout > 0 {
		return b.Timeout
	}
	return DefaultEngineTimeout
}

// processAlive checks whether the Python subprocess is still running.
// Caller must hold b.mu.
func (b *Bridge) processAlive() bool {
	if !b.initialized || b.cmd == nil || b.cmd.Process == nil {
		return false
	}
	select {
	case <-b.waitCh:
		return false
	default:
	}
	// On Unix, sending signal 0 checks process existence without killing it.
	if err := b.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}

// ensureAlive restarts the Python subprocess if it has died.
// It never spawns a new process when ctx is already done.
// Caller must hold b.mu.
func (b *Bridge) ensureAlive(ctx context.Context) error {
	if b.initialized && b.processAlive() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("engine not running and context done (not restarting): %w", err)
	}
	slog.Info("python engine dead, restarting...")
	return b.startLocked(ctx)
}

// Execute sends a capability request to the Python Engine and returns the response.
//
// The mutex covers the whole write+read cycle on purpose: the subprocess
// speaks lock-step JSON-lines over a single stdin/stdout pair, so two
// interleaved writes would scramble responses. For concurrent execution
// use EnginePool (one Bridge per process), not parallel Execute on one Bridge.
//
// The request is sent at most once: on timeout, cancellation, or transport
// errors the engine is killed and the error is returned WITHOUT retrying,
// because a retry could duplicate side effects. The next Execute lazily
// restarts with a fresh process. A timeout/cancellation error is an
// *OutcomeUnknownError: the operation may or may not have executed.
func (b *Bridge) Execute(ctx context.Context, capability string, params map[string]any) (*Response, error) {
	if err := validateCapability(capability); err != nil {
		return nil, err
	}
	if params == nil {
		params = map[string]any{}
	}
	// Never spawn or restart the engine for an already-done context.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("capability %q not sent (context done before write): %w", capability, err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Re-check after acquiring the lock: the context may have been
	// cancelled while waiting, and the write below must not happen then.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("capability %q not sent (context done before write): %w", capability, err)
	}

	// Ensure the engine is alive (restart if needed). A previous failed
	// start leaves the bridge unavailable: refuse instead of retrying a
	// known-bad backend (the stored start error is already a stable,
	// path-free "python engine unavailable (reason)" value). A fresh
	// bridge (never started) still lazily starts here via ensureAlive.
	if b.startErr != nil {
		return nil, b.startErr
	}
	if err := b.ensureAlive(ctx); err != nil {
		return nil, err
	}

	resp, err := b.executeLocked(ctx, capability, params)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("engine returned no response for capability %q: restart the engine and retry", capability)
	}
	if resp.Status == "error" && resp.Error == "" {
		resp.Error = fmt.Sprintf("capability %q failed without detail; check engine stderr logs", capability)
	}
	return resp, nil
}

type readResult struct {
	line string
	err  error
}

// executeLocked performs a single request-response cycle, exactly once.
// Never invokes a shell: the Python interpreter is exec'd directly with
// runner.py as argv (no sh -c), so capability/params cannot inject commands.
// Caller must hold b.mu.
func (b *Bridge) executeLocked(ctx context.Context, capability string, params map[string]any) (*Response, error) {
	if params == nil {
		params = map[string]any{}
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("capability %q not sent (context done before write): %w", capability, err)
	}
	b.nextID++
	id := b.nextID
	req := Request{
		ID:         id,
		Capability: capability,
		Params:     params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request for capability %q: %w (hint: params must be JSON-serializable)", capability, err)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("capability %q not sent (context done before write): %w", capability, err)
	}

	timeout := b.timeout()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Cancellable write: a stuck or dead engine must not park us forever
	// holding the mutex. The goroutine only touches locals + stdin.
	stdin := b.stdin
	writeCh := make(chan error, 1)
	go func() {
		if stdin == nil {
			writeCh <- errors.New("engine stdin is closed")
			return
		}
		_, werr := fmt.Fprintln(stdin, string(data))
		writeCh <- werr
	}()

	select {
	case werr := <-writeCh:
		if werr != nil {
			b.killLocked()
			return nil, fmt.Errorf("write capability %q to engine: %w (engine killed; it restarts on the next call)", capability, werr)
		}
	case <-ctx.Done():
		b.killLocked()
		return nil, &OutcomeUnknownError{Capability: capability, Err: fmt.Errorf("cancelled during write: %w", ctx.Err())}
	case <-timer.C:
		b.killLocked()
		return nil, &OutcomeUnknownError{Capability: capability, Err: fmt.Errorf("engine timeout (%s) during write", timeout)}
	}

	// Capped, id-correlated read. The reader goroutine only touches its
	// local copy of the current fresh reader; on timeout/cancel the engine
	// is killed and the reader discarded, so a late answer can never leak
	// into a subsequent request.
	reader := b.stdout
	if reader == nil {
		b.killLocked()
		return nil, fmt.Errorf("engine reader gone for capability %q (engine killed; it restarts on the next call)", capability)
	}
	readCh := make(chan readResult, 1)
	go func() {
		line, rerr := readLineCapped(reader, MaxResponseSize)
		readCh <- readResult{line: line, err: rerr}
	}()

	select {
	case r := <-readCh:
		if r.err != nil {
			b.killLocked()
			return nil, fmt.Errorf("read capability %q: %w (engine killed; it restarts on the next call)", capability, r.err)
		}
		line := strings.TrimSpace(r.line)
		if line == "" {
			b.killLocked()
			return nil, fmt.Errorf("empty response from engine for capability %q (engine killed; hint: runner.py printed nothing; check stderr logs)", capability)
		}
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			b.killLocked()
			return nil, fmt.Errorf("parse response for capability %q: %w (engine killed; line: %s)", capability, err, truncate(line, 200))
		}
		if resp.ID == nil || *resp.ID != id {
			b.killLocked()
			return nil, fmt.Errorf("response id mismatch for capability %q: got %s, want %d (engine killed; stale answer rejected)", capability, formatResponseID(resp.ID), id)
		}
		return &resp, nil
	case <-ctx.Done():
		// Cancelled after the write: the operation may have executed.
		b.killLocked()
		return nil, &OutcomeUnknownError{Capability: capability, Err: fmt.Errorf("cancelled while waiting for response: %w", ctx.Err())}
	case <-timer.C:
		b.killLocked()
		return nil, &OutcomeUnknownError{Capability: capability, Err: fmt.Errorf("engine timeout (%s) for capability %q (hint: retry with a larger Bridge.Timeout or split the workload)", timeout, capability)}
	}
}

// readLineCapped reads one newline-terminated line, rejecting lines over
// maxBytes with a clear error before they can grow without bound. On
// oversize the stream position is left mid-line; the caller must kill the
// engine to resync (a fresh reader starts on the next Execute).
func readLineCapped(r *bufio.Reader, maxBytes int) (string, error) {
	var sb strings.Builder
	total := 0
	for {
		frag, err := r.ReadString('\n')
		total += len(frag)
		if total > maxBytes+1 {
			return "", fmt.Errorf("engine response exceeds %d bytes, rejecting oversized line", maxBytes)
		}
		sb.WriteString(frag)
		if err != nil {
			if err == io.EOF && sb.Len() > 0 {
				return sb.String(), nil
			}
			return "", err
		}
		if strings.HasSuffix(frag, "\n") {
			return sb.String(), nil
		}
	}
}

func formatResponseID(id *uint64) string {
	if id == nil {
		return "<missing>"
	}
	return fmt.Sprintf("%d", *id)
}

// listCapabilities requests the list of all available capabilities.
// Caller must hold b.mu.
func (b *Bridge) listCapabilitiesLocked(ctx context.Context) ([]CapabilityInfo, error) {
	resp, err := b.executeLocked(ctx, "__list__", map[string]any{})
	if err != nil {
		return nil, err
	}
	if resp.Status != "ok" {
		return nil, fmt.Errorf("list failed: %s", resp.Error)
	}

	dataList, ok := resp.Data.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected data format")
	}

	var caps []CapabilityInfo
	for _, item := range dataList {
		if m, ok := item.(map[string]any); ok {
			caps = append(caps, CapabilityInfo{
				Name:        toString(m["capability"]),
				Description: toString(m["description"]),
			})
		}
	}
	return caps, nil
}

// Capabilities returns a copy of the cached list of capabilities, or nil
// when the engine is unavailable (never started successfully).
func (b *Bridge) Capabilities() []CapabilityInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.availableLocked() {
		return nil
	}
	out := make([]CapabilityInfo, len(b.capabilities))
	copy(out, b.capabilities)
	return out
}

// Stop cleanly shuts down the Python Engine.
func (b *Bridge) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stopLocked()
}

// stopLocked shuts down the subprocess. Caller must hold b.mu.
func (b *Bridge) stopLocked() error {
	b.killLocked()
	return nil
}

// killLocked SIGKILLs the subprocess, closes the pipes (unblocking any
// parked reader goroutine, which then exits via its buffered channel),
// waits for the process to be reaped, and marks the bridge dead. The next
// Execute restarts with a fresh process and a fresh reader, so answers
// from the old process can never be matched to new requests.
// Caller must hold b.mu.
func (b *Bridge) killLocked() {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	if b.stdin != nil {
		_ = b.stdin.Close()
		b.stdin = nil
	}
	if b.stdoutPipe != nil {
		_ = b.stdoutPipe.Close()
		b.stdoutPipe = nil
	}
	b.stdout = nil
	wc := b.waitCh
	b.waitCh = nil
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
	b.cmd = nil
	b.initialized = false
	if wc != nil {
		select {
		case <-wc:
		case <-time.After(killWaitTimeout):
			slog.Warn("python engine kill timed out waiting for exit")
		}
	}
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// BatchExecute runs a sequence of capabilities, respecting dependencies.
func (b *Bridge) BatchExecute(ctx context.Context, steps []struct {
	StepID     string
	Capability string
	Params     map[string]any
	DependsOn  []string
}) ([]struct {
	StepID string
	Result *Response
	Error  error
}, error) {
	var results []struct {
		StepID string
		Result *Response
		Error  error
	}

	completed := make(map[string]*Response)

	for _, step := range steps {
		allMet := true
		for _, dep := range step.DependsOn {
			if _, ok := completed[dep]; !ok {
				allMet = false
				break
			}
		}

		if !allMet {
			results = append(results, struct {
				StepID string
				Result *Response
				Error  error
			}{StepID: step.StepID, Error: fmt.Errorf("dependency not met")})
			continue
		}

		params := make(map[string]any)
		for k, v := range step.Params {
			params[k] = v
		}
		for _, dep := range step.DependsOn {
			if r, ok := completed[dep]; ok {
				params[dep+"_result"] = r.Data
			}
		}

		resp, err := b.Execute(ctx, step.Capability, params)
		result := struct {
			StepID string
			Result *Response
			Error  error
		}{StepID: step.StepID, Result: resp, Error: err}

		results = append(results, result)
		if err == nil && resp != nil && resp.Status == "ok" {
			completed[step.StepID] = resp
		}
	}

	return results, nil
}

// EnginePool manages multiple Python Engine instances for concurrent execution.
type EnginePool struct {
	bridges []*Bridge
	next    int
	mu      sync.Mutex
}

// NewEnginePool creates a pool of Python Engine bridges.
func NewEnginePool(enginePath string, count int) (*EnginePool, error) {
	if count <= 0 {
		count = 1
	}
	pool := &EnginePool{
		bridges: make([]*Bridge, count),
	}
	for i := 0; i < count; i++ {
		pool.bridges[i] = NewBridge(enginePath)
	}
	return pool, nil
}

// Start initializes all bridges in the pool.
func (p *EnginePool) Start(ctx context.Context) error {
	for _, b := range p.bridges {
		if err := b.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Get returns the next available bridge (round-robin).
func (p *EnginePool) Get() *Bridge {
	p.mu.Lock()
	defer p.mu.Unlock()
	b := p.bridges[p.next]
	p.next = (p.next + 1) % len(p.bridges)
	return b
}

// Stop shuts down all bridges.
func (p *EnginePool) Stop() {
	for _, b := range p.bridges {
		b.Stop()
	}
}
