package terminal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
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

// wsResponse is the JSON message sent back to the client. A command streams
// zero or more "partial" output messages followed by one final message that
// carries the exit code and the shell's working directory.
type wsResponse struct {
	Type     string `json:"type"` // "output" or "error"
	Output   string `json:"output,omitempty"`
	ExitCode int    `json:"exit_code"`
	Cwd      string `json:"cwd,omitempty"`
	Partial  bool   `json:"partial,omitempty"`
}

// processStarter is the executor capability the terminal needs: start a
// long-running shell process.
type processStarter interface {
	StartSession(ctx context.Context, name string, args ...string) (*executor.Session, error)
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

// HandleWS upgrades the HTTP connection to a WebSocket and runs a persistent
// bash session behind it. All commands from one connection share the same
// shell process, so state such as the working directory and exported
// variables survives between commands.
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

	// Check for website-scoped terminal query parameters.
	websiteID := r.URL.Query().Get("website_id")
	webUser := r.URL.Query().Get("web_user")
	workdir := r.URL.Query().Get("workdir")

	// Log session start.
	sessionDetail := "terminal WebSocket session opened"
	if websiteID != "" {
		sessionDetail = "terminal WebSocket session opened for website " + websiteID
	}
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "terminal_session_start",
		Module: "terminal",
		Target: websiteID,
		Detail: sessionDetail,
		IP:     r.RemoteAddr,
	})

	starter, ok := h.exec.(processStarter)
	if !ok {
		h.sendError(conn, "terminal executor does not support persistent sessions")
		return
	}

	// Session timeout context; cancelling it kills the shell process.
	ctx, cancel := context.WithTimeout(r.Context(), sessionTimeout)
	defer cancel()

	var session *executor.Session
	if webUser != "" {
		session, err = starter.StartSession(ctx, "/usr/bin/sudo", "-u", webUser, "bash", "--norc")
	} else {
		session, err = starter.StartSession(ctx, "/usr/bin/sudo", "bash", "--norc")
	}
	if err != nil {
		h.sendError(conn, "failed to start shell: "+err.Error())
		return
	}
	defer session.Stop()

	writeMu := sync.Mutex{}
	writeJSON := func(resp wsResponse) bool {
		data, err := json.Marshal(resp)
		if err != nil {
			return true // nothing sensible to send; keep draining output
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
		return conn.WriteMessage(websocket.TextMessage, data) == nil
	}

	// Forward shell output to the client: partial chunks stream as they
	// arrive, completion markers become the final message of a command.
	parser := &outputParser{
		onChunk: func(chunk string) {
			_ = writeJSON(wsResponse{Type: "output", Output: chunk, Partial: true})
		},
		onComplete: func(exitCode int, cwd string) {
			_ = writeJSON(wsResponse{Type: "output", ExitCode: exitCode, Cwd: cwd})
		},
	}

	// Merge stdout and stderr into one parser feed; the parser is not safe
	// for concurrent writes.
	merged := make(chan []byte, 32)
	var pumpWG sync.WaitGroup
	pump := func(rd io.Reader) {
		defer pumpWG.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := rd.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				merged <- chunk
			}
			if readErr != nil {
				return
			}
		}
	}
	pumpWG.Add(2)
	go pump(session.Stdout())
	go pump(session.Stderr())
	go func() {
		pumpWG.Wait()
		close(merged)
	}()
	go func() {
		for chunk := range merged {
			_, _ = parser.Write(chunk)
		}
	}()

	// Close the WebSocket when the shell dies or the session times out;
	// this also unblocks the blocking ReadMessage below.
	go func() {
		select {
		case <-session.Done():
			_ = writeJSON(wsResponse{Type: "error", Output: "terminal session ended"})
		case <-ctx.Done():
			_ = writeJSON(wsResponse{Type: "error", Output: "session timed out after 30 minutes"})
		}
		_ = conn.Close()
	}()

	// Start in the website's working directory, then probe once so the
	// client learns the initial working directory.
	initScript := ""
	if webUser != "" && workdir != "" {
		initScript += cdLine(workdir)
	}
	initScript += commandLine(":")
	if _, err := session.Stdin().Write([]byte(initScript)); err != nil {
		h.sendError(conn, "failed to initialize shell: "+err.Error())
		return
	}

	// Read pump: read commands from the client.
	for {
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
			Target: websiteID,
			Detail: req.Command,
			IP:     r.RemoteAddr,
		})

		if _, err := session.Stdin().Write([]byte(commandLine(req.Command))); err != nil {
			h.sendError(conn, "terminal session ended")
			return
		}
	}
}

// sendError writes an error message to the client, tolerating failures.
func (h *Handler) sendError(conn *websocket.Conn, message string) {
	data, err := json.Marshal(wsResponse{Type: "error", Output: message})
	if err != nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
	_ = conn.WriteMessage(websocket.TextMessage, data)
}
