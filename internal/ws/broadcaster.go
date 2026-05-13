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

// Broadcaster fans structured log lines out to registered WebSocket connections.
// Each connection is associated with a userID at registration time. Log entries
// that carry a "user_id" JSON field are delivered only to the matching connection;
// entries without that field are broadcast to every connection.
type Broadcaster struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]string // conn → userID
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{clients: make(map[*websocket.Conn]string)}
}

// Write implements io.Writer so it can be used as a zerolog output.
// Each call is expected to be one complete JSON log line.
func (b *Broadcaster) Write(p []byte) (int, error) {
	// Try to parse as JSON map; fall back to raw string.
	var payload any
	var msg map[string]any
	if err := json.Unmarshal(p, &msg); err == nil {
		payload = msg
	} else {
		payload = string(p)
	}

	// Determine which user this log entry belongs to, if any.
	var targetUser string
	if m, ok := payload.(map[string]any); ok {
		targetUser, _ = m["user_id"].(string)
	}

	// Snapshot matching connections under the read lock, then write outside it.
	// This prevents a slow or stalled WebSocket client from blocking all log writes.
	b.mu.RLock()
	conns := make([]*websocket.Conn, 0, len(b.clients))
	for conn, connUserID := range b.clients {
		if targetUser == "" || connUserID == targetUser {
			conns = append(conns, conn)
		}
	}
	b.mu.RUnlock()

	ctx := context.Background()
	for _, conn := range conns {
		_ = wsjson.Write(ctx, conn, payload)
	}
	return len(p), nil
}

// Register adds conn to the broadcast set, associated with userID.
// Pass an empty userID to receive all log entries (no filtering).
func (b *Broadcaster) Register(conn *websocket.Conn, userID string) {
	b.mu.Lock()
	b.clients[conn] = userID
	b.mu.Unlock()
}

// Unregister removes conn from the broadcast set.
func (b *Broadcaster) Unregister(conn *websocket.Conn) {
	b.mu.Lock()
	delete(b.clients, conn)
	b.mu.Unlock()
}
