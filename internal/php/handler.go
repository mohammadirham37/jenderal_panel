package php

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// Handler handles PHP management HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
	tasks *taskrunner.Runner
}

// NewHandler creates a new PHP HTTP handler.
func NewHandler(svc *Service, auditSvc *audit.Service, tasks *taskrunner.Runner) *Handler {
	return &Handler{svc: svc, audit: auditSvc, tasks: tasks}
}

// logAction writes an audit log entry for a mutating action.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "php",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// List returns the status of all supported PHP versions.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	versions, err := h.svc.ListInstalled(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, versions)
}

// Install installs the specified PHP version via background task.
func (h *Handler) Install(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	if err := h.svc.validateVersion(version); err != nil {
		httputil.HandleError(w, err)
		return
	}

	taskID := h.tasks.RunMultiple("Install PHP "+version, [][]string{
		{"apt-get", "update", "-qq"},
		{"apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=120", "software-properties-common", "ca-certificates"},
		{"add-apt-repository", "-y", "ppa:ondrej/php"},
		{"apt-get", "update", "-qq"},
		{"apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=120",
			"php" + version + "-fpm",
			"php" + version + "-cli",
			"php" + version + "-common",
			"php" + version + "-mysql",
			"php" + version + "-pgsql",
			"php" + version + "-mbstring",
			"php" + version + "-xml",
			"php" + version + "-curl",
			"php" + version + "-zip",
			"php" + version + "-gd",
			"php" + version + "-intl",
			"php" + version + "-bcmath",
		},
	})

	h.logAction(r, "install_php", "php"+version, "task:"+taskID)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// Uninstall removes the specified PHP version.
func (h *Handler) Uninstall(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	if err := h.svc.Uninstall(r.Context(), version); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "uninstall_php", "php"+version, "uninstalled PHP "+version)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Restart restarts the PHP-FPM service for the specified version.
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	if err := h.svc.Restart(r.Context(), version); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "restart_php", "php"+version+"-fpm", "restarted PHP "+version+" FPM")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetConfig returns the FPM php.ini content for the specified PHP version.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	content, err := h.svc.GetPHPINI(r.Context(), version)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"content": content})
}

// SaveConfig saves the FPM php.ini content for the specified PHP version.
func (h *Handler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	var body struct {
		Content string `json:"content"`
	}
	if err := httputil.DecodeJSON(r, &body); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.SavePHPINI(r.Context(), version, body.Content); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "save_php_config", "php"+version+"/php.ini", "saved PHP "+version+" FPM config")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
