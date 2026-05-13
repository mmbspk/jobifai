package ws_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	jobws "github.com/user/jobifai/internal/ws"
)

// dialTest dials the given httptest server and returns an open WS connection.
// The connection and its cancel context are cleaned up automatically.
func dialTest(t *testing.T, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + path
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, url, nil)
	require.NoError(t, err, "dial %s", url)
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

// serveWS wires a broadcaster to an HTTP server so tests can use real WebSocket connections.
func serveWS(t *testing.T, b *jobws.Broadcaster) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		// Pass empty userID — test connections receive all log entries.
		b.Register(conn, "")
		defer func() {
			b.Unregister(conn)
			conn.CloseNow()
		}()
		// Hold the connection open until the client closes it or the request context is done.
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// readJSONWithTimeout reads one WebSocket message or fails after d.
func readJSONWithTimeout(t *testing.T, conn *websocket.Conn, d time.Duration) any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	var msg any
	err := wsjson.Read(ctx, conn, &msg)
	require.NoError(t, err)
	return msg
}

// ── Register / Unregister ─────────────────────────────────────────────────────

func TestBroadcaster_RegisterUnregister(t *testing.T) {
	b := jobws.NewBroadcaster()
	srv := serveWS(t, b)

	conn := dialTest(t, srv, "/ws")

	// Write to broadcaster — registered client should receive it.
	payload := `{"level":"info","message":"hello"}`
	n, err := b.Write([]byte(payload))
	require.NoError(t, err)
	assert.Equal(t, len(payload), n)

	msg := readJSONWithTimeout(t, conn, 2*time.Second)
	asMap, ok := msg.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello", asMap["message"])

	// Close the connection — the server handler will Unregister it.
	conn.CloseNow()

	// A small sleep so the server goroutine processes the close.
	time.Sleep(50 * time.Millisecond)

	// Write again — no clients, should not block or error.
	n2, err2 := b.Write([]byte(`{"level":"info","message":"after unregister"}`))
	require.NoError(t, err2)
	assert.Greater(t, n2, 0)
}

// ── Broadcast to multiple clients ─────────────────────────────────────────────

func TestBroadcaster_MultipleClients(t *testing.T) {
	b := jobws.NewBroadcaster()
	srv := serveWS(t, b)

	conn1 := dialTest(t, srv, "/ws")
	conn2 := dialTest(t, srv, "/ws")

	// Give the server goroutines time to register both connections.
	time.Sleep(20 * time.Millisecond)

	payload := `{"level":"info","message":"broadcast"}`
	_, err := b.Write([]byte(payload))
	require.NoError(t, err)

	var wg sync.WaitGroup
	recv := make([]any, 2)
	for i, c := range []*websocket.Conn{conn1, conn2} {
		wg.Add(1)
		go func(idx int, conn *websocket.Conn) {
			defer wg.Done()
			recv[idx] = readJSONWithTimeout(t, conn, 2*time.Second)
		}(i, c)
	}
	wg.Wait()

	for i, msg := range recv {
		asMap, ok := msg.(map[string]any)
		require.True(t, ok, "client %d: unexpected type", i)
		assert.Equal(t, "broadcast", asMap["message"], "client %d", i)
	}
}

// ── Non-JSON fallback ─────────────────────────────────────────────────────────

func TestBroadcaster_NonJSONPayload(t *testing.T) {
	b := jobws.NewBroadcaster()
	srv := serveWS(t, b)
	conn := dialTest(t, srv, "/ws")
	time.Sleep(20 * time.Millisecond)

	raw := []byte("plain text log line")
	_, err := b.Write(raw)
	require.NoError(t, err)

	msg := readJSONWithTimeout(t, conn, 2*time.Second)
	// Non-JSON is forwarded as a JSON string.
	s, ok := msg.(string)
	require.True(t, ok, "non-JSON payload should be forwarded as a string, got %T", msg)
	assert.Equal(t, "plain text log line", s)
}

// ── Write with no clients is a no-op ─────────────────────────────────────────

func TestBroadcaster_WriteWithNoClients(t *testing.T) {
	b := jobws.NewBroadcaster()
	payload := `{"level":"debug","message":"nobody home"}`
	n, err := b.Write([]byte(payload))
	assert.NoError(t, err)
	assert.Equal(t, len(payload), n)
}

// ── UsageTracker (lives in the llm package, but testing here via its public API) ──────

func TestBroadcaster_JSONStructured(t *testing.T) {
	b := jobws.NewBroadcaster()
	srv := serveWS(t, b)
	conn := dialTest(t, srv, "/ws")
	time.Sleep(20 * time.Millisecond)

	type logLine struct {
		Level   string `json:"level"`
		Message string `json:"message"`
		Extra   int    `json:"extra"`
	}
	line := logLine{Level: "warn", Message: "structured", Extra: 42}
	raw, err := json.Marshal(line)
	require.NoError(t, err)
	_, err = b.Write(raw)
	require.NoError(t, err)

	msg := readJSONWithTimeout(t, conn, 2*time.Second)
	asMap, ok := msg.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "warn", asMap["level"])
	assert.Equal(t, "structured", asMap["message"])
	assert.EqualValues(t, 42, asMap["extra"])
}
