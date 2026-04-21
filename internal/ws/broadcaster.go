// Package ws provides a WebSocket log broadcaster.
// A zerolog writer pushes JSON log entries to all connected clients.
package ws

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Broadcaster fans structured log lines out to all registered WebSocket connections.
type Broadcaster struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{clients: make(map[*websocket.Conn]struct{})}
}

// Write implements io.Writer so it can be used as a zerolog output.
// Each call is expected to be one complete JSON log line.
func (b *Broadcaster) Write(p []byte) (int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Try to parse as JSON map; fall back to raw string.
	var payload any
	var msg map[string]any
	if err := json.Unmarshal(p, &msg); err == nil {
		payload = msg
	} else {
		payload = string(p)
	}

	ctx := context.Background()
	for conn := range b.clients {
		_ = wsjson.Write(ctx, conn, payload)
	}
	return len(p), nil
}

// Register adds conn to the broadcast set.
func (b *Broadcaster) Register(conn *websocket.Conn) {
	b.mu.Lock()
	b.clients[conn] = struct{}{}
	b.mu.Unlock()
}

// Unregister removes conn from the broadcast set.
func (b *Broadcaster) Unregister(conn *websocket.Conn) {
	b.mu.Lock()
	delete(b.clients, conn)
	b.mu.Unlock()
}
