package queue

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Handler handles queue worker management HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new queue Handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// logAction writes an audit log entry for a mutating action.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "queue",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// Create handles POST /api/queue-workers.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req QueueWorkerRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	worker, err := h.svc.Create(r.Context(), req)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "create_queue_worker", worker.ID, "created queue worker: "+worker.Command)
	httputil.JSON(w, http.StatusCreated, worker)
}

// CreateForWebsite handles POST /api/websites/{id}/queue-workers.
func (h *Handler) CreateForWebsite(w http.ResponseWriter, r *http.Request) {
	var req QueueWorkerRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	req.WebsiteID = chi.URLParam(r, "id")
	worker, err := h.svc.Create(r.Context(), req)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "create_queue_worker", worker.ID, "created queue worker: "+worker.Command)
	httputil.JSON(w, http.StatusCreated, worker)
}

// List handles GET /api/queue-workers.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	workers, err := h.svc.List(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, workers)
}

// ListForWebsite handles GET /api/websites/{id}/queue-workers.
func (h *Handler) ListForWebsite(w http.ResponseWriter, r *http.Request) {
	workers, err := h.svc.ListByWebsite(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, workers)
}

// Get handles GET /api/queue-workers/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	worker, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, worker)
}

// Delete handles DELETE /api/queue-workers/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.Delete(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_queue_worker", id, "deleted queue worker")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Start handles POST /api/queue-workers/{id}/start.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.Start(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "start_queue_worker", id, "started queue worker")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// scopedWorker resolves a worker from the URL and verifies it belongs to the
// website in the same path; mismatches return NotFound so other sites'
// workers are not discoverable.
func (h *Handler) scopedWorker(r *http.Request) (model.QueueWorker, error) {
	worker, err := h.svc.Get(r.Context(), chi.URLParam(r, "workerId"))
	if err != nil {
		return model.QueueWorker{}, err
	}
	if worker.WebsiteID != chi.URLParam(r, "id") {
		return model.QueueWorker{}, model.ErrNotFound
	}
	return worker, nil
}

func (h *Handler) workerActionForWebsite(w http.ResponseWriter, r *http.Request, action string) {
	worker, err := h.scopedWorker(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.RunAction(r.Context(), worker.ID, action); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, action+"_queue_worker", worker.ID, action+" queue worker for website "+worker.WebsiteID)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// StartForWebsite handles POST /api/websites/{id}/queue-workers/{workerId}/start.
func (h *Handler) StartForWebsite(w http.ResponseWriter, r *http.Request) {
	h.workerActionForWebsite(w, r, "start")
}

// StopForWebsite handles POST /api/websites/{id}/queue-workers/{workerId}/stop.
func (h *Handler) StopForWebsite(w http.ResponseWriter, r *http.Request) {
	h.workerActionForWebsite(w, r, "stop")
}

// RestartForWebsite handles POST /api/websites/{id}/queue-workers/{workerId}/restart.
func (h *Handler) RestartForWebsite(w http.ResponseWriter, r *http.Request) {
	h.workerActionForWebsite(w, r, "restart")
}

// DeleteForWebsite handles DELETE /api/websites/{id}/queue-workers/{workerId}.
func (h *Handler) DeleteForWebsite(w http.ResponseWriter, r *http.Request) {
	worker, err := h.scopedWorker(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.Delete(r.Context(), worker.ID); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_queue_worker", worker.ID, "deleted queue worker for website "+worker.WebsiteID)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// StatusForWebsite handles GET /api/websites/{id}/queue-workers/{workerId}/status.
func (h *Handler) StatusForWebsite(w http.ResponseWriter, r *http.Request) {
	worker, err := h.scopedWorker(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	status, err := h.svc.Status(r.Context(), worker.ID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status_output": status})
}

// LogsForWebsite handles GET /api/websites/{id}/queue-workers/{workerId}/logs.
func (h *Handler) LogsForWebsite(w http.ResponseWriter, r *http.Request) {
	worker, err := h.scopedWorker(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	logs, err := h.svc.Logs(r.Context(), worker.ID, lines)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"logs": logs})
}

// Stop handles POST /api/queue-workers/{id}/stop.
func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.Stop(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "stop_queue_worker", id, "stopped queue worker")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Restart handles POST /api/queue-workers/{id}/restart.
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.Restart(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "restart_queue_worker", id, "restarted queue worker")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
