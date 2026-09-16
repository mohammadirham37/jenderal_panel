package waf

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

// operationTimeout bounds the ModSecurity package installation.
const operationTimeout = 15 * time.Minute

// Handler handles WAF and DoS protection HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
	tasks *taskrunner.Runner
}

// NewHandler creates a new waf Handler.
func NewHandler(svc *Service, auditSvc *audit.Service, tasks *taskrunner.Runner) *Handler {
	return &Handler{svc: svc, audit: auditSvc, tasks: tasks}
}

func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "security",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// Status handles GET /security/waf.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, status)
}

// Install handles POST /security/waf/modsecurity/install.
func (h *Handler) Install(w http.ResponseWriter, r *http.Request) {
	taskID := h.tasks.RunFuncWithOptions(taskrunner.Options{
		Name: "Install ModSecurity WAF", Module: "security", Timeout: operationTimeout,
	}, func(ctx context.Context, log func(string)) error {
		return h.svc.Install(ctx, log)
	})
	h.logAction(r, "install_modsecurity", "waf", "task:"+taskID)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// EnableModsecurity handles POST /security/waf/modsecurity/enable.
func (h *Handler) EnableModsecurity(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Enable(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "enable_modsecurity", "waf", "")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DisableModsecurity handles POST /security/waf/modsecurity/disable.
func (h *Handler) DisableModsecurity(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Disable(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "disable_modsecurity", "waf", "")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SetMode handles PUT /security/waf/modsecurity/mode.
func (h *Handler) SetMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if err := h.svc.SetMode(r.Context(), req.Mode); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "set_modsecurity_mode", "waf", req.Mode)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ConfigureDoS handles PUT /security/waf/dosevasive.
func (h *Handler) ConfigureDoS(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestsPerMinute int `json:"requests_per_minute"`
		Burst             int `json:"burst"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if err := h.svc.ConfigureDoS(r.Context(), req.RequestsPerMinute, req.Burst); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "configure_dos_protection", "waf",
		"requests_per_minute:"+strconv.Itoa(req.RequestsPerMinute)+" burst:"+strconv.Itoa(req.Burst))
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DisableDoS handles POST /security/waf/dosevasive/disable.
func (h *Handler) DisableDoS(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DisableDoS(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "disable_dos_protection", "waf", "")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
