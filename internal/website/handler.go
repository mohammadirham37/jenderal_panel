package website

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles website management HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new website HTTP handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
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
	websites, err := h.svc.List(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, websites)
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
	removeFiles := r.URL.Query().Get("remove_files") == "true"

	if err := h.svc.Delete(r.Context(), id, removeFiles); err != nil {
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

// parseLines extracts the "lines" query parameter, defaulting to 100.
func parseLines(r *http.Request) int {
	if s := r.URL.Query().Get("lines"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 100
}
