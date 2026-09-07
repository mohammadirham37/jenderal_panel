package service

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
)

// Handler handles service management HTTP requests.
type Handler struct {
	mgr   ServiceManager
	audit *audit.Service
}

// NewHandler creates a new service Handler.
func NewHandler(mgr ServiceManager, auditSvc *audit.Service) *Handler {
	return &Handler{
		mgr:   mgr,
		audit: auditSvc,
	}
}

// List returns the status list for the named service.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	statuses, err := h.mgr.List(r.Context(), name)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, statuses)
}

// Start starts the named service.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.mgr.Start(r.Context(), name); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "start_service",
		Module: "services",
		Target: name,
		Detail: "started service " + name,
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Stop stops the named service.
func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.mgr.Stop(r.Context(), name); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "stop_service",
		Module: "services",
		Target: name,
		Detail: "stopped service " + name,
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Restart restarts the named service.
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.mgr.Restart(r.Context(), name); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "restart_service",
		Module: "services",
		Target: name,
		Detail: "restarted service " + name,
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Reload reloads the named service.
func (h *Handler) Reload(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.mgr.Reload(r.Context(), name); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "reload_service",
		Module: "services",
		Target: name,
		Detail: "reloaded service " + name,
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
