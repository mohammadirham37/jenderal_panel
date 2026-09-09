package cron

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles cron job management HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new cron Handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// logAction writes an audit log entry for a mutating action.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "cron",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// List handles GET /api/cron-jobs.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.svc.List(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, jobs)
}

// ListForWebsite handles GET /api/websites/{id}/cron-jobs.
func (h *Handler) ListForWebsite(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.svc.ListByWebsite(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, jobs)
}

// Create handles POST /api/cron-jobs.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CronJobRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	job, err := h.svc.Create(r.Context(), req)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "create_cron_job", job.ID, "created cron job: "+job.Command)
	httputil.JSON(w, http.StatusCreated, job)
}

// CreateForWebsite handles POST /api/websites/{id}/cron-jobs.
func (h *Handler) CreateForWebsite(w http.ResponseWriter, r *http.Request) {
	var req CronJobRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	req.WebsiteID = chi.URLParam(r, "id")
	if err := h.svc.requireWebsite(r.Context(), req.WebsiteID); err != nil {
		httputil.HandleError(w, err)
		return
	}
	job, err := h.svc.Create(r.Context(), req)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "create_cron_job", job.ID, "created cron job: "+job.Command)
	httputil.JSON(w, http.StatusCreated, job)
}

// Get handles GET /api/cron-jobs/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, job)
}

// Update handles PUT /api/cron-jobs/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req CronJobRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.Update(r.Context(), id, req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "update_cron_job", id, "updated cron job")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Delete handles DELETE /api/cron-jobs/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.Delete(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_cron_job", id, "deleted cron job")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Enable handles POST /api/cron-jobs/{id}/enable.
func (h *Handler) Enable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.Enable(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "enable_cron_job", id, "enabled cron job")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Disable handles POST /api/cron-jobs/{id}/disable.
func (h *Handler) Disable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.Disable(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "disable_cron_job", id, "disabled cron job")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
