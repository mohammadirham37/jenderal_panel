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

type customRequest struct {
	WebsiteID      string `json:"website_id"`
	Domain         string `json:"domain"`
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
}

type autoRenewRequest struct {
	AutoRenew bool `json:"auto_renew"`
}

// Issue handles POST /api/ssl/certificates. It returns 202 Accepted because
// certificate issuance may involve ACME challenges.
func (h *Handler) Issue(w http.ResponseWriter, r *http.Request) {
	var req issueRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.issue(w, r, req.WebsiteID, req.Domain)
}

// IssueForWebsite handles POST /api/websites/{id}/ssl/issue.
func (h *Handler) IssueForWebsite(w http.ResponseWriter, r *http.Request) {
	var req issueRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	websiteID := chi.URLParam(r, "id")
	if err := h.svc.requireWebsite(r.Context(), websiteID); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.issue(w, r, websiteID, req.Domain)
}

func (h *Handler) issue(w http.ResponseWriter, r *http.Request, websiteID, domain string) {
	cert, err := h.svc.Issue(r.Context(), websiteID, domain)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	if cert.Status == "failed" {
		h.logAction(r, "issue_ssl_failed", cert.ID, "SSL certificate issuance failed for "+cert.Domain)
	} else {
		h.logAction(r, "issue_ssl", cert.ID, "issued SSL certificate for "+cert.Domain)
	}
	httputil.JSON(w, http.StatusAccepted, cert)
}

// InstallCustom handles POST /api/v1/ssl/custom.
func (h *Handler) InstallCustom(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	var req customRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.installCustom(w, r, req.WebsiteID, req)
}

// InstallCustomForWebsite handles POST /api/websites/{id}/ssl/custom.
func (h *Handler) InstallCustomForWebsite(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	var req customRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	websiteID := chi.URLParam(r, "id")
	if err := h.svc.requireWebsite(r.Context(), websiteID); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.installCustom(w, r, websiteID, req)
}

func (h *Handler) installCustom(w http.ResponseWriter, r *http.Request, websiteID string, req customRequest) {
	cert, err := h.svc.InstallCustom(
		r.Context(), websiteID, req.Domain,
		[]byte(req.CertificatePEM), []byte(req.PrivateKeyPEM),
	)
	req.CertificatePEM = ""
	req.PrivateKeyPEM = ""
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	if cert.Status == "failed" {
		h.logAction(r, "install_custom_ssl_failed", cert.ID, "custom SSL certificate installation failed for "+cert.Domain+" (issuer: "+cert.Issuer+")")
	} else {
		h.logAction(r, "install_custom_ssl", cert.ID, "installed custom SSL certificate for "+cert.Domain+" (issuer: "+cert.Issuer+")")
	}
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

// ListForWebsite handles GET /api/websites/{id}/ssl.
func (h *Handler) ListForWebsite(w http.ResponseWriter, r *http.Request) {
	certs, err := h.svc.ListByWebsite(r.Context(), chi.URLParam(r, "id"))
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

// Update handles PUT /api/v1/ssl/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req autoRenewRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if err := h.svc.SetAutoRenew(r.Context(), id, req.AutoRenew); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "update_ssl", id, "updated SSL auto-renew setting")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
