package alert

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Handler handles alert rule and history HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new alert Handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// logAction writes an audit log entry for a mutating action.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "alert",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// ListRules handles GET /api/alert-rules.
func (h *Handler) ListRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.svc.ListRules(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, rules)
}

// CreateRule handles POST /api/alert-rules.
func (h *Handler) CreateRule(w http.ResponseWriter, r *http.Request) {
	var req model.AlertRule
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	rule, err := h.svc.CreateRule(r.Context(), req)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "create_alert_rule", rule.ID, "created alert rule for "+rule.Metric)
	httputil.JSON(w, http.StatusCreated, rule)
}

// GetRule handles GET /api/alert-rules/{id}.
func (h *Handler) GetRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rule, err := h.svc.GetRule(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, rule)
}

// UpdateRule handles PUT /api/alert-rules/{id}.
func (h *Handler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req model.AlertRule
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.UpdateRule(r.Context(), id, req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "update_alert_rule", id, "updated alert rule")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteRule handles DELETE /api/alert-rules/{id}.
func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteRule(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_alert_rule", id, "deleted alert rule")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListHistory handles GET /api/alert-history.
func (h *Handler) ListHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	events, err := h.svc.ListHistory(r.Context(), limit)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, events)
}
