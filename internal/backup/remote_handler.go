package backup

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// GetRemoteConfig handles GET /api/v1/backup-remote/config.
func (h *Handler) GetRemoteConfig(w http.ResponseWriter, r *http.Request) {
	view, err := h.svc.RemoteConfigView(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, view)
}

// SaveRemoteConfig handles PUT /api/v1/backup-remote/config.
func (h *Handler) SaveRemoteConfig(w http.ResponseWriter, r *http.Request) {
	var req RemoteConfigRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if err := h.svc.SaveRemoteConfig(r.Context(), req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "save_remote_config", req.Type, "remote storage config saved (type "+req.Type+")")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// TestRemote handles POST /api/v1/backup-remote/test.
func (h *Handler) TestRemote(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.TestRemoteConnection(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"info": info})
}

// GDriveAuthorize handles GET /api/v1/backup-remote/gdrive/authorize.
func (h *Handler) GDriveAuthorize(w http.ResponseWriter, r *http.Request) {
	url, err := h.svc.GDriveAuthorizeURL(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"url": url})
}

// GDriveExchange handles POST /api/v1/backup-remote/gdrive/exchange.
func (h *Handler) GDriveExchange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	info, err := h.svc.GDriveExchange(r.Context(), req.Code)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "gdrive_connect", info, "Google Drive connected as "+info)
	httputil.JSON(w, http.StatusOK, map[string]string{"info": info})
}
