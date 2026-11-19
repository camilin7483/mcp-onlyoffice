package plugin

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// BridgeVersion identifies this bridge to the plugin scanner and must match
// the plugin manifest version and the MCP server version.
const BridgeVersion = "2.0.0"

// AppName identifies this bridge in /ping and /pair responses so the plugin
// scanner only connects to the real mcp-onlyoffice process.
const AppName = "mcp-onlyoffice"

const (
	// maxMessageBytes caps a single plugin frame (read limit, 1 MiB).
	maxMessageBytes = 1 << 20
	// maxPending caps concurrent in-flight commands.
	maxPending = 32
	// scanPorts is how many ports from base are tried before a random fallback.
	scanPorts = 50
)

type Command struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

type Response struct {
	ID     string `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type pendingReq struct {
	ch    chan Response
	timer *time.Timer
}

type Bridge struct {
	mu        sync.Mutex
	conn      *websocket.Conn
	pending   map[string]*pendingReq
	connected bool
	port      int
	realPort  int
	srv       *http.Server
	token     string
	tokenPath string
}

func NewBridge(port int) *Bridge {
	b := &Bridge{
		pending: make(map[string]*pendingReq),
		port:    port,
		token:   newToken(),
	}
	b.tokenPath = defaultTokenPath()
	if b.tokenPath == "" {
		slog.Warn("bridge token kept in memory only: home dir unavailable")
		return b
	}
	if err := persistToken(b.tokenPath, b.token); err != nil {
		slog.Warn("bridge token kept in memory only", "error", err)
	}
	return b
}

func newToken() string {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(fmt.Sprintf("bridge token: %v", err))
	}
	return hex.EncodeToString(buf[:])
}

func defaultTokenPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "mcp-onlyoffice", "bridge-token")
}

func persistToken(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("mkdir token dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		return fmt.Errorf("write token: %w", err)
	}
	return nil
}

// Token returns the per-launch pairing secret the plugin must present.
func (b *Bridge) Token() string { return b.token }

// TokenPath returns where the token was persisted ("" if memory-only).
func (b *Bridge) TokenPath() string { return b.tokenPath }

// ListenAddr returns the actual address the bridge is listening on.
func (b *Bridge) ListenAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", b.realPort)
}

// ListenPort returns the actual port the bridge is listening on.
func (b *Bridge) ListenPort() int {
	return b.realPort
}

func (b *Bridge) Start() error {
	var listener net.Listener
	var err error
	for port := b.port; port < b.port+scanPorts; port++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		listener, err = net.Listen("tcp", addr)
		if err == nil {
			b.realPort = port
			break
		}
	}
	if listener == nil {
		// Last resort: random port. The plugin only scans the bounded range,
		// so this fallback is unreachable by discovery; say so explicitly.
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("bridge listen: %w", err)
		}
		b.realPort = listener.Addr().(*net.TCPAddr).Port
		slog.Warn("bridge on random port outside plugin scan range; plugin discovery will not find it",
			"port", b.realPort)
	}

	b.srv = &http.Server{
		Addr:    listener.Addr().String(),
		Handler: b.handler(),
	}

	slog.Info("WebSocket bridge", "addr", fmt.Sprintf("ws://127.0.0.1:%d/plugin", b.realPort))

	if err := b.srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (b *Bridge) Stop() {
	if b.srv != nil {
		b.srv.Close()
	}
}

func (b *Bridge) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", b.handlePing)
	mux.HandleFunc("/pair", b.handlePair)
	mux.HandleFunc("/plugin", b.handleWebSocket)
	return mux
}

// handlePing identifies this process to the plugin scanner. No auth and no
// secrets: it only lets the plugin verify it reached the right process.
func (b *Bridge) handlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{"app": AppName, "port": b.realPort, "version": BridgeVersion})
}

// handlePair hands the per-launch token to a local pairing client. Only
// reachable from loopback with no browser-page Origin (websites always send
// their own Origin and get 403).
func (b *Bridge) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isLoopbackHost(r.Host) {
		http.Error(w, "pairing allowed from loopback only", http.StatusForbidden)
		return
	}
	if !allowedPairOrigin(r.Header.Get("Origin")) {
		http.Error(w, "pairing refused for browser page origin", http.StatusForbidden)
		return
	}
	// The plugin runs inside a file:// / null-origin iframe; without this the
	// browser blocks reading the token even though pairing is allowed.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	writeJSON(w, map[string]any{"token": b.token, "port": b.realPort, "app": AppName})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// allowedWSOrigin reports whether a WebSocket handshake Origin is acceptable:
// absent (non-browser clients), "null" (sandboxed webviews), file://
// (local plugin pages), or loopback http(s) origins. Real websites are rejected.
func allowedWSOrigin(origin string) bool {
	if origin == "" || origin == "null" {
		return true
	}
	if origin == "file://" || strings.HasPrefix(origin, "file://") {
		return true
	}
	// Desktop Editors plugin webview (privileged local origin).
	if strings.HasPrefix(origin, "onlyoffice://plugin") {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return isLoopbackHost(u.Host)
}

// allowedPairOrigin is stricter than the WS rule: the plugin webview pairs
// with no Origin, "null", or file:// only.
func allowedPairOrigin(origin string) bool {
	return origin == "" || origin == "null" ||
		origin == "file://" || strings.HasPrefix(origin, "file://") ||
		strings.HasPrefix(origin, "onlyoffice://plugin")
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (b *Bridge) validToken(r *http.Request) bool {
	if t := r.URL.Query().Get("token"); t != "" {
		return subtle.ConstantTimeCompare([]byte(t), []byte(b.token)) == 1
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		t := strings.TrimPrefix(h, "Bearer ")
		return subtle.ConstantTimeCompare([]byte(t), []byte(b.token)) == 1
	}
	return false
}

func (b *Bridge) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !allowedWSOrigin(r.Header.Get("Origin")) {
		http.Error(w, "origin rejected", http.StatusForbidden)
		return
	}
	if !b.validToken(r) {
		http.Error(w, "invalid or missing token", http.StatusUnauthorized)
		return
	}
	// Origin was already validated above; skip the library's host-based check,
	// which rejects Desktop Editors' custom "onlyoffice://plugin" origin.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		slog.Warn("websocket accept", "error", err)
		return
	}
	c.SetReadLimit(maxMessageBytes)

	slog.Info("desktop editors connected")

	b.mu.Lock()
	if old := b.conn; old != nil {
		// Only one plugin client at a time: close the stale one explicitly
		// instead of silently stealing its connection.
		slog.Warn("closing previous plugin connection: second client connected")
		_ = old.Close(websocket.StatusPolicyViolation, "replaced by new plugin connection")
	}
	b.conn = c
	b.connected = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		// Only clear state if this handler still owns the connection; a newer
		// client may have replaced it while this one was draining.
		if b.conn == c {
			b.conn = nil
			b.connected = false
			for id, p := range b.pending {
				p.timer.Stop()
				// Wake the waiter with an error, never a zero Response that
				// SendCommand could mistake for success.
				select {
				case p.ch <- Response{ID: id, Error: "plugin disconnected"}:
				default:
				}
				close(p.ch)
				delete(b.pending, id)
			}
		}
		b.mu.Unlock()
	}()

	ctx := r.Context()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			slog.Warn("plugin read", "error", err)
			return
		}

		var resp Response
		if err := json.Unmarshal(data, &resp); err != nil {
			slog.Warn("plugin parse", "error", err)
			continue
		}

		b.deliverResponse(resp)
	}
}

func (b *Bridge) deliverResponse(resp Response) {
	b.mu.Lock()
	p, ok := b.pending[resp.ID]
	if ok {
		p.timer.Stop()
		delete(b.pending, resp.ID)
	}
	b.mu.Unlock()

	if ok {
		p.ch <- resp
		close(p.ch)
	}
}

func (b *Bridge) SendCommand(ctx context.Context, method string, params map[string]any, timeout time.Duration) (any, error) {
	id := fmt.Sprintf("mcp-%d", time.Now().UnixNano())

	cmd := Command{
		ID:     id,
		Method: method,
		Params: params,
	}

	ch := make(chan Response, 1)
	timer := time.NewTimer(timeout)

	b.mu.Lock()
	if !b.connected || b.conn == nil {
		b.mu.Unlock()
		timer.Stop()
		return nil, fmt.Errorf("plugin not connected")
	}
	if len(b.pending) >= maxPending {
		b.mu.Unlock()
		timer.Stop()
		return nil, fmt.Errorf("too many pending plugin requests")
	}
	b.pending[id] = &pendingReq{ch: ch, timer: timer}
	conn := b.conn
	b.mu.Unlock()

	defer func() {
		timer.Stop()
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("plugin disconnected")
		}
		if resp.Error != "" {
			return nil, fmt.Errorf("plugin error: %s", resp.Error)
		}
		return resp.Result, nil
	case <-timer.C:
		return nil, fmt.Errorf("timeout waiting for plugin response")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ReplaceMatches interprets a replace_text plugin result as a match count.
// The plugin reports {"matches": N}; anything else is an error, never a
// silent success.
func ReplaceMatches(result any) (int, error) {
	switch v := result.(type) {
	case map[string]any:
		n, ok := v["matches"]
		if !ok {
			return 0, fmt.Errorf("replace_text returned no match count")
		}
		return asInt(n)
	case float64:
		return asInt(v)
	case float32:
		return asInt(float64(v))
	case int:
		return asInt(v)
	case int64:
		return asInt(v)
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, fmt.Errorf("replace_text returned bad match count: %v", result)
		}
		return asInt(f)
	default:
		return 0, fmt.Errorf("replace_text returned no match count")
	}
}

func asInt(n any) (int, error) {
	var f float64
	switch v := n.(type) {
	case float64:
		f = v
	case float32:
		f = float64(v)
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case json.Number:
		ff, err := v.Float64()
		if err != nil {
			return 0, fmt.Errorf("replace_text returned bad match count")
		}
		f = ff
	default:
		return 0, fmt.Errorf("replace_text returned bad match count")
	}
	if f < 0 || f != float64(int(f)) {
		return 0, fmt.Errorf("replace_text returned bad match count")
	}
	return int(f), nil
}

func (b *Bridge) IsConnected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}
