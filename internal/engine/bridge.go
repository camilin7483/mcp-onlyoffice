package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DefaultEngineTimeout bounds every request to the Python engine.
// Override per-bridge via the Timeout field.
const DefaultEngineTimeout = 120 * time.Second

// capabilityPattern is the allowlist: "module.method" in dot notation,
// plus the internal "__list__" and "__version__" commands.
// Anything else (paths, shell metachars, spaces) is rejected before IPC.
var capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// Request is sent to the Python Engine via stdin.
type Request struct {
	Capability string         `json:"capability"`
	Params     map[string]any `json:"params"`
}

// Response is received from the Python Engine via stdout.
type Response struct {
	Status    string `json:"status"`
	Data      any    `json:"data,omitempty"`
	Error     string `json:"error,omitempty"`
	Traceback string `json:"traceback,omitempty"`
	Warnings  any    `json:"warnings,omitempty"`
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
// via newline-delimited JSON over stdin/stdout. If the subprocess dies,
// the Bridge automatically restarts it (lazy restart on next Execute).
type Bridge struct {
	mu           sync.Mutex
	enginePath   string
	pythonCmd    string
	initialized  bool
	capabilities []CapabilityInfo
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       *bufio.Reader
	cancel       context.CancelFunc
	// Timeout bounds each Execute call. Zero means DefaultEngineTimeout.
	//
	// NOTE: on timeout the reader goroutine blocked in ReadString is NOT
	// killed — it stays parked until the next response line arrives on
	// stdout or the process dies (channel is buffered with cap 1, so at
	// most one zombie goroutine per timed-out call; it is reaped on the
	// next successful read or process restart). If this ever matters,
	// kill and restart the subprocess on timeout instead of reusing it.
	Timeout time.Duration
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

// Start initializes the Python Engine subprocess.
func (b *Bridge) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.startLocked(ctx)
}

// startLocked starts or restarts the Python subprocess. Caller must hold b.mu.
func (b *Bridge) startLocked(ctx context.Context) error {
	// If already initialized and the process is alive, do nothing.
	if b.initialized && b.processAlive() {
		return nil
	}

	// Clean up any previous state.
	b.stopLocked()

	runnerPath := b.runnerPath()
	if runnerPath == "" {
		return fmt.Errorf("empty engine path: set Bridge enginePath to the python_engine directory or runner.py")
	}

	engCtx, cancel := context.WithCancel(ctx)
	// No shell is involved: argv is [python, runner.py] executed directly.
	cmd := exec.CommandContext(engCtx, b.pythonCmd, runnerPath)

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
		return fmt.Errorf("start python engine (%s %s): %w (hint: install python3 and run: pip install -r python_engine/requirements.txt)", b.pythonCmd, runnerPath, err)
	}

	b.cmd = cmd
	b.stdin = stdin
	b.stdout = bufio.NewReaderSize(stdout, 1024*1024) // 1MB read buffer
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

	// Wait for the process to exit and mark as uninitialized.
	go func() {
		_ = cmd.Wait()
		b.mu.Lock()
		b.initialized = false
		b.mu.Unlock()
		slog.Debug("python engine process exited")
	}()

	// List capabilities to verify the engine is responsive.
	caps, err := b.listCapabilitiesLocked(engCtx)
	if err != nil {
		slog.Warn("list capabilities on startup", "error", err)
	} else {
		b.capabilities = caps
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
	return fmt.Errorf("rejected capability %q: must match \"module.method\" with lowercase letters, digits, underscores (e.g. \"excel.create_workbook\")", capability)
}

// processAlive checks whether the Python subprocess is still running.

// timeout returns the effective per-request timeout.
func (b *Bridge) timeout() time.Duration {
	if b.Timeout > 0 {
		return b.Timeout
	}
	return DefaultEngineTimeout
}
func (b *Bridge) processAlive() bool {
	if b.cmd == nil || b.cmd.Process == nil {
		return false
	}
	// On Unix, sending signal 0 checks process existence without killing it.
	if err := b.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}

// ensureAlive restarts the Python subprocess if it has died.
// Caller must hold b.mu.
func (b *Bridge) ensureAlive(ctx context.Context) error {
	if b.initialized && b.processAlive() {
		return nil
	}
	slog.Info("python engine dead, restarting...")
	return b.startLocked(ctx)
}

// Execute sends a capability request to the Python Engine and returns the response.
// If the Python subprocess has died, it is automatically restarted before retrying.
//
// The mutex covers the whole write+read cycle on purpose: the subprocess
// speaks lock-step JSON-lines over a single stdin/stdout pair, so two
// interleaved writes would scramble responses. For concurrent execution
// use EnginePool (one Bridge per process), not parallel Execute on one Bridge.
func (b *Bridge) Execute(ctx context.Context, capability string, params map[string]any) (*Response, error) {
	if err := validateCapability(capability); err != nil {
		return nil, err
	}
	if params == nil {
		params = map[string]any{}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Ensure the engine is alive (restart if needed).
	if err := b.ensureAlive(ctx); err != nil {
		return nil, fmt.Errorf("engine restart: %w", err)
	}

	resp, err := b.executeLocked(ctx, capability, params)
	if err != nil {
		// If the write or read failed, the process is likely dead.
		// Restart and retry once.
		slog.Warn("engine call failed, restarting and retrying",
			"capability", capability, "error", err)
		if restartErr := b.startLocked(ctx); restartErr != nil {
			return nil, fmt.Errorf("engine restart after failure (capability %q): %w", capability, restartErr)
		}
		resp, err = b.executeLocked(ctx, capability, params)
	}
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("engine returned no response for capability %q: restart the engine and retry", capability)
	}
	if resp.Status == "error" && resp.Error == "" {
		resp.Error = fmt.Sprintf("capability %q failed without detail; check engine stderr logs", capability)
	}
	return resp, err
}

// executeLocked performs a single request-response cycle.
// Never invokes a shell: the Python interpreter is exec'd directly with
// runner.py as argv (no sh -c), so capability/params cannot inject commands.
// Caller must hold b.mu.
func (b *Bridge) executeLocked(ctx context.Context, capability string, params map[string]any) (*Response, error) {
	if params == nil {
		params = map[string]any{}
	}
	req := Request{
		Capability: capability,
		Params:     params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request for capability %q: %w (hint: params must be JSON-serializable)", capability, err)
	}

	// Write the request line to stdin.
	if _, err := fmt.Fprintln(b.stdin, string(data)); err != nil {
		return nil, fmt.Errorf("write capability %q to engine: %w (hint: engine may have crashed; check stderr logs)", capability, err)
	}

	// Read the response line from stdout.
	type respResult struct {
		resp *Response
		err  error
	}

	ch := make(chan respResult, 1)
	go func() {
		line, readErr := b.stdout.ReadString('\n')
		if readErr != nil && line == "" {
			ch <- respResult{nil, fmt.Errorf("engine closed during capability %q: %w (hint: runner.py crashed; check stderr logs)", capability, readErr)}
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			ch <- respResult{nil, fmt.Errorf("empty response from engine for capability %q (hint: runner.py printed nothing; check stderr logs)", capability)}
			return
		}
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			ch <- respResult{nil, fmt.Errorf("parse response for capability %q: %w (line: %s)", capability, err, truncate(line, 200))}
			return
		}
		ch <- respResult{&resp, nil}
	}()

	timeout := b.timeout()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-ch:
		return result.resp, result.err
	case <-ctx.Done():
		return nil, fmt.Errorf("capability %q cancelled: %w", capability, ctx.Err())
	case <-timer.C:
		return nil, fmt.Errorf("engine timeout (%s) for capability %q (hint: retry with a larger Bridge.Timeout or split the workload)", timeout, capability)
	}
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

// Capabilities returns a copy of the cached list of capabilities.
func (b *Bridge) Capabilities() []CapabilityInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
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
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	if b.stdin != nil {
		b.stdin.Close()
	}
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
	b.initialized = false
	b.cmd = nil
	b.stdin = nil
	b.stdout = nil
	return nil
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
