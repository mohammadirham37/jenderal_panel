package system

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// logUpgrader is used for WebSocket upgrades in log streaming.
// Named differently from the upgrader in ws.go to avoid duplicate declarations.
var logUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// allowedLogPrefixes contains directory prefixes allowed for log reading.
var allowedLogPrefixes = []string{
	"/var/log/nginx/",
	"/var/log/jenderal/",
}

// allowedLogExact contains exact paths allowed for log reading.
var allowedLogExact = []string{
	"/var/log/syslog",
}

// LogService provides log file reading and streaming capabilities.
type LogService struct {
	exec executor.CommandExecutor
}

// NewLogService creates a new LogService.
func NewLogService(exec executor.CommandExecutor) *LogService {
	return &LogService{exec: exec}
}

// isAllowedLogPath checks whether the given path is in the whitelist.
func isAllowedLogPath(path string) bool {
	cleaned := filepath.Clean(path)

	// Reject if path contains ".." or if cleaning changed the path.
	if strings.Contains(path, "..") || cleaned != path {
		return false
	}

	// Check exact matches.
	for _, exact := range allowedLogExact {
		if cleaned == exact {
			return true
		}
	}

	// Check prefix matches.
	for _, prefix := range allowedLogPrefixes {
		if strings.HasPrefix(cleaned, prefix) {
			return true
		}
	}

	return false
}

// ReadLog reads the last N lines of an allowed log file.
func (s *LogService) ReadLog(ctx context.Context, path string, lines int) (string, error) {
	if !isAllowedLogPath(path) {
		return "", model.NewValidationError(fmt.Sprintf("log path %q is not allowed", path))
	}

	if lines <= 0 {
		lines = 100
	}
	if lines > 5000 {
		lines = 5000
	}

	res, err := s.exec.RunSudo(ctx, "tail", "-n", strconv.Itoa(lines), path)
	if err != nil {
		return "", fmt.Errorf("read log: %w", err)
	}
	if res.ExitCode != 0 {
		return "", model.NewDomainError("LOG_READ_FAILED", strings.TrimSpace(res.Stderr), nil)
	}

	return res.Stdout, nil
}

// logWSMessage is the JSON structure sent over the WebSocket.
type logWSMessage struct {
	Type      string `json:"type"`
	Line      string `json:"line"`
	Timestamp string `json:"timestamp"`
}

// StreamLog upgrades the connection to WebSocket and streams log output.
// It polls the log file every 2 seconds, sending new lines as JSON messages.
func (s *LogService) StreamLog(w http.ResponseWriter, r *http.Request, path string) {
	if !isAllowedLogPath(path) {
		http.Error(w, "log path not allowed", http.StatusBadRequest)
		return
	}

	// Verify authentication.
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := logUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

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

	// Track previously sent lines to detect new content.
	var lastLines []string

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Send initial batch immediately.
	s.sendLogLines(conn, r.Context(), path, &lastLines)

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := s.sendLogLines(conn, r.Context(), path, &lastLines); err != nil {
				return
			}
		}
	}
}

// sendLogLines reads the tail of the log and sends any new lines over the WebSocket.
func (s *LogService) sendLogLines(conn *websocket.Conn, ctx context.Context, path string, lastLines *[]string) error {
	res, err := s.exec.RunSudo(ctx, "tail", "-n", "50", path)
	if err != nil {
		return err
	}

	currentLines := strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n")
	if len(currentLines) == 1 && currentLines[0] == "" {
		currentLines = nil
	}

	// Find new lines by comparing against last known output.
	newLines := findNewLines(*lastLines, currentLines)
	*lastLines = currentLines

	now := time.Now().UTC().Format(time.RFC3339)
	for _, line := range newLines {
		msg := logWSMessage{
			Type:      "log",
			Line:      line,
			Timestamp: now,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return err
		}
	}

	return nil
}

// findNewLines returns lines present in current but not in previous.
// It finds the longest suffix of current that was not seen in previous.
func findNewLines(previous, current []string) []string {
	if len(previous) == 0 {
		return current
	}
	if len(current) == 0 {
		return nil
	}

	// Find where the previous tail ends in the current output.
	lastPrev := previous[len(previous)-1]
	matchIdx := -1
	for i := len(current) - 1; i >= 0; i-- {
		if current[i] == lastPrev {
			matchIdx = i
			break
		}
	}

	if matchIdx == -1 {
		// No overlap found, all lines are new.
		return current
	}
	if matchIdx == len(current)-1 {
		// Last line matches, nothing new.
		return nil
	}

	return current[matchIdx+1:]
}
