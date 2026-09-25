package supervisor

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Handler exposes the supervisor over HTTP.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new supervisor Handler.
func NewHandler(svc *Service, a *audit.Service) *Handler {
	return &Handler{svc: svc, audit: a}
}

// List handles GET /api/v1/supervisor.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	processes, err := h.svc.List(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, processes)
}

// Create handles POST /api/v1/supervisor.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req model.SupervisorProcessRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	p, err := h.svc.Create(r.Context(), req, u.ID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "proc_create", p.ID, "created process "+p.Name)
	httputil.JSON(w, http.StatusCreated, p)
}

// Update handles PUT /api/v1/supervisor/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req model.SupervisorProcessRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	p, err := h.svc.Update(r.Context(), id, req)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "proc_update", id, "updated process "+p.Name)
	httputil.JSON(w, http.StatusOK, p)
}

// Delete handles DELETE /api/v1/supervisor/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Delete(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "proc_delete", id, "deleted process")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Action handles POST /api/v1/supervisor/{id}/{action} (start|stop|restart).
func (h *Handler) Action(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	action := chi.URLParam(r, "action")
	if err := h.svc.Action(r.Context(), id, action); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "proc_"+action, id, action+" process")
	p, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, p)
}

// Logs handles GET /api/v1/supervisor/{id}/logs?lines=N.
func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	lines, err := strconv.Atoi(r.URL.Query().Get("lines"))
	if err != nil || lines <= 0 {
		lines = 100
	}
	logs, err := h.svc.Logs(r.Context(), id, lines)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"logs": logs})
}

func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	if h.audit == nil {
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{UserID: u.ID, Action: action, Module: "supervisor", Target: target, Detail: detail, IP: r.RemoteAddr})
}
