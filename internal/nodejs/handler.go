package nodejs

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// Handler handles Node.js management HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
	tasks *taskrunner.Runner
}

// NewHandler creates a new Node.js HTTP handler.
func NewHandler(svc *Service, auditSvc *audit.Service, tasks *taskrunner.Runner) *Handler {
	return &Handler{svc: svc, audit: auditSvc, tasks: tasks}
}

// logAction writes an audit log entry for a mutating action.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "nodejs",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// ListVersions handles GET /api/nodejs/versions.
func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := h.svc.ListVersions(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, versions)
}

// Install handles POST /api/nodejs/install.
func (h *Handler) Install(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	if version == "" {
		var req struct {
			Version string `json:"version"`
		}
		if err := httputil.DecodeJSON(r, &req); err == nil && req.Version != "" {
			version = req.Version
		}
	}
	if version == "" {
		version = "20" // default LTS
	}
	taskID := h.tasks.RunMultiple("Install Node.js "+version, [][]string{
		{"bash", "-c", "curl -fsSL https://deb.nodesource.com/setup_" + version + ".x | bash -"},
		{"apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=120", "nodejs"},
	})

	h.logAction(r, "install_nodejs", "nodejs-"+version, "task:"+taskID)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// CreateApp handles POST /api/nodejs/apps.
func (h *Handler) CreateApp(w http.ResponseWriter, r *http.Request) {
	var req CreateAppRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	app, err := h.svc.CreateApp(r.Context(), req)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "create_node_app", app.ID, fmt.Sprintf("created Node.js app on port %d", app.Port))
	httputil.JSON(w, http.StatusCreated, app)
}

// List handles GET /api/nodejs/apps.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	apps, err := h.svc.List(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, apps)
}

// Get handles GET /api/nodejs/apps/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	app, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, app)
}

// Start handles POST /api/nodejs/apps/{id}/start.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Start(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "start_node_app", id, "started Node.js app")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Stop handles POST /api/nodejs/apps/{id}/stop.
func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Stop(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "stop_node_app", id, "stopped Node.js app")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Restart handles POST /api/nodejs/apps/{id}/restart.
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Restart(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "restart_node_app", id, "restarted Node.js app")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Delete handles DELETE /api/nodejs/apps/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Delete(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "delete_node_app", id, "deleted Node.js app")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
