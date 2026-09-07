package process

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles process-related HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new process Handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// List returns the list of running processes.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	sortBy := r.URL.Query().Get("sort")
	processes, err := h.svc.List(r.Context(), sortBy)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, processes)
}

// Kill sends a signal to a process identified by the PID URL parameter.
func (h *Handler) Kill(w http.ResponseWriter, r *http.Request) {
	pidStr := chi.URLParam(r, "pid")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid PID")
		return
	}

	var req struct {
		Signal string `json:"signal"`
	}
	// Attempt to decode body; if empty or invalid, default to TERM.
	_ = httputil.DecodeJSON(r, &req)
	if req.Signal == "" {
		req.Signal = "TERM"
	}

	if err := h.svc.Kill(r.Context(), pid, req.Signal); err != nil {
		httputil.HandleError(w, err)
		return
	}

	// Audit log the kill action.
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "kill_process",
		Module: "process",
		Target: pidStr,
		Detail: fmt.Sprintf("sent signal %s to PID %d", req.Signal, pid),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
