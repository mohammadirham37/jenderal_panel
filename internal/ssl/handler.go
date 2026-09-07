package ssl

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles SSL management HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new SSL HTTP handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// logAction writes an audit log entry for a mutating action.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "ssl",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// issueRequest is the JSON body for issuing a new certificate.
type issueRequest struct {
	WebsiteID string `json:"website_id"`
	Domain    string `json:"domain"`
}

// Issue handles POST /api/ssl/certificates. It returns 202 Accepted because
// certificate issuance may involve ACME challenges.
func (h *Handler) Issue(w http.ResponseWriter, r *http.Request) {
	var req issueRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	cert, err := h.svc.Issue(r.Context(), req.WebsiteID, req.Domain)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "issue_ssl", cert.ID, "issued SSL certificate for "+cert.Domain)
	httputil.JSON(w, http.StatusAccepted, cert)
}

// List handles GET /api/ssl/certificates.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	certs, err := h.svc.List(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, certs)
}

// Get handles GET /api/ssl/certificates/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cert, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, cert)
}

// Renew handles POST /api/ssl/certificates/{id}/renew.
func (h *Handler) Renew(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Renew(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "renew_ssl", id, "renewed SSL certificate")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Revoke handles POST /api/ssl/certificates/{id}/revoke.
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Revoke(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "revoke_ssl", id, "revoked SSL certificate")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Delete handles DELETE /api/ssl/certificates/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Delete(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_ssl", id, "deleted SSL certificate")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
