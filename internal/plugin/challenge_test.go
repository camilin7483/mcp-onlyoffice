package plugin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/version"
)

// fetchChallenge gets a nonce the same way the plugin page does.
func fetchChallenge(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	res, err := http.Get(srv.URL + "/pair/challenge")
	if err != nil {
		t.Fatalf("GET /pair/challenge: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("challenge status = %d, want 200", res.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	nonce, _ := body["nonce"].(string)
	if len(nonce) != 32 {
		t.Fatalf("nonce = %q, want 32 hex chars", nonce)
	}
	if body["expires_in"] != float64(60) {
		t.Fatalf("expires_in = %v, want 60", body["expires_in"])
	}
	return nonce
}

func getPairStatus(t *testing.T, srv *httptest.Server, query string) (int, string) {
	t.Helper()
	res, err := http.Get(srv.URL + "/pair" + query)
	if err != nil {
		t.Fatalf("GET /pair: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(raw)
}

func TestPairChallengeShape(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	nonce := fetchChallenge(t, srv)
	if nonce == "" {
		t.Fatal("empty nonce")
	}
}

func TestPairChallengeCORS(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/pair/challenge", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS /pair/challenge: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", res.StatusCode)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("challenge OPTIONS must allow file:// webview reads")
	}
	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/pair/challenge", nil)
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET /pair/challenge: %v", err)
	}
	res2.Body.Close()
	if res2.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("challenge GET must allow file:// webview reads")
	}
}

func TestPairWithoutNonceForbidden(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	for _, q := range []string{"", "?nonce="} {
		code, body := getPairStatus(t, srv, q)
		if code != http.StatusForbidden {
			t.Errorf("GET /pair%s: status = %d, want 403", q, code)
		}
		if strings.Contains(body, b.Token()) {
			t.Error("403 body must never contain the token")
		}
	}
}

func TestPairUnknownNonceForbidden(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	code, body := getPairStatus(t, srv, "?nonce=0123456789abcdef0123456789abcdef")
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", code)
	}
	if strings.Contains(body, b.Token()) {
		t.Error("403 body must never contain the token")
	}
}

func TestPairNonceReplayForbidden(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	nonce := fetchChallenge(t, srv)
	code, _ := getPairStatus(t, srv, "?nonce="+url.QueryEscape(nonce))
	if code != http.StatusOK {
		t.Fatalf("first use: status = %d, want 200", code)
	}
	code, body := getPairStatus(t, srv, "?nonce="+url.QueryEscape(nonce))
	if code != http.StatusForbidden {
		t.Fatalf("replay: status = %d, want 403", code)
	}
	if strings.Contains(body, b.Token()) {
		t.Error("403 body must never contain the token")
	}
}

func TestPairExpiredNonceForbidden(t *testing.T) {
	b, _ := testBridge(t)
	now := time.Now()
	b.nonceNow = func() time.Time { return now }
	b.nonceTTL = time.Minute
	srv := serveBridge(t, b)
	nonce := fetchChallenge(t, srv)
	now = now.Add(61 * time.Second) // no sleep: move the injected clock
	code, _ := getPairStatus(t, srv, "?nonce="+url.QueryEscape(nonce))
	if code != http.StatusForbidden {
		t.Fatalf("expired nonce: status = %d, want 403", code)
	}
}

func TestPairNonceIPMismatchForbidden(t *testing.T) {
	b, _ := testBridge(t)
	// Issue for IP A by calling the handler directly with a forged RemoteAddr.
	req := httptest.NewRequest(http.MethodGet, "/pair/challenge", nil)
	req.Host = "127.0.0.1"
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	b.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("challenge for IP A: status = %d", rec.Code)
	}
	var ch map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ch); err != nil {
		t.Fatalf("decode: %v", err)
	}
	nonce, _ := ch["nonce"].(string)
	// Same nonce presented from IP B.
	req2 := httptest.NewRequest(http.MethodGet, "/pair?nonce="+url.QueryEscape(nonce), nil)
	req2.Host = "127.0.0.1"
	req2.RemoteAddr = "10.0.0.2:5678"
	rec2 := httptest.NewRecorder()
	b.handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("IP-mismatched nonce: status = %d, want 403", rec2.Code)
	}
}

// TestPairChallengeThenWebSocket runs the full plugin flow: challenge,
// pair, then the token authenticates a websocket handshake.
func TestPairChallengeThenWebSocket(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	nonce := fetchChallenge(t, srv)
	res, err := http.Get(srv.URL + "/pair?nonce=" + url.QueryEscape(nonce))
	if err != nil {
		t.Fatalf("GET /pair: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	token, _ := body["token"].(string)
	if token != b.Token() {
		t.Fatal("pair did not return the bridge token")
	}
	if body["nonce"] != nonce {
		t.Fatalf("pair must echo the consumed nonce, got %v", body["nonce"])
	}
	if body["app"] != AppName {
		t.Fatalf("app = %v, want %s", body["app"], AppName)
	}
	c, _ := helloConnect(t, srv, b, "inst-pair", "word", wordCaps(), version.BridgeProtocol)
	_ = c // helloConnect already authenticated with the paired token
}

func TestNonceTableCapEvictsOldest(t *testing.T) {
	b, _ := testBridge(t)
	var first string
	for i := 0; i < maxNonces+1; i++ {
		n, _ := b.issueChallenge("127.0.0.1")
		if i == 0 {
			first = n
		}
	}
	b.nonceMu.Lock()
	n := len(b.nonces)
	b.nonceMu.Unlock()
	if n != maxNonces {
		t.Fatalf("outstanding nonces = %d, want %d", n, maxNonces)
	}
	if b.consumeNonce(first, "127.0.0.1") {
		t.Error("oldest nonce should have been evicted")
	}
}

func TestPluginHTMLChallengeFlow(t *testing.T) {
	html := pluginIndexHTML()
	for _, want := range []string{"/pair/challenge", "/pair?nonce=", "challenge rejected", "nonce"} {
		if !strings.Contains(html, want) {
			t.Errorf("embedded plugin page should contain %q (challenge-then-pair flow)", want)
		}
	}
	if !strings.Contains(html, "scheduleReconnect()") {
		t.Error("plugin page must keep the backoff reconnect on pairing failure")
	}
}
