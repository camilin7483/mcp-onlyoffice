package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/version"
	"github.com/coder/websocket"
)

func testBridge(t *testing.T) (*Bridge, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return NewBridge(2021), t.TempDir()
}

func serveBridge(t *testing.T, b *Bridge) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(b.handler())
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	_, port, _ := net.SplitHostPort(u.Host)
	p, _ := strconv.Atoi(port)
	b.setRealPort(p)
	return srv
}

func wsURL(srv *httptest.Server, token string) string {
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/plugin"
	if token != "" {
		u += "?token=" + url.QueryEscape(token)
	}
	return u
}

func dialWS(t *testing.T, rawurl string, origin, bearer string) *websocket.Conn {
	t.Helper()
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	if bearer != "" {
		h.Set("Authorization", "Bearer "+bearer)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, rawurl, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		t.Fatalf("dial %s (origin=%q): %v", rawurl, origin, err)
	}
	t.Cleanup(func() { c.Close(websocket.StatusNormalClosure, "test done") })
	return c
}

func TestTokenPersistedWith0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	b := NewBridge(2021)
	if b.Token() == "" {
		t.Fatal("empty token")
	}
	if len(b.Token()) < 32 {
		t.Fatalf("token too short: %q", b.Token())
	}
	want := filepath.Join(home, ".config", "mcp-onlyoffice", "bridge-token")
	if b.TokenPath() != want {
		t.Fatalf("token path = %q, want %q", b.TokenPath(), want)
	}
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if strings.TrimSpace(string(raw)) != b.Token() {
		t.Fatal("token file content mismatch")
	}
	fi, _ := os.Stat(want)
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("token file perm = %o, want 600", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(want))
	if di.Mode().Perm() != 0700 {
		t.Fatalf("token dir perm = %o, want 700", di.Mode().Perm())
	}
}

func TestAllowedWSOrigin(t *testing.T) {
	accept := []string{"", "null", "file://", "file:///index.html",
		"http://127.0.0.1:2021", "https://127.0.0.1", "http://localhost:3000",
		"https://localhost", "http://[::1]:2021", "onlyoffice://plugin"}
	for _, o := range accept {
		if !allowedWSOrigin(o) {
			t.Errorf("origin %q should be accepted", o)
		}
	}
	reject := []string{"https://evil.com", "http://evil.com", "https://127.0.0.1.evil.com",
		"http://192.168.1.1", "ftp://127.0.0.1", "ws://127.0.0.1:2021", "javascript://x"}
	for _, o := range reject {
		if allowedWSOrigin(o) {
			t.Errorf("origin %q should be rejected", o)
		}
	}
}

func TestPingNoAuth(t *testing.T) {
	b, _ := testBridge(t)
	b.setRealPort(2021)
	srv := serveBridge(t, b)
	res, err := http.Get(srv.URL + "/ping")
	if err != nil {
		t.Fatalf("GET /ping: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["app"] != AppName || body["version"] != BridgeVersion {
		t.Fatalf("unexpected body: %v", body)
	}
	if int(body["port"].(float64)) != b.ListenPort() {
		t.Fatalf("port = %v, want %d", body["port"], b.ListenPort())
	}
	if strings.Contains(string(mustJSON(body)), b.Token()) {
		t.Fatal("/ping must not leak the token")
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestPairOriginRules(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	getChallenge := func(origin string) (string, int) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/pair/challenge", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /pair/challenge: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return "", res.StatusCode
		}
		var body map[string]any
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		n, _ := body["nonce"].(string)
		return n, res.StatusCode
	}
	getPair := func(origin, nonce string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/pair?nonce="+url.QueryEscape(nonce), nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /pair: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode == http.StatusOK {
			var body map[string]any
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body["token"] != b.Token() || body["app"] != AppName {
				t.Fatalf("unexpected body: %v", body)
			}
			if body["nonce"] != nonce {
				t.Fatalf("pair must echo consumed nonce, got %v", body)
			}
			return res.StatusCode, body
		}
		return res.StatusCode, nil
	}
	for _, o := range []string{"", "null", "file://", "file:///index.html", "onlyoffice://plugin"} {
		nonce, code := getChallenge(o)
		if code != http.StatusOK || nonce == "" {
			t.Errorf("challenge origin %q: status = %d, want 200 with nonce", o, code)
			continue
		}
		if code := func() int { c, _ := getPair(o, nonce); return c }(); code != http.StatusOK {
			t.Errorf("pair origin %q: status = %d, want 200", o, code)
		}
	}
	for _, o := range []string{"https://evil.com", "http://evil.com", "http://127.0.0.1:2021", "https://localhost"} {
		if _, code := getChallenge(o); code != http.StatusForbidden {
			t.Errorf("challenge origin %q: status = %d, want 403", o, code)
		}
		nonce, code := getChallenge("")
		if code != http.StatusOK {
			t.Fatalf("setup challenge: status = %d", code)
		}
		if c, _ := getPair(o, nonce); c != http.StatusForbidden {
			t.Errorf("pair origin %q: status = %d, want 403", o, c)
		}
	}
}

func TestWebSocketTokenValidation(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dial := func(rawurl string, h http.Header) error {
		c, resp, err := websocket.Dial(ctx, rawurl, &websocket.DialOptions{HTTPHeader: h})
		if err != nil {
			if resp != nil {
				resp.Body.Close()
			}
			return err
		}
		c.Close(websocket.StatusNormalClosure, "test done")
		return nil
	}
	if err := dial(wsURL(srv, ""), nil); err == nil {
		t.Error("missing token should be rejected")
	}
	if err := dial(wsURL(srv, "wrong"), nil); err == nil {
		t.Error("wrong token should be rejected")
	}
	if err := dial(wsURL(srv, b.Token()), nil); err != nil {
		t.Errorf("query token should be accepted: %v", err)
	}
	h := http.Header{}
	h.Set("Authorization", "Bearer "+b.Token())
	if err := dial(wsURL(srv, ""), h); err != nil {
		t.Errorf("bearer token should be accepted: %v", err)
	}
	hBad := http.Header{}
	hBad.Set("Authorization", "Bearer wrong")
	if err := dial(wsURL(srv, ""), hBad); err == nil {
		t.Error("wrong bearer token should be rejected")
	}
}

func TestWebSocketRejectsBrowserOrigin(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	h.Set("Origin", "https://evil.com")
	_, resp, err := websocket.Dial(ctx, wsURL(srv, b.Token()), &websocket.DialOptions{HTTPHeader: h})
	if err == nil {
		t.Fatal("browser page origin should be rejected even with valid token")
	}
	if resp != nil {
		resp.Body.Close()
	}
}

func TestSecondClientCoexists(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	a, sessA := helloConnect(t, srv, b, "inst-a", "word", wordCaps(), version.BridgeProtocol)
	second, sessB := helloConnect(t, srv, b, "inst-b", "word", wordCaps(), version.BridgeProtocol)
	if sessA == sessB {
		t.Fatal("session ids must differ")
	}
	if n := b.reg.Count(); n != 2 {
		t.Fatalf("sessions = %d, want 2 (second client must not evict the first)", n)
	}
	// Both sessions stay usable.
	if v := roundtrip(t, a, b, sessA, "ping", "pong-a"); v != "pong-a" {
		t.Fatalf("session A result = %v, want pong-a", v)
	}
	if v := roundtrip(t, second, b, sessB, "ping", "pong-b"); v != "pong-b" {
		t.Fatalf("session B result = %v, want pong-b", v)
	}
}

func TestSendCommandNoConnection(t *testing.T) {
	b, _ := testBridge(t)
	res, err := b.SendCommand(context.Background(), "ping", nil, time.Second)
	if err == nil {
		t.Fatal("expected error when no plugin is connected")
	}
	if res != nil {
		t.Fatalf("result = %v, want nil (never false success)", res)
	}
}

func TestSendCommandFailsOnDisconnect(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	c, _ := helloConnect(t, srv, b, "inst-disc", "word", wordCaps(), version.BridgeProtocol)
	type outcome struct {
		res any
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := b.SendCommand(context.Background(), "ping", nil, 10*time.Second)
		done <- outcome{res, err}
	}()
	_ = readCommand(t, c, 5*time.Second) // let the command go in-flight
	c.Close(websocket.StatusNormalClosure, "bye")
	select {
	case o := <-done:
		if o.err == nil {
			t.Fatalf("expected disconnect error, got result %v", o.res)
		}
		if o.res != nil {
			t.Fatalf("result = %v, want nil on disconnect", o.res)
		}
		var rpc *RPCError
		if !errors.As(o.err, &rpc) {
			t.Fatalf("error is %T (%v), want *RPCError", o.err, o.err)
		}
		if !rpc.OutcomeUnknown {
			t.Error("disconnect after write must carry OutcomeUnknown=true")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("SendCommand did not return after disconnect")
	}
}

func TestReplaceMatches(t *testing.T) {
	n, err := ReplaceMatches(map[string]any{"matches": float64(3)})
	if err != nil || n != 3 {
		t.Fatalf("got (%d, %v), want (3, nil)", n, err)
	}
	n, err = ReplaceMatches(map[string]any{"matches": float64(0)})
	if err != nil || n != 0 {
		t.Fatalf("zero matches must be valid: got (%d, %v)", n, err)
	}
	for _, bad := range []any{nil, "ok", map[string]any{"ok": true}, map[string]any{}, float64(-1)} {
		if _, err := ReplaceMatches(bad); err == nil {
			t.Errorf("result %v should be an error, never silent success", bad)
		}
	}
}
