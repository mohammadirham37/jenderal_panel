package security

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type Handler struct {
	service *Service
	events  *EventService
	audit   *audit.Service
}

func NewHandler(service *Service, events *EventService, auditService *audit.Service) *Handler {
	return &Handler{service: service, events: events, audit: auditService}
}

func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.service.Overview(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, overview)
}

func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	filter := EventFilter{
		Status: EventStatus(r.URL.Query().Get("status")), Severity: Severity(r.URL.Query().Get("severity")),
		Component: r.URL.Query().Get("component"), Limit: perPage, Offset: (page - 1) * perPage,
	}
	events, total, err := h.service.ListEvents(r.Context(), filter)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSONList(w, events, page, min(perPage, 200), total)
}

type transitionEventRequest struct {
	Status EventStatus `json:"status"`
}

func (h *Handler) TransitionEvent(w http.ResponseWriter, r *http.Request) {
	var request transitionEventRequest
	if err := httputil.DecodeJSON(r, &request); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if request.Status != StatusAcknowledged && request.Status != StatusResolved && request.Status != StatusFalsePositive {
		httputil.HandleError(w, model.NewValidationError("status must be acknowledged, resolved, or false_positive"))
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.service.TransitionEvent(r.Context(), id, request.Status, time.Now().UTC()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if h.audit != nil {
		user, _ := auth.UserFromContext(r.Context())
		_ = h.audit.Log(r.Context(), audit.LogEntry{
			UserID: user.ID, Action: "transition_security_event", Module: "security",
			Target: id, Detail: fmt.Sprintf("changed event status to %s", request.Status), IP: r.RemoteAddr,
		})
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status": string(request.Status)})
}
