package deployment

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles deployment HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new deployment HTTP handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// logAction writes an audit log entry for a deployment action.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "deployment",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// Deploy handles POST /api/websites/{id}/deployments.
// It creates a new deployment and returns 202 Accepted.
func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request) {
	websiteID := chi.URLParam(r, "id")

	var body struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
	}
	if err := httputil.DecodeJSON(r, &body); err != nil {
		httputil.HandleError(w, err)
		return
	}

	d, err := h.svc.Deploy(r.Context(), websiteID, body.Repo, body.Branch)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "deploy_website", websiteID, "started deployment "+d.ID+" branch "+body.Branch)
	httputil.JSON(w, http.StatusAccepted, d)
}

// List handles GET /api/websites/{id}/deployments.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	websiteID := chi.URLParam(r, "id")

	deployments, err := h.svc.ListByWebsite(r.Context(), websiteID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, deployments)
}

// Get handles GET /api/deployments/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	d, err := h.svc.GetDeployment(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, d)
}
