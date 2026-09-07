package backup

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles backup management HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new backup Handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// logAction writes an audit log entry for a mutating action.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "backup",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// --- Backup endpoints ---

// CreateBackup handles POST /api/backups.
// Returns 202 Accepted because the backup runs asynchronously.
func (h *Handler) CreateBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type   string `json:"type"`
		Target string `json:"target"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	b, err := h.svc.CreateBackup(r.Context(), req.Type, req.Target)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "create_backup", b.ID, "created "+b.Type+" backup for "+b.Target)
	httputil.JSON(w, http.StatusAccepted, b)
}

// List handles GET /api/backups.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	backups, err := h.svc.List(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, backups)
}

// Get handles GET /api/backups/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	b, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, b)
}

// Delete handles DELETE /api/backups/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteBackup(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_backup", id, "deleted backup")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Restore handles POST /api/backups/{id}/restore.
func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.RestoreBackup(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "restore_backup", id, "restored backup")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Schedule endpoints ---

// ListSchedules handles GET /api/backup-schedules.
func (h *Handler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := h.svc.ListSchedules(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, schedules)
}

// CreateSchedule handles POST /api/backup-schedules.
func (h *Handler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	var req ScheduleRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	sched, err := h.svc.CreateSchedule(r.Context(), req)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "create_backup_schedule", sched.ID, "created "+sched.Type+" backup schedule")
	httputil.JSON(w, http.StatusCreated, sched)
}

// UpdateSchedule handles PUT /api/backup-schedules/{id}.
func (h *Handler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req ScheduleRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.UpdateSchedule(r.Context(), id, req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "update_backup_schedule", id, "updated backup schedule")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteSchedule handles DELETE /api/backup-schedules/{id}.
func (h *Handler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteSchedule(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_backup_schedule", id, "deleted backup schedule")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// EnableSchedule handles POST /api/backup-schedules/{id}/enable.
func (h *Handler) EnableSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.EnableSchedule(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "enable_backup_schedule", id, "enabled backup schedule")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DisableSchedule handles POST /api/backup-schedules/{id}/disable.
func (h *Handler) DisableSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DisableSchedule(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "disable_backup_schedule", id, "disabled backup schedule")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
