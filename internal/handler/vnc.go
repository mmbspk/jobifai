package handler

import (
	"io"
	"net"
	"net/http"

	"github.com/coder/websocket"
	"github.com/rs/zerolog/log"
)

type VNCHandlers struct{ svc *Services }

func NewVNCHandlers(svc *Services) *VNCHandlers { return &VNCHandlers{svc: svc} }

// Websockify handles GET /novnc/websockify?token=<JWT>
// Validates the JWT (passed as query param because the browser WebSocket API
// cannot set Authorization headers), then bidirectionally proxies binary frames
// between the noVNC client and x11vnc on localhost:5900.
func (h *VNCHandlers) Websockify(w http.ResponseWriter, r *http.Request) {
	if h.svc.TokenManager == nil {
		http.Error(w, "auth not configured", http.StatusInternalServerError)
		return
	}
	if _, err := h.svc.TokenManager.Verify(r.URL.Query().Get("token")); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	vnc, err := net.Dial("tcp", "localhost:5900")
	if err != nil {
		log.Error().Err(err).Msg("vnc: cannot connect to x11vnc")
		http.Error(w, "VNC unavailable", http.StatusServiceUnavailable)
		return
	}
	defer vnc.Close()

	opts := wsOptions(r)
	opts.Subprotocols = []string{"binary"}
	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx := r.Context()
	done := make(chan struct{}, 2)

	// WS → VNC
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_, msg, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if _, err := vnc.Write(msg); err != nil {
				return
			}
		}
	}()

	// VNC → WS
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 32*1024)
		for {
			n, err := vnc.Read(buf)
			if n > 0 {
				if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Debug().Err(err).Msg("vnc: read from x11vnc")
				}
				return
			}
		}
	}()

	<-done
}
