package handler

import (
	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// WSHandlers groups WebSocket handlers.
type WSHandlers struct{ svc *Services }

func NewWSHandlers(svc *Services) *WSHandlers { return &WSHandlers{svc: svc} }

// GET /ws/logs — live structured log stream
func (h *WSHandlers) Logs(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	if h.svc.Logs != nil {
		h.svc.Logs.Register(conn)
		defer h.svc.Logs.Unregister(conn)
	} else {
		// Broadcaster not wired — send a single info message.
		_ = wsjson.Write(r.Context(), conn, map[string]string{
			"level":   "info",
			"message": "log stream connected (broadcaster not initialised)",
		})
	}

	// Block until client disconnects.
	for {
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
	}
}
