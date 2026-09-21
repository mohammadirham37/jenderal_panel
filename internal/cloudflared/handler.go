package cloudflared

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// Handler exposes the Cloudflare Tunnel connector over HTTP.
type Handler struct {
	service *Service
	tasks   *taskrunner.Runner
	audit   *audit.Service
}

// NewHandler creates a new cloudflared Handler.
func NewHandler(s *Service, t *taskrunner.Runner, a *audit.Service) *Handler {
	return &Handler{service: s, tasks: t, audit: a}
}

// Status reports connector state.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	st, err := h.service.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, st)
}

// Install downloads and installs the pinned binary (also used for updates).
func (h *Handler) Install(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.Install(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.log(r, "cloudflared_install", "cloudflared", "accepted install; task:"+task)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": task})
}

// Connect wires the pasted connector token into the systemd service.
func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if err := ValidateToken(req.Token); err != nil {
		httputil.HandleError(w, err)
		return
	}
	task := h.tasks.RunFuncWithOptions(
		taskrunner.Options{Name: "Connect Cloudflare Tunnel", Module: "tunnel", Timeout: 2 * time.Minute},
		func(ctx context.Context, log func(string)) error {
			return h.service.Connect(ctx, req.Token, log)
		},
	)
	h.log(r, "cloudflared_connect", "cloudflared", "accepted connect; task:"+task)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": task})
}

// Disconnect stops the connector and removes token + unit.
func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Disconnect(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.log(r, "cloudflared_disconnect", "cloudflared", "tunnel disconnected")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

// Restart restarts the connector service.
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Restart(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.log(r, "cloudflared_restart", "cloudflared", "tunnel service restarted")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "restarted"})
}

// Logs tails the connector's journald log.
func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	lines, err := strconv.Atoi(r.URL.Query().Get("lines"))
	if err != nil || lines <= 0 {
		lines = 200
	}
	output, err := h.service.Logs(r.Context(), lines)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"lines": lines, "output": output})
}

func (h *Handler) log(r *http.Request, action, target, detail string) {
	if h.audit == nil {
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{UserID: u.ID, Action: action, Module: "tunnel", Target: target, Detail: detail, IP: r.RemoteAddr})
}
