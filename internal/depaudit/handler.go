package depaudit

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler serves the dependency audit API.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates the handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// Report handles GET /api/v1/security/dependencies.
func (h *Handler) Report(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	report, err := h.svc.Latest(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, report)
}

// Scan handles POST /api/v1/security/dependencies/scan.
func (h *Handler) Scan(w http.ResponseWriter, r *http.Request) {
	id, err := h.svc.StartScan(TriggerManual)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	if h.audit != nil {
		user, _ := auth.UserFromContext(r.Context())
		_ = h.audit.Log(r.Context(), audit.LogEntry{
			UserID: user.ID, Action: "security_dependency_scan", Module: "security",
			Target: "dependencies", Detail: "scan:" + id, IP: r.RemoteAddr,
		})
	}
	httputil.JSON(w, http.StatusAccepted, map[string]string{"scan_id": id})
}
