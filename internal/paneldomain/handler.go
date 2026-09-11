package paneldomain

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles panel domain HTTP requests.
type Handler struct {
	svc *Service
}

// NewHandler creates a new paneldomain Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Status handles GET /api/panel-domain.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, status)
}

// Setup handles POST /api/panel-domain/setup and starts the provisioning
// task (202 with the task ID).
func (h *Handler) Setup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
		Email  string `json:"email"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	taskID, err := h.svc.Setup(r.Context(), req.Domain, req.Email)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// Renew handles POST /api/panel-domain/renew and starts the renewal task.
func (h *Handler) Renew(w http.ResponseWriter, r *http.Request) {
	taskID, err := h.svc.Renew(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// Disable handles POST /api/panel-domain/disable.
func (h *Handler) Disable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if err := h.svc.Disable(r.Context(), req.Domain); err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
