package system

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
)

// Handler handles system-related HTTP requests.
type Handler struct {
	info    *Info
	metrics *MetricsCollector
	audit   *audit.Service
}

// NewHandler creates a new system Handler.
func NewHandler(info *Info, metrics *MetricsCollector, auditSvc *audit.Service) *Handler {
	return &Handler{
		info:    info,
		metrics: metrics,
		audit:   auditSvc,
	}
}

// GetInfo returns server information.
func (h *Handler) GetInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.info.Get(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, info)
}

// Dashboard returns combined server info, latest metrics, and placeholder counts.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	info, err := h.info.Get(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	latest := h.metrics.Buffer().Latest()

	httputil.JSON(w, http.StatusOK, map[string]any{
		"server":  info,
		"metrics": latest,
		"counts": map[string]int{
			"users":    0,
			"services": 0,
		},
	})
}

// DashboardMetrics returns the most recent 60 metrics entries from the database.
func (h *Handler) DashboardMetrics(w http.ResponseWriter, r *http.Request) {
	recent, err := h.metrics.GetRecent(r.Context(), 60)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, recent)
}

// Reboot initiates a system reboot.
func (h *Handler) Reboot(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())

	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "reboot",
		Module: "system",
		Target: "server",
		Detail: "initiated system reboot",
		IP:     r.RemoteAddr,
	})

	if err := h.info.Reboot(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "rebooting"})
}

// SetHostname changes the system hostname.
func (h *Handler) SetHostname(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hostname string `json:"hostname"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.info.SetHostname(r.Context(), req.Hostname); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "set_hostname",
		Module: "system",
		Target: req.Hostname,
		Detail: "changed system hostname",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SetTimezone changes the system timezone.
func (h *Handler) SetTimezone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Timezone string `json:"timezone"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.info.SetTimezone(r.Context(), req.Timezone); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "set_timezone",
		Module: "system",
		Target: req.Timezone,
		Detail: "changed system timezone",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
