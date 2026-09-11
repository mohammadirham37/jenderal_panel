package website

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/sshaccount"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// Handler handles website management HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
	tasks *taskrunner.Runner
	ssh   *sshaccount.Service
}

// NewHandler creates a new website HTTP handler.
func NewHandler(svc *Service, auditSvc *audit.Service, tasks ...*taskrunner.Runner) *Handler {
	h := &Handler{svc: svc, audit: auditSvc}
	if len(tasks) > 0 {
		h.tasks = tasks[0]
	}
	return h
}

// SetSSHAccounts wires the optional SSH account service used to keep site
// access in sync when website ownership changes.
func (h *Handler) SetSSHAccounts(svc *sshaccount.Service) {
	h.ssh = svc
}

// logAction writes an audit log entry for a mutating action.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "website",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// Create handles POST /api/websites. It returns 202 Accepted because
// provisioning happens asynchronously.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	req.CreatedBy = user.ID

	website, err := h.svc.Create(r.Context(), req)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "create_website", website.ID, "created website "+website.Domain)
	httputil.JSON(w, http.StatusAccepted, website)
}

// List handles GET /api/websites.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	websites, err := h.listScoped(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, websites)
}

// listScoped returns all websites for admins and only the caller's own
// websites for the user role.
func (h *Handler) listScoped(r *http.Request) ([]model.Website, error) {
	if auth.AdminFromContext(r.Context()) {
		return h.svc.List(r.Context())
	}
	user, _ := auth.UserFromContext(r.Context())
	return h.svc.ListByOwner(r.Context(), user.ID)
}

// TransferOwnership handles POST /websites/{id}/owner (admin only).
func (h *Handler) TransferOwnership(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.UserID == "" {
		httputil.HandleError(w, model.NewValidationError("user_id is required"))
		return
	}

	// Capture the previous owner so the SSH account access can be revoked.
	previous, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	website, err := h.svc.TransferOwnership(r.Context(), id, req.UserID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	// Re-run the ACL/group sync for both owners; SSH access follows
	// ownership. Failures are best effort and logged for the admin.
	if h.ssh != nil {
		if previous.CreatedBy != "" && previous.CreatedBy != req.UserID {
			if oldOwner, err := h.svc.OwnerUsername(r.Context(), previous.CreatedBy); err == nil && oldOwner != "" {
				_ = h.ssh.RevokeWebsite(r.Context(), oldOwner, website.WebUser)
			}
		}
		_ = h.ssh.SyncOwnedWebsites(r.Context(), req.UserID)
	}

	h.logAction(r, "transfer_website_ownership", website.ID,
		"transferred ownership of "+website.Domain+" to user "+req.UserID)
	httputil.JSON(w, http.StatusOK, website)
}

// WpStatus handles GET /api/websites/{id}/wp.
func (h *Handler) WpStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.WpStatus(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, status)
}

// WpAction handles POST /api/websites/{id}/wp/{action} and runs the wp-cli
// maintenance action as a background task (202 with the task ID).
func (h *Handler) WpAction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	action := chi.URLParam(r, "action")
	taskID, err := h.svc.WpRunTask(r.Context(), id, action)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "wp_"+action, id, "ran wp-cli "+action)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// OctaneStatus handles GET /api/websites/{id}/octane.
func (h *Handler) OctaneStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	status, err := h.svc.OctaneStatus(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, status)
}

// OctaneEnable handles POST /api/websites/{id}/octane/enable and returns the
// background task that installs Octane and starts the server.
func (h *Handler) OctaneEnable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	taskID, err := h.svc.EnableOctane(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "octane_enable", id, "enabling Laravel Octane")
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// OctaneDisable handles POST /api/websites/{id}/octane/disable.
func (h *Handler) OctaneDisable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.DisableOctane(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "octane_disable", id, "disabled Laravel Octane")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// OctaneAction handles POST /api/websites/{id}/octane/{action} for
// start/stop/restart.
func (h *Handler) OctaneAction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	action := chi.URLParam(r, "action")
	if err := h.svc.OctaneAction(r.Context(), id, action); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "octane_"+action, id, action+" Octane server")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// OctaneReload handles POST /api/websites/{id}/octane/reload and returns the
// background task for the zero-downtime reload.
func (h *Handler) OctaneReload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	taskID, err := h.svc.ReloadOctane(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "octane_reload", id, "reloaded Octane workers")
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// OctaneWorkers handles PUT /api/websites/{id}/octane/workers.
func (h *Handler) OctaneWorkers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Workers int `json:"workers"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	site, err := h.svc.SetOctaneWorkers(r.Context(), id, req.Workers)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "octane_workers", id, strconv.Itoa(req.Workers)+" Octane workers")
	httputil.JSON(w, http.StatusOK, site)
}

// OctaneLog handles GET /api/websites/{id}/logs/octane.
func (h *Handler) OctaneLog(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	lines := parseLines(r)

	content, err := h.svc.GetLogs(r.Context(), id, "octane", lines)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"content": content})
}

// Options handles GET /api/websites/options.
func (h *Handler) Options(w http.ResponseWriter, r *http.Request) {
	options, err := h.svc.Options(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, options)
}

// Get handles GET /api/websites/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	website, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, website)
}

// Update handles PUT /api/websites/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req UpdateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.Update(r.Context(), id, req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "update_website", id, "updated website")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Delete handles DELETE /api/websites/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.Delete(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_website", id, "deleted website")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Suspend handles POST /api/websites/{id}/suspend.
func (h *Handler) Suspend(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Suspend(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "suspend_website", id, "suspended website")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Enable handles POST /api/websites/{id}/enable.
func (h *Handler) Enable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Enable(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "enable_website", id, "enabled website")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Retry handles POST /api/websites/{id}/retry.
func (h *Handler) Retry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Retry(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "retry_website", id, "retrying website provisioning")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetConfig handles GET /api/websites/{id}/config.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	content, err := h.svc.GetConfig(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"content": content})
}

// SaveConfig handles PUT /api/websites/{id}/config.
func (h *Handler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Content string `json:"content"`
	}
	if err := httputil.DecodeJSON(r, &body); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.SaveConfig(r.Context(), id, body.Content); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "save_website_config", id, "saved website nginx config")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SetNginxProfile handles PUT /api/websites/{id}/nginx-profile.
func (h *Handler) SetNginxProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Profile string `json:"profile"`
	}
	if err := httputil.DecodeJSON(r, &body); err != nil {
		httputil.HandleError(w, err)
		return
	}

	site, err := h.svc.SetNginxProfile(r.Context(), id, body.Profile)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "set_website_nginx_profile", id, "set nginx template profile to "+site.NginxProfile)
	httputil.JSON(w, http.StatusOK, map[string]string{
		"status":    "ok",
		"profile":   site.NginxProfile,
		"effective": NginxProfileForWebsite(site),
	})
}

// AddDomain handles POST /api/websites/{id}/domains.
func (h *Handler) AddDomain(w http.ResponseWriter, r *http.Request) {
	websiteID := chi.URLParam(r, "id")
	var body struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := httputil.DecodeJSON(r, &body); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.AddDomain(r.Context(), websiteID, body.Name, body.Type); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "add_domain", websiteID, "added domain "+body.Name)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RemoveDomain handles DELETE /api/websites/{id}/domains/{domainID}.
func (h *Handler) RemoveDomain(w http.ResponseWriter, r *http.Request) {
	websiteID := chi.URLParam(r, "id")
	domainID := chi.URLParam(r, "domainID")

	if err := h.svc.RemoveDomain(r.Context(), websiteID, domainID); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "remove_domain", websiteID, "removed domain "+domainID)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AccessLog handles GET /api/websites/{id}/logs/access.
func (h *Handler) AccessLog(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	lines := parseLines(r)

	content, err := h.svc.GetLogs(r.Context(), id, "access", lines)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"content": content})
}

// ErrorLog handles GET /api/websites/{id}/logs/error.
func (h *Handler) ErrorLog(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	lines := parseLines(r)

	content, err := h.svc.GetLogs(r.Context(), id, "error", lines)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"content": content})
}

// GenerateDeployKey handles POST /api/websites/{id}/deploy-key.
func (h *Handler) GenerateDeployKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	pubKey, err := h.svc.GenerateDeployKey(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "website.deploy_key.generate", id, "")
	httputil.JSON(w, http.StatusOK, map[string]string{"public_key": pubKey})
}

// GetDeployKey handles GET /api/websites/{id}/deploy-key.
func (h *Handler) GetDeployKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	pubKey, exists, err := h.svc.GetDeployKey(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	if !exists {
		httputil.JSONError(w, http.StatusNotFound, "NOT_FOUND", "no deploy key found")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"public_key": pubKey})
}

// DeleteDeployKey handles DELETE /api/websites/{id}/deploy-key.
func (h *Handler) DeleteDeployKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.DeleteDeployKey(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "website.deploy_key.delete", id, "")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetCommandPresets handles GET /api/websites/{id}/command-presets.
func (h *Handler) GetCommandPresets(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	presets, err := h.svc.GetCommandPresets(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, presets)
}

// RunCommand handles POST /api/websites/{id}/run-command.
func (h *Handler) RunCommand(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Command string `json:"command"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	taskID, err := h.svc.RunCommand(r.Context(), id, req.Command)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "website.run_command", id, req.Command)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// UploadDeploy handles POST /api/websites/{id}/upload-deploy.
func (h *Handler) UploadDeploy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	r.ParseMultipartForm(500 << 20) // 500MB max
	file, header, err := r.FormFile("file")
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "file required")
		return
	}
	defer file.Close()
	taskID, err := h.svc.UploadArchive(r.Context(), id, file, header.Filename)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "website.upload_deploy", id, header.Filename)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// GetEnv handles GET /api/websites/{id}/env.
func (h *Handler) GetEnv(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	content, exists, err := h.svc.GetEnvFile(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"exists": exists, "content": content})
}

// UpdateEnv handles PUT /api/websites/{id}/env.
func (h *Handler) UpdateEnv(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body struct {
		Content string `json:"content"`
	}
	if err := httputil.DecodeJSON(r, &body); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.UpdateEnvFile(r.Context(), id, body.Content); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "website.env.update", id, "")
	httputil.JSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// parseLines extracts the "lines" query parameter, defaulting to 100.
func parseLines(r *http.Request) int {
	if s := r.URL.Query().Get("lines"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 100
}
