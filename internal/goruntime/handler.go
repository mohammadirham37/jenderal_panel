package goruntime

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles Go runtime HTTP requests.
type Handler struct {
	svc *Service
}

// NewHandler creates a new goruntime Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Status handles GET /api/goruntime.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, status)
}

// Install handles POST /api/goruntime/install and starts the install as a
// background task (202 with the task ID).
func (h *Handler) Install(w http.ResponseWriter, r *http.Request) {
	taskID, err := h.svc.Install(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}
