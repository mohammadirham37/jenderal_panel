package releaseupgrade

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler serves the release upgrade API.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates the release upgrade handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// Preflight handles GET /api/v1/server/release-upgrade.
func (h *Handler) Preflight(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.Preflight(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, p)
}

// Status handles GET /api/v1/server/release-upgrade/status.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	st, err := h.svc.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, st)
}

// Start handles POST /api/v1/server/release-upgrade.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if err := h.svc.Start(r.Context(), req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.log(r, "release_upgrade_started", "Ubuntu "+FromVersion+" to "+ToVersion)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"status": StateRunning})
}

// Repair handles POST /api/v1/server/release-upgrade/repair.
func (h *Handler) Repair(w http.ResponseWriter, r *http.Request) {
	taskID, err := h.svc.Repair(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.log(r, "release_upgrade_repair", "task:"+taskID)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

func (h *Handler) log(r *http.Request, action, detail string) {
	if h.audit == nil {
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "server",
		Target: "server",
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}
