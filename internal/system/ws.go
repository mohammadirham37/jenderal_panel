package system

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mohammadirham37/jenderal_panel/internal/auth"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type wsMessage struct {
	Type      string `json:"type"`
	Data      any    `json:"data"`
	Timestamp string `json:"timestamp"`
}

// WSMetrics upgrades the connection to WebSocket and pushes metrics at 5-second
// intervals. The client must be authenticated (user must be in context).
func (h *Handler) WSMetrics(w http.ResponseWriter, r *http.Request) {
	// Verify authentication from context.
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Send latest metrics immediately.
	latest := h.metrics.Buffer().Latest()
	msg := wsMessage{
		Type:      "metrics",
		Data:      latest,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(msg)
	if err == nil {
		_ = conn.WriteMessage(websocket.TextMessage, data)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Read pump: discard incoming messages; detect close.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			m := h.metrics.Buffer().Latest()
			msg := wsMessage{
				Type:      "metrics",
				Data:      m,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}
			data, err := json.Marshal(msg)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}
}
