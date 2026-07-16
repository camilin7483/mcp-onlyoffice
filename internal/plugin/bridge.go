package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Command struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

type Response struct {
	ID     string         `json:"id"`
	Result any            `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
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
}

func NewBridge(port int) *Bridge {
	return &Bridge{
		pending: make(map[string]*pendingReq),
		port:    port,
	}
}

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
	for port := b.port; port < b.port+50; port++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		listener, err = net.Listen("tcp", addr)
		if err == nil {
			b.realPort = port
			break
		}
	}
	if listener == nil {
		// Last resort: random port
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("bridge listen: %w", err)
		}
		b.realPort = listener.Addr().(*net.TCPAddr).Port
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/plugin", b.handleWebSocket)

	b.srv = &http.Server{
		Addr:    listener.Addr().String(),
		Handler: mux,
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

func (b *Bridge) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Warn("websocket accept", "error", err)
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "bye")

	slog.Info("desktop editors connected")

	b.mu.Lock()
	b.conn = c
	b.connected = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.conn = nil
		b.connected = false
		for id, p := range b.pending {
			p.timer.Stop()
			close(p.ch)
			delete(b.pending, id)
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
		return nil, fmt.Errorf("plugin not connected")
	}
	b.pending[id] = &pendingReq{ch: ch, timer: timer}
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

	b.mu.Lock()
	err = b.conn.Write(ctx, websocket.MessageText, data)
	b.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case resp := <-ch:
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

func (b *Bridge) IsConnected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}
