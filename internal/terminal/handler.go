package terminal

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

const (
	// sessionTimeout is the maximum duration a terminal session may stay open.
	sessionTimeout = 30 * time.Minute

	// writeWait is the time allowed to write a message to the client.
	writeWait = 10 * time.Second
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// wsRequest is the JSON message sent by the client.
type wsRequest struct {
	Command string `json:"command"`
}

// wsResponse is the JSON message sent back to the client.
type wsResponse struct {
	Type     string `json:"type"` // "output" or "error"
	Output   string `json:"output,omitempty"`
	ExitCode int    `json:"exit_code"`
}

// Handler handles WebSocket terminal connections.
type Handler struct {
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewHandler creates a new terminal Handler.
func NewHandler(exec executor.CommandExecutor, auditSvc *audit.Service) *Handler {
	return &Handler{exec: exec, audit: auditSvc}
}

// HandleWS upgrades the HTTP connection to a WebSocket and provides a simple
// command execution interface. Each text message is interpreted as a shell
// command; the result is returned as a JSON response. This is NOT a full PTY;
// it is a stateless command executor over WebSocket.
//
// Only authenticated admin users may use the terminal.
func (h *Handler) HandleWS(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Log session start.
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "terminal_session_start",
		Module: "terminal",
		Target: "",
		Detail: "terminal WebSocket session opened",
		IP:     r.RemoteAddr,
	})

	// Session timeout context.
	ctx, cancel := context.WithTimeout(r.Context(), sessionTimeout)
	defer cancel()

	// Read pump: read commands from the client.
	for {
		select {
		case <-ctx.Done():
			resp := wsResponse{
				Type:   "error",
				Output: "session timed out after 30 minutes",
			}
			data, _ := json.Marshal(resp)
			_ = conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "session timeout"),
				time.Now().Add(writeWait))
			_ = conn.WriteMessage(websocket.TextMessage, data)
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		// Try to parse as JSON first; fall back to plain text.
		var req wsRequest
		if jsonErr := json.Unmarshal(message, &req); jsonErr != nil {
			req.Command = strings.TrimSpace(string(message))
		}

		if req.Command == "" {
			continue
		}

		// Audit log the command.
		_ = h.audit.Log(ctx, audit.LogEntry{
			UserID: user.ID,
			Action: "terminal_command",
			Module: "terminal",
			Target: "",
			Detail: req.Command,
			IP:     r.RemoteAddr,
		})

		// Execute the command via sh -c.
		result, execErr := h.exec.RunSudo(ctx, "sh", "-c", req.Command)

		resp := wsResponse{
			Type: "output",
		}

		if execErr != nil {
			resp.Type = "error"
			resp.Output = execErr.Error()
			resp.ExitCode = -1
		} else {
			resp.Output = result.Stdout
			if result.Stderr != "" {
				if resp.Output != "" {
					resp.Output += "\n"
				}
				resp.Output += result.Stderr
			}
			resp.ExitCode = result.ExitCode
		}

		data, _ := json.Marshal(resp)
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}
}
