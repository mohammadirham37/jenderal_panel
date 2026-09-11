package backup

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
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

// callerFromContext resolves the scoped caller for a request.
func callerFromContext(r *http.Request) Caller {
	user, _ := auth.UserFromContext(r.Context())
	return Caller{UserID: user.ID, Admin: auth.AdminFromContext(r.Context())}
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
// Returns 202 Accepted because the backup runs asynchronously as a task.
func (h *Handler) CreateBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type   string `json:"type"`
		Target string `json:"target"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	b, err := h.svc.CreateBackup(r.Context(), callerFromContext(r), req.Type, req.Target)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "create_backup", b.ID, "created "+b.Type+" backup for "+b.Target)
	httputil.JSON(w, http.StatusAccepted, b)
}

// List handles GET /api/backups. Non-admin callers see only their own.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	var (
		backups []model.Backup
		err     error
	)
	if auth.AdminFromContext(r.Context()) {
		backups, err = h.svc.List(r.Context())
	} else {
		backups, err = h.svc.ListByOwner(r.Context(), callerFromContext(r).UserID)
	}
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, backups)
}

// Stats handles GET /api/backups/stats.
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.svc.Stats(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, stats)
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

	b, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	if !auth.CanManageResource(auth.AdminFromContext(r.Context()), callerFromContext(r).UserID, b.CreatedBy) {
		httputil.HandleError(w, model.ErrForbidden)
		return
	}

	if err := h.svc.DeleteBackup(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_backup", id, "deleted backup")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Download handles GET /api/backups/{id}/download and streams the archive
// as an attachment.
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	filename, data, err := h.svc.DownloadBackup(r.Context(), callerFromContext(r), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "download_backup", id, "downloaded "+filename)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// Restore handles POST /api/backups/{id}/restore and runs the restore as a
// background task (202 with the task ID). A safety backup of the current
// state is created automatically before restoring.
func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Component string `json:"component"`
	}
	_ = httputil.DecodeJSON(r, &req) // body optional

	taskID, err := h.svc.RequestRestore(r.Context(), callerFromContext(r), id, req.Component)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "restore_backup", id, "restore started (task "+taskID+")")
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// Prune handles POST /api/backups/prune and runs the retention pass now.
func (h *Handler) Prune(w http.ResponseWriter, r *http.Request) {
	pruned, err := h.svc.PruneNow(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "prune_backups", "all", strconv.Itoa(pruned)+" expired backups pruned")
	httputil.JSON(w, http.StatusOK, map[string]int{"pruned": pruned})
}

// --- Schedule endpoints ---

// ListSchedules handles GET /api/backup-schedules.
func (h *Handler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := h.svc.ListSchedules(r.Context(), callerFromContext(r))
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

	sched, err := h.svc.CreateSchedule(r.Context(), callerFromContext(r), req)
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

	if err := h.svc.UpdateSchedule(r.Context(), callerFromContext(r), id, req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "update_backup_schedule", id, "updated backup schedule")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteSchedule handles DELETE /api/backup-schedules/{id}.
func (h *Handler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteSchedule(r.Context(), callerFromContext(r), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_backup_schedule", id, "deleted backup schedule")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// EnableSchedule handles POST /api/backup-schedules/{id}/enable.
func (h *Handler) EnableSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.EnableSchedule(r.Context(), callerFromContext(r), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "enable_backup_schedule", id, "enabled backup schedule")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DisableSchedule handles POST /api/backup-schedules/{id}/disable.
func (h *Handler) DisableSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DisableSchedule(r.Context(), callerFromContext(r), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "disable_backup_schedule", id, "disabled backup schedule")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
