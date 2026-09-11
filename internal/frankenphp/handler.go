package frankenphp

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler serves the FrankenPHP server-wide install endpoints.
type Handler struct {
	svc *Service
}

// NewHandler creates a new frankenphp Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Status reports the FrankenPHP installation state.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, status)
}

// Install starts the pinned download in the background and returns the task.
func (h *Handler) Install(w http.ResponseWriter, r *http.Request) {
	taskID, err := h.svc.Install(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}
