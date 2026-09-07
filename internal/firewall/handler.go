package firewall

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Handler handles firewall management HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new firewall Handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// Status returns the current UFW firewall status and rules.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, status)
}

// Enable activates the UFW firewall.
func (h *Handler) Enable(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Enable(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "enable_firewall",
		Module: "firewall",
		Target: "ufw",
		Detail: "enabled UFW firewall",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Disable deactivates the UFW firewall.
func (h *Handler) Disable(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Disable(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "disable_firewall",
		Module: "firewall",
		Target: "ufw",
		Detail: "disabled UFW firewall",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListRules returns only the firewall rules from the current status.
func (h *Handler) ListRules(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, status.Rules)
}

// AddRule adds a new UFW firewall rule.
func (h *Handler) AddRule(w http.ResponseWriter, r *http.Request) {
	var req AddRuleRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.AddRule(r.Context(), req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "add_firewall_rule",
		Module: "firewall",
		Target: fmt.Sprintf("%d/%s", req.Port, req.Protocol),
		Detail: fmt.Sprintf("added %s rule for port %d", req.Action, req.Port),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteRule removes a firewall rule by its number. The rule number is taken
// from the URL path parameter "number", and the "force" query parameter
// controls whether SSH protection warnings are bypassed.
func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	numStr := chi.URLParam(r, "number")
	number, err := strconv.Atoi(numStr)
	if err != nil {
		httputil.HandleError(w, model.NewValidationError("invalid rule number"))
		return
	}

	force := r.URL.Query().Get("force") == "true"

	if err := h.svc.DeleteRule(r.Context(), number, force); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "delete_firewall_rule",
		Module: "firewall",
		Target: numStr,
		Detail: fmt.Sprintf("deleted firewall rule %d (force=%v)", number, force),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
