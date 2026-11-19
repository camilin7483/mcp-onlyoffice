package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cam/mcp-onlyoffice/internal/version"
	"github.com/coder/websocket"
)

// wordCaps mirrors the word advertisement in pluginIndexHTML.
func wordCaps() []string {
	return []string{"reader.selected_text", "reader.full_content",
		"writer.insert_text", "writer.replace_text",
		"writer.create_table", "writer.create_list"}
}

func writeFrame(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readFrame(t *testing.T, c *websocket.Conn, timeout time.Duration) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return data
}

// helloConnect dials, sends hello and waits for hello_ack, returning the
// connection and the assigned session id.
func helloConnect(t *testing.T, srv *httptest.Server, b *Bridge, instanceID, editor string, caps []string, protocol int) (*websocket.Conn, string) {
	t.Helper()
	c := dialWS(t, wsURL(srv, b.Token()), "", "")
	writeFrame(t, c, helloFrame{
		Type: "hello", Protocol: protocol, PluginInstanceID: instanceID,
		EditorType: editor, Capabilities: caps, ClientVersion: "test",
	})
	raw := readFrame(t, c, 5*time.Second)
	var ack helloAckFrame
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("decode hello_ack: %v (raw %s)", err, raw)
	}
	if ack.Type != "hello_ack" || ack.Protocol != version.BridgeProtocol || ack.SessionID == "" {
		t.Fatalf("bad hello_ack: %s", raw)
	}
	return c, ack.SessionID
}

func readCommand(t *testing.T, c *websocket.Conn, timeout time.Duration) commandFrame {
	t.Helper()
	raw := readFrame(t, c, timeout)
	var cmd commandFrame
	if err := json.Unmarshal(raw, &cmd); err != nil {
		t.Fatalf("decode command: %v (raw %s)", err, raw)
	}
	if cmd.Type != "command" || cmd.ID == "" || len(cmd.ID) != 32 {
		t.Fatalf("bad command frame: %s", raw)
	}
	return cmd
}

func writeOK(t *testing.T, c *websocket.Conn, id string, result any) {
	t.Helper()
	writeFrame(t, c, responseFrame{Type: "response", ID: id, OK: true, Result: result})
}

// roundtrip sends via the bridge and answers from the client side.
func roundtrip(t *testing.T, c *websocket.Conn, b *Bridge, sessID, method string, reply any) any {
	t.Helper()
	type outcome struct {
		v   any
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		v, err := b.SendTo(sessID, method, nil, 5*time.Second)
		done <- outcome{v, err}
	}()
	cmd := readCommand(t, c, 5*time.Second)
	writeOK(t, c, cmd.ID, reply)
	select {
	case o := <-done:
		if o.err != nil {
			t.Fatalf("SendTo %s: %v", method, o.err)
		}
		return o.v
	case <-time.After(8 * time.Second):
		t.Fatal("roundtrip timed out")
		return nil
	}
}

// 1. Handshake succeeds and returns a session id.
func TestHandshakeSucceeds(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	caps := wordCaps()
	_, sessID := helloConnect(t, srv, b, "inst-1", "word", caps, version.BridgeProtocol)
	if sessID == "" {
		t.Fatal("empty session id")
	}
	sess := b.Sessions()
	if len(sess) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sess))
	}
	if sess[0].PluginInstanceID != "inst-1" || sess[0].EditorType != "word" {
		t.Fatalf("bad snapshot: %+v", sess[0])
	}
	if len(sess[0].Capabilities) != len(caps) {
		t.Fatalf("capabilities = %v, want sorted %v", sess[0].Capabilities, caps)
	}
	for i := 1; i < len(sess[0].Capabilities); i++ {
		if sess[0].Capabilities[i-1] >= sess[0].Capabilities[i] {
			t.Fatalf("capabilities not sorted: %v", sess[0].Capabilities)
		}
	}
	if !sess[0].Active {
		t.Error("single session should be active")
	}
}

// 2. No hello within the window closes the connection.
func TestNoHelloCloses(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	c := dialWS(t, wsURL(srv, b.Token()), "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if err == nil {
		t.Fatal("connection without hello should be closed")
	}
	if n := b.reg.Count(); n != 0 {
		t.Fatalf("sessions = %d, want 0", n)
	}
}

// 3. Wrong protocol closes the connection with no session.
func TestWrongProtocolClosed(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	c := dialWS(t, wsURL(srv, b.Token()), "", "")
	writeFrame(t, c, helloFrame{Type: "hello", Protocol: 999, PluginInstanceID: "inst-x",
		EditorType: "word", Capabilities: wordCaps()})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	sawMismatch := false
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			break
		}
		if strings.Contains(string(data), "protocol_mismatch") {
			sawMismatch = true
		}
	}
	if !sawMismatch {
		t.Error("expected a protocol_mismatch error frame before close")
	}
	if n := b.reg.Count(); n != 0 {
		t.Fatalf("sessions = %d, want 0", n)
	}
}

// 4. Command/response roundtrip on two concurrent sessions.
func TestConcurrentSessionsRoundtrip(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	a, sessA := helloConnect(t, srv, b, "inst-a", "word", wordCaps(), version.BridgeProtocol)
	c2, sessB := helloConnect(t, srv, b, "inst-b", "word", wordCaps(), version.BridgeProtocol)
	if sessA == sessB {
		t.Fatal("session ids must differ")
	}
	if n := b.reg.Count(); n != 2 {
		t.Fatalf("sessions = %d, want 2", n)
	}
	doneA := make(chan any, 1)
	doneB := make(chan any, 1)
	go func() {
		v, err := b.SendTo(sessA, "ping", nil, 5*time.Second)
		if err != nil {
			t.Errorf("session A: %v", err)
			doneA <- nil
			return
		}
		doneA <- v
	}()
	go func() {
		v, err := b.SendTo(sessB, "ping", nil, 5*time.Second)
		if err != nil {
			t.Errorf("session B: %v", err)
			doneB <- nil
			return
		}
		doneB <- v
	}()
	cmdA := readCommand(t, a, 5*time.Second)
	cmdB := readCommand(t, c2, 5*time.Second)
	writeOK(t, a, cmdA.ID, "result-a")
	writeOK(t, c2, cmdB.ID, "result-b")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	select {
	case v := <-doneA:
		if v != "result-a" {
			t.Errorf("session A got %v, want result-a", v)
		}
	case <-ctx.Done():
		t.Fatal("session A timed out")
	}
	select {
	case v := <-doneB:
		if v != "result-b" {
			t.Errorf("session B got %v, want result-b", v)
		}
	case <-ctx.Done():
		t.Fatal("session B timed out")
	}
}

// 5. Unsolicited response id is ignored, pending request still completes.
func TestUnsolicitedIgnored(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	c, sess := helloConnect(t, srv, b, "inst-u", "word", wordCaps(), version.BridgeProtocol)
	u0, _ := b.DropStats()
	done := make(chan any, 1)
	go func() {
		v, err := b.SendTo(sess, "ping", nil, 5*time.Second)
		if err != nil {
			t.Errorf("SendTo: %v", err)
			done <- nil
			return
		}
		done <- v
	}()
	cmd := readCommand(t, c, 5*time.Second)
	writeFrame(t, c, responseFrame{Type: "response", ID: newWireID(), OK: true, Result: "bogus"})
	writeOK(t, c, cmd.ID, "real")
	select {
	case v := <-done:
		if v != "real" {
			t.Fatalf("got %v, want real (unsolicited must not match)", v)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("pending request never completed")
	}
	if u1, _ := b.DropStats(); u1 <= u0 {
		t.Error("unsolicited response was not counted")
	}
	if !b.IsConnected() {
		t.Error("connection must stay open after unsolicited id")
	}
}

// 6. Duplicate response for a completed id is ignored.
func TestDuplicateIgnored(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	c, sess := helloConnect(t, srv, b, "inst-d", "word", wordCaps(), version.BridgeProtocol)
	_, d0 := b.DropStats()
	cmdID := func() string {
		done := make(chan string, 1)
		go func() {
			v, err := b.SendTo(sess, "ping", nil, 5*time.Second)
			if err != nil {
				t.Errorf("SendTo: %v", err)
				done <- ""
				return
			}
			_ = v
			done <- "ok"
		}()
		cmd := readCommand(t, c, 5*time.Second)
		writeOK(t, c, cmd.ID, "one")
		select {
		case <-done:
		case <-time.After(8 * time.Second):
			t.Fatal("first roundtrip timed out")
		}
		return cmd.ID
	}()
	writeFrame(t, c, responseFrame{Type: "response", ID: cmdID, OK: true, Result: "stale"})
	if v := roundtrip(t, c, b, sess, "ping", "two"); v != "two" {
		t.Fatalf("got %v, want two (duplicate must not match)", v)
	}
	if _, d1 := b.DropStats(); d1 <= d0 {
		t.Error("duplicate response was not counted")
	}
}

// 7. Oversized frame rejects/closes the connection with no session left.
func TestOversizedFrameClosed(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	c := dialWS(t, wsURL(srv, b.Token()), "", "")
	big := strings.Repeat("x", 2<<20)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","protocol":2,"plugin_instance_id":"`+big+`"}`))
	rctx, rcancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer rcancel()
	for {
		_, _, err := c.Read(rctx)
		if err != nil {
			break
		}
	}
	if n := b.reg.Count(); n != 0 {
		t.Fatalf("sessions = %d, want 0 after oversized frame", n)
	}
}

// 8. Disconnect during a pending command fails typed. Semantics: the
// command bytes were already written (the client read them), so the
// mutation may have applied -> the typed error carries OutcomeUnknown=true.
func TestDisconnectDuringPending(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	c, sess := helloConnect(t, srv, b, "inst-p", "word", wordCaps(), version.BridgeProtocol)
	done := make(chan error, 1)
	go func() {
		_, err := b.SendTo(sess, "ping", nil, 10*time.Second)
		done <- err
	}()
	_ = readCommand(t, c, 5*time.Second) // proves bytes were written
	_ = c.Close(websocket.StatusNormalClosure, "bye")
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected typed error on disconnect")
		}
		var rpc *RPCError
		if !errors.As(err, &rpc) {
			t.Fatalf("error is %T (%v), want *RPCError", err, err)
		}
		if !rpc.OutcomeUnknown {
			t.Errorf("OutcomeUnknown = false, want true (bytes were written)")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("SendTo did not return after disconnect")
	}
}

// 9. A session lacking the method's capability is refused.
func TestUnsupportedCapability(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	_, sess := helloConnect(t, srv, b, "inst-c", "cell",
		[]string{"reader.selected_text", "writer.insert_text"}, version.BridgeProtocol)
	_, err := b.SendTo(sess, "replace_text", map[string]any{"old": "a", "new": "b"}, 2*time.Second)
	if err == nil {
		t.Fatal("expected unsupported_capability error")
	}
	var rpc *RPCError
	if !errors.As(err, &rpc) {
		t.Fatalf("error is %T (%v), want *RPCError", err, err)
	}
	if rpc.Code != CodeUnsupportedCapability {
		t.Fatalf("code = %q, want %q", rpc.Code, CodeUnsupportedCapability)
	}
	if n := b.reg.Count(); n != 1 {
		t.Fatalf("sessions = %d, want 1 (refused command keeps session)", n)
	}
}

// 10. Session cap enforced: the 9th hello is rejected, 8 remain.
func TestSessionCapEnforced(t *testing.T) {
	b, _ := testBridge(t)
	srv := serveBridge(t, b)
	conns := make([]*websocket.Conn, 0, MaxSessions)
	for i := 0; i < MaxSessions; i++ {
		c, _ := helloConnect(t, srv, b, "inst-cap-"+strconv.Itoa(i), "word", wordCaps(), version.BridgeProtocol)
		conns = append(conns, c)
	}
	_ = conns
	extra := dialWS(t, wsURL(srv, b.Token()), "", "")
	writeFrame(t, extra, helloFrame{Type: "hello", Protocol: version.BridgeProtocol,
		PluginInstanceID: "inst-extra", EditorType: "word", Capabilities: wordCaps()})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	closed := false
	for {
		_, _, err := extra.Read(ctx)
		if err != nil {
			closed = true
			break
		}
	}
	if !closed {
		t.Error("9th session should be closed")
	}
	if n := b.reg.Count(); n != MaxSessions {
		t.Fatalf("sessions = %d, want %d", n, MaxSessions)
	}
}
