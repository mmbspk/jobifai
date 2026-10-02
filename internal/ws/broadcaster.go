// Package ws provides a WebSocket log broadcaster.
// A zerolog writer pushes JSON log entries to all connected clients.
package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Broadcaster fans structured log lines out to registered WebSocket connections.
// Each connection is associated with a userID at registration time. Log entries
// that carry a "user_id" JSON field are delivered only to the matching connection;
// entries without that field are broadcast to every connection.
// StreamPolicy controls production log filtering for dashboard WebSocket clients.
type StreamPolicy struct {
	Production bool
	// Verbose returns true when the user should receive full log streams (debug, LLM, etc.).
	Verbose func(userID string) bool
}

type Broadcaster struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]string // conn → userID
	policy  StreamPolicy
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{clients: make(map[*websocket.Conn]string)}
}

// SetStreamPolicy configures production filtering. Safe to call before clients connect.
func (b *Broadcaster) SetStreamPolicy(p StreamPolicy) {
	b.mu.Lock()
	b.policy = p
	b.mu.Unlock()
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
	policy := b.policy
	type target struct {
		conn *websocket.Conn
		user string
	}
	targets := make([]target, 0, len(b.clients))
	for conn, connUserID := range b.clients {
		if targetUser != "" && connUserID != targetUser {
			continue
		}
		if policy.Production && policy.Verbose != nil && !policy.Verbose(connUserID) {
			if m, ok := payload.(map[string]any); ok && !VisibleOnDashboard(m) {
				continue
			}
		}
		targets = append(targets, target{conn: conn, user: connUserID})
	}
	b.mu.RUnlock()

	for _, t := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = wsjson.Write(ctx, t.conn, payload)
		cancel()
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
