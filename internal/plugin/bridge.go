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
	"sync/atomic"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/version"
	"github.com/coder/websocket"
)

// BridgeVersion identifies this bridge to the plugin scanner and must match
// the plugin manifest version and the MCP server version.
// It is an alias of version.Version, the single source of truth.
var BridgeVersion = version.Version

// AppName identifies this bridge in /ping and /pair responses so the plugin
// scanner only connects to the real mcp-onlyoffice process.
const AppName = "mcp-onlyoffice"

const (
	// maxMessageBytes caps a single plugin frame (read limit, 1 MiB).
	maxMessageBytes = 1 << 20
	// helloWindow is how long a new connection has to send hello.
	helloWindow = 5 * time.Second
	// defaultRequestTimeout applies when the caller passes no timeout.
	defaultRequestTimeout = 10 * time.Second
	// scanPorts is how many ports from base are tried before a random fallback.
	scanPorts = 50
)

// Wire frames. The plugin never receives JavaScript: the server only sends
// closed-switch method names, and the plugin only replies with results.
type helloFrame struct {
	Type             string   `json:"type"`
	Protocol         int      `json:"protocol"`
	PluginInstanceID string   `json:"plugin_instance_id"`
	EditorType       string   `json:"editor_type"`
	Capabilities     []string `json:"capabilities"`
	ClientVersion    string   `json:"client_version"`
}

type helloAckFrame struct {
	Type          string         `json:"type"`
	Protocol      int            `json:"protocol"`
	SessionID     string         `json:"session_id"`
	ServerVersion string         `json:"server_version"`
	Limits        map[string]int `json:"limits"`
}

type commandFrame struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Method     string         `json:"method"`
	Params     map[string]any `json:"params,omitempty"`
	DeadlineMs int64          `json:"deadline_ms"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type responseFrame struct {
	Type   string         `json:"type"`
	ID     string         `json:"id"`
	OK     bool           `json:"ok"`
	Result any            `json:"result,omitempty"`
	Error  *responseError `json:"error,omitempty"`
}

type pendingResult struct {
	resp responseFrame
	fail *RPCError
}

type pendingReq struct {
	ch chan pendingResult
}

// sessionEntry is a Session plus its live connection state. Writes are
// serialised with writeMu because websocket writes are not concurrent-safe.
type sessionEntry struct {
	Session
	conn      *websocket.Conn
	writeMu   sync.Mutex
	pending   map[string]*pendingReq
	completed map[string]time.Time
}

type Bridge struct {
	reg *Registry
	// drop counters: unsolicited ids vs repeats of completed ids.
	unsolicited atomic.Uint64
	duplicate   atomic.Uint64

	port     int
	realPort int
	// mu guards realPort: Start writes it while ListenAddr/ListenPort and
	// the /ping and /pair handlers read it from other goroutines. It is
	// never held while blocking in Serve.
	mu        sync.RWMutex
	srv       *http.Server
	token     string
	tokenPath string
	// Pairing challenges: single-use nonces bound to the requesting IP.
	// nonceMu guards nonces and nonceOrder; never held while blocking.
	nonceMu    sync.Mutex
	nonces     map[string]nonceEntry
	nonceOrder []string
	nonceNow   func() time.Time
	nonceTTL   time.Duration
}

// nonceEntry is one outstanding pairing challenge.
type nonceEntry struct {
	ip      string
	expires time.Time
}

const (
	// challengeTTLSeconds is how long a /pair/challenge nonce stays valid.
	challengeTTLSeconds = 60
	// maxNonces caps outstanding challenges; the oldest is evicted when full.
	maxNonces = 32
)

func NewBridge(port int) *Bridge {
	b := &Bridge{
		reg:      NewRegistry(),
		port:     port,
		token:    newToken(),
		nonces:   make(map[string]nonceEntry),
		nonceNow: time.Now,
		nonceTTL: challengeTTLSeconds * time.Second,
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

// newWireID returns 32 hex chars from crypto/rand. Request ids are never
// timestamps, so concurrent sessions cannot collide on them.
func newWireID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(fmt.Sprintf("wire id: %v", err))
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

// setRealPort records the bound port. Caller must not hold b.mu.
func (b *Bridge) setRealPort(port int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.realPort = port
}

// ListenAddr returns the actual address the bridge is listening on.
func (b *Bridge) ListenAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", b.ListenPort())
}

// ListenPort returns the actual port the bridge is listening on.
func (b *Bridge) ListenPort() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.realPort
}

func (b *Bridge) Start() error {
	var listener net.Listener
	var err error
	chosen := 0
	for port := b.port; port < b.port+scanPorts; port++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		listener, err = net.Listen("tcp", addr)
		if err == nil {
			chosen = port
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
		chosen = listener.Addr().(*net.TCPAddr).Port
		slog.Warn("bridge on random port outside plugin scan range; plugin discovery will not find it",
			"port", chosen)
	}
	b.setRealPort(chosen)

	b.srv = &http.Server{
		Addr:    listener.Addr().String(),
		Handler: b.handler(),
	}

	slog.Info("WebSocket bridge", "addr", fmt.Sprintf("ws://127.0.0.1:%d/plugin", b.ListenPort()))

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
	mux.HandleFunc("/pair/challenge", b.handlePairChallenge)
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
	writeJSON(w, map[string]any{"app": AppName, "port": b.ListenPort(), "version": BridgeVersion, "protocol": version.BridgeProtocol})
}

// handlePairChallenge issues a single-use nonce bound to the caller's IP.
// Same loopback and origin rules as /pair; the plugin webview needs the
// wildcard CORS header to read the response from a file:// page.
func (b *Bridge) handlePairChallenge(w http.ResponseWriter, r *http.Request) {
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
	nonce, expiresIn := b.issueChallenge(remoteIP(r))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	writeJSON(w, map[string]any{"nonce": nonce, "expires_in": expiresIn})
}

// issueChallenge creates a nonce bound to ip, evicting the oldest when full.
func (b *Bridge) issueChallenge(ip string) (string, int) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(fmt.Sprintf("pair nonce: %v", err))
	}
	nonce := hex.EncodeToString(buf[:])
	ttl := b.nonceTTL
	if ttl <= 0 {
		ttl = challengeTTLSeconds * time.Second
	}
	now := b.nonceNow
	if now == nil {
		now = time.Now
	}
	b.nonceMu.Lock()
	defer b.nonceMu.Unlock()
	// Reclaim expired entries first so a burst of stale nonces cannot
	// evict a live one.
	nowT := now()
	kept := b.nonceOrder[:0]
	for _, n := range b.nonceOrder {
		if e, ok := b.nonces[n]; ok && nowT.Before(e.expires) {
			kept = append(kept, n)
		} else {
			delete(b.nonces, n)
		}
	}
	b.nonceOrder = kept
	if len(b.nonceOrder) >= maxNonces {
		oldest := b.nonceOrder[0]
		delete(b.nonces, oldest)
		b.nonceOrder = b.nonceOrder[1:]
	}
	b.nonces[nonce] = nonceEntry{ip: ip, expires: nowT.Add(ttl)}
	b.nonceOrder = append(b.nonceOrder, nonce)
	return nonce, int(ttl / time.Second)
}

// consumeNonce atomically validates and removes a nonce. Any failure
// returns false; only a valid nonce is consumed.
func (b *Bridge) consumeNonce(nonce, ip string) bool {
	now := b.nonceNow
	if now == nil {
		now = time.Now
	}
	b.nonceMu.Lock()
	defer b.nonceMu.Unlock()
	e, ok := b.nonces[nonce]
	if !ok {
		return false
	}
	remove := func() {
		delete(b.nonces, nonce)
		for i, n := range b.nonceOrder {
			if n == nonce {
				b.nonceOrder = append(b.nonceOrder[:i], b.nonceOrder[i+1:]...)
				break
			}
		}
	}
	if !now().Before(e.expires) {
		remove()
		return false
	}
	if e.ip != ip {
		return false
	}
	remove()
	return true
}

// remoteIP returns the host part of r.RemoteAddr for nonce binding.
func remoteIP(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.Trim(host, "[]")
}

// handlePair hands the per-launch token to a local pairing client. Only
// reachable from loopback with no browser-page Origin (websites always send
// their own Origin and get 403). Requires a single-use ?nonce= from
// /pair/challenge, consumed before the token is written so a replay loses.
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
	nonce := r.URL.Query().Get("nonce")
	if nonce == "" || !b.consumeNonce(nonce, remoteIP(r)) {
		http.Error(w, "pairing requires a valid nonce", http.StatusForbidden)
		return
	}
	// The plugin runs inside a file:// / null-origin iframe; without this the
	// browser blocks reading the token even though pairing is allowed.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	writeJSON(w, map[string]any{"token": b.token, "port": b.ListenPort(), "app": AppName, "nonce": nonce})
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

func normalizeEditorType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "word", "cell", "slide", "pdf":
		return strings.ToLower(strings.TrimSpace(t))
	default:
		return "unknown"
	}
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

	// The first frame must be hello within the window; anything else (or
	// silence) closes the connection before any session exists.
	hctx, cancel := context.WithTimeout(r.Context(), helloWindow)
	_, data, err := c.Read(hctx)
	cancel()
	if err != nil {
		slog.Warn("plugin hello timeout", "error", err)
		_ = c.Close(websocket.StatusPolicyViolation, "hello required within 5 seconds")
		return
	}
	var hello helloFrame
	if err := json.Unmarshal(data, &hello); err != nil || hello.Type != "hello" {
		slog.Warn("plugin first frame must be hello")
		_ = c.Close(websocket.StatusPolicyViolation, "first frame must be hello")
		return
	}
	if hello.Protocol != version.BridgeProtocol {
		msg := fmt.Sprintf("unsupported protocol %d, want %d", hello.Protocol, version.BridgeProtocol)
		payload, _ := json.Marshal(map[string]any{"type": "error", "code": CodeProtocolMismatch, "message": msg})
		_ = c.Write(context.Background(), websocket.MessageText, payload)
		slog.Warn("plugin protocol mismatch", "got", hello.Protocol)
		_ = c.Close(websocket.StatusProtocolError, "protocol_mismatch: "+msg)
		return
	}
	if strings.TrimSpace(hello.PluginInstanceID) == "" {
		_ = c.Close(websocket.StatusPolicyViolation, "invalid_frame: missing plugin_instance_id")
		return
	}

	caps := make(map[string]bool, len(hello.Capabilities))
	for _, cp := range hello.Capabilities {
		if cp = strings.TrimSpace(cp); cp != "" {
			caps[cp] = true
		}
	}
	now := time.Now()
	entry := &sessionEntry{
		Session: Session{
			ID:               newWireID(),
			PluginInstanceID: hello.PluginInstanceID,
			EditorType:       normalizeEditorType(hello.EditorType),
			Capabilities:     caps,
			ConnectedAt:      now,
			LastSeen:         now,
		},
		conn:      c,
		pending:   make(map[string]*pendingReq),
		completed: make(map[string]time.Time),
	}
	if err := b.reg.add(entry); err != nil {
		slog.Warn("plugin session limit reached")
		_ = c.Close(websocket.StatusPolicyViolation, "session limit reached (8)")
		return
	}

	ack, _ := json.Marshal(helloAckFrame{
		Type:          "hello_ack",
		Protocol:      version.BridgeProtocol,
		SessionID:     entry.ID,
		ServerVersion: version.Version,
		Limits:        map[string]int{"max_frame_bytes": maxMessageBytes, "request_timeout_ms": 10000},
	})
	if err := c.Write(r.Context(), websocket.MessageText, ack); err != nil {
		b.reg.remove(entry.ID)
		return
	}
	slog.Info("desktop editors connected", "session", entry.ID, "editor", entry.EditorType)

	defer func() {
		if removed := b.reg.remove(entry.ID); removed != nil {
			fail := errDisconnected()
			b.reg.mu.Lock()
			for id, p := range removed.pending {
				// Wake the waiter with a typed error, never a zero result
				// SendCommand could mistake for success.
				select {
				case p.ch <- pendingResult{fail: fail}:
				default:
				}
				markCompletedLocked(removed, id)
				delete(removed.pending, id)
			}
			b.reg.mu.Unlock()
		}
		_ = c.Close(websocket.StatusNormalClosure, "done")
	}()

	ctx := r.Context()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			slog.Warn("plugin read", "error", err)
			return
		}
		var resp responseFrame
		if err := json.Unmarshal(data, &resp); err != nil || resp.Type != "response" {
			slog.Warn("plugin invalid frame: want {type:response,...}")
			continue
		}
		b.deliver(entry, resp)
	}
}

func markCompletedLocked(e *sessionEntry, id string) {
	e.completed[id] = time.Now()
	if len(e.completed) > 128 {
		oldest, oldestAt := "", time.Now()
		for cid, at := range e.completed {
			if oldest == "" || at.Before(oldestAt) {
				oldest, oldestAt = cid, at
			}
		}
		delete(e.completed, oldest)
	}
}

func (b *Bridge) deliver(entry *sessionEntry, resp responseFrame) {
	b.reg.mu.Lock()
	p, ok := entry.pending[resp.ID]
	if ok {
		delete(entry.pending, resp.ID)
		markCompletedLocked(entry, resp.ID)
		entry.LastSeen = time.Now()
	}
	var dup bool
	if !ok {
		_, dup = entry.completed[resp.ID]
		if dup {
			b.duplicate.Add(1)
		} else {
			b.unsolicited.Add(1)
		}
	}
	b.reg.mu.Unlock()

	if !ok {
		// Unsolicited or duplicate: log at warn, ignore, never close the
		// connection and never match it to another request.
		if dup {
			slog.Warn("duplicate plugin response ignored", "session", entry.ID, "id", resp.ID)
		} else {
			slog.Warn("unsolicited plugin response ignored", "session", entry.ID, "id", resp.ID)
		}
		return
	}
	select {
	case p.ch <- pendingResult{resp: resp}:
	default:
	}
}

// DropStats reports ignored responses: unsolicited ids and repeats of
// already-completed ids.
func (b *Bridge) DropStats() (unsolicited, duplicate uint64) {
	return b.unsolicited.Load(), b.duplicate.Load()
}

// Sessions returns the read-only registry snapshot.
func (b *Bridge) Sessions() []SessionInfo {
	return b.reg.Sessions()
}

// SendTo runs one command on the named session.
func (b *Bridge) SendTo(sessionID, method string, params map[string]any, timeout time.Duration) (any, error) {
	entry := b.reg.get(sessionID)
	if entry == nil {
		if b.reg.Count() == 0 {
			return nil, errNotConnected()
		}
		return nil, errSessionNotFound(sessionID)
	}
	return b.sendOnEntry(context.Background(), entry, method, params, timeout)
}

// SendCommand runs one command on the active session (most recently
// connected or used). It keeps the legacy signature used by the six editor
// tool handlers.
func (b *Bridge) SendCommand(ctx context.Context, method string, params map[string]any, timeout time.Duration) (any, error) {
	entry := b.reg.active()
	if entry == nil {
		return nil, errNotConnected()
	}
	return b.sendOnEntry(ctx, entry, method, params, timeout)
}

func (b *Bridge) sendOnEntry(ctx context.Context, entry *sessionEntry, method string, params map[string]any, timeout time.Duration) (any, error) {
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	// Refuse commands the session did not advertise; never assume the
	// editor supports them.
	if need, ok := methodCapabilities[method]; ok && need != "" && !entry.Capabilities[need] {
		return nil, errUnsupported(entry.ID, entry.EditorType, method, need)
	}
	id := newWireID()
	cmd, err := json.Marshal(commandFrame{
		Type: "command", ID: id, Method: method, Params: params,
		DeadlineMs: timeout.Milliseconds(),
	})
	if err != nil {
		return nil, errPluginError(fmt.Sprintf("marshal: %v", err))
	}

	ch := make(chan pendingResult, 1)
	b.reg.mu.Lock()
	if b.reg.sessions[entry.ID] != entry {
		b.reg.mu.Unlock()
		return nil, errSessionNotFound(entry.ID)
	}
	if len(entry.pending) >= MaxPendingPerSession {
		b.reg.mu.Unlock()
		return nil, errTooManyPending()
	}
	entry.pending[id] = &pendingReq{ch: ch}
	b.reg.mu.Unlock()

	fail := func(f *RPCError) (any, error) {
		b.reg.mu.Lock()
		delete(entry.pending, id)
		markCompletedLocked(entry, id)
		b.reg.mu.Unlock()
		return nil, f
	}

	// A deadline already exceeded before the write is a plain timeout:
	// no bytes went out, so the outcome is known (nothing happened).
	if err := ctx.Err(); err != nil {
		return fail(errTimeout("timeout before write: " + err.Error()))
	}
	entry.writeMu.Lock()
	werr := entry.conn.Write(ctx, websocket.MessageText, cmd)
	entry.writeMu.Unlock()
	if werr != nil {
		// The write failed partway: the command may have reached the
		// plugin, so the outcome is unknown.
		return fail(&RPCError{Code: CodePluginError, Message: "write: " + werr.Error(), Retriable: true, OutcomeUnknown: true})
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r, ok := <-ch:
		if !ok {
			return nil, errDisconnected()
		}
		if r.fail != nil {
			return nil, r.fail
		}
		resp := r.resp
		b.reg.mu.Lock()
		entry.LastSeen = time.Now()
		b.reg.mu.Unlock()
		if !resp.OK {
			// Missing or unknown error codes are still errors, never success.
			code, msg := CodePluginError, ""
			if resp.Error != nil {
				msg = resp.Error.Message
				if resp.Error.Code != "" {
					code = resp.Error.Code
				}
			}
			if msg == "" {
				msg = "plugin error"
			}
			return nil, &RPCError{Code: code, Message: "plugin error: " + msg}
		}
		if mutationMethods[method] {
			b.reg.mu.Lock()
			entry.Revision++
			b.reg.mu.Unlock()
		}
		b.reg.touch(entry.ID)
		return resp.Result, nil
	case <-timer.C:
		// The command bytes were written but no reply arrived: outcome unknown.
		b.reg.mu.Lock()
		delete(entry.pending, id)
		markCompletedLocked(entry, id)
		b.reg.mu.Unlock()
		return nil, errOutcomeUnknown("timeout waiting for plugin response (outcome unknown: command was sent)")
	case <-ctx.Done():
		b.reg.mu.Lock()
		delete(entry.pending, id)
		markCompletedLocked(entry, id)
		b.reg.mu.Unlock()
		return nil, &RPCError{Code: CodeTimeout, Message: "context done waiting for plugin response: " + ctx.Err().Error(), Retriable: true, OutcomeUnknown: true}
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

// IsConnected reports whether any session is live.
func (b *Bridge) IsConnected() bool {
	return b.reg.Count() > 0
}
