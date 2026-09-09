package update

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

type Handler struct {
	svc   *Service
	audit *audit.Service
}

func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

func (h *Handler) Current(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	httputil.JSON(w, http.StatusOK, h.svc.Current())
}

func (h *Handler) Check(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	info, err := h.svc.Check(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, info)
}

func (h *Handler) Perform(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "update_started",
		Module: "update",
		Detail: "self-update from source",
		IP:     r.RemoteAddr,
	})

	taskID, err := h.svc.Update(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}
