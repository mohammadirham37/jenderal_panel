package update

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles self-update HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new update Handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// Check handles GET /api/update/check. It queries the GitHub releases API
// and returns information about whether an update is available.
func (h *Handler) Check(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.Check(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, info)
}

// Perform handles POST /api/update/perform. It downloads and installs the
// latest release, then restarts the panel service.
func (h *Handler) Perform(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())

	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "update_started",
		Module: "update",
		Target: "",
		Detail: "self-update initiated",
		IP:     r.RemoteAddr,
	})

	if err := h.svc.Update(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}

	// If we reach here, the restart hasn't killed us yet.
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "update_completed",
		Module: "update",
		Target: "",
		Detail: "self-update completed successfully",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
