package nginx

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles Nginx management HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new Nginx HTTP handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// logAction writes an audit log entry for a mutating action.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "nginx",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// Status returns the current Nginx status.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, status)
}

// Install installs Nginx.
func (h *Handler) Install(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Install(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "install_nginx", "nginx", "installed nginx")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Start starts the Nginx service.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Start(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "start_nginx", "nginx", "started nginx")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Stop stops the Nginx service.
func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Stop(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "stop_nginx", "nginx", "stopped nginx")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Restart restarts the Nginx service.
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Restart(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "restart_nginx", "nginx", "restarted nginx")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Reload reloads the Nginx configuration.
func (h *Handler) Reload(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Reload(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "reload_nginx", "nginx", "reloaded nginx")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// TestConfig tests the Nginx configuration.
func (h *Handler) TestConfig(w http.ResponseWriter, r *http.Request) {
	valid, output, err := h.svc.TestConfig(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"valid":  valid,
		"output": output,
	})
}

// GetConfig returns the main Nginx configuration.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	content, err := h.svc.GetMainConfig(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"content": content})
}

// SaveConfig saves the main Nginx configuration.
func (h *Handler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := httputil.DecodeJSON(r, &body); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.SaveMainConfig(r.Context(), body.Content); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "save_nginx_config", "nginx.conf", "saved main nginx config")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListSites returns all site configurations.
func (h *Handler) ListSites(w http.ResponseWriter, r *http.Request) {
	sites, err := h.svc.ListSites(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, sites)
}

// GetSiteConfig returns a specific site configuration.
func (h *Handler) GetSiteConfig(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	content, err := h.svc.GetSiteConfig(r.Context(), name)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"name": name, "content": content})
}

// SaveSiteConfig saves a specific site configuration.
func (h *Handler) SaveSiteConfig(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		Content string `json:"content"`
	}
	if err := httputil.DecodeJSON(r, &body); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.SaveSiteConfig(r.Context(), name, body.Content); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "save_site_config", name, "saved site config "+name)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// EnableSite enables a site by creating a symlink in sites-enabled.
func (h *Handler) EnableSite(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.svc.EnableSite(r.Context(), name); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "enable_site", name, "enabled site "+name)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DisableSite disables a site by removing its symlink from sites-enabled.
func (h *Handler) DisableSite(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.svc.DisableSite(r.Context(), name); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "disable_site", name, "disabled site "+name)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteSite removes a site from both sites-available and sites-enabled.
func (h *Handler) DeleteSite(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.svc.DeleteSite(r.Context(), name); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "delete_site", name, "deleted site "+name)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AccessLog returns the last N lines of the Nginx access log.
func (h *Handler) AccessLog(w http.ResponseWriter, r *http.Request) {
	lines := parseLines(r)
	content, err := h.svc.GetAccessLog(r.Context(), lines)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"content": content})
}

// ErrorLog returns the last N lines of the Nginx error log.
func (h *Handler) ErrorLog(w http.ResponseWriter, r *http.Request) {
	lines := parseLines(r)
	content, err := h.svc.GetErrorLog(r.Context(), lines)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"content": content})
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
