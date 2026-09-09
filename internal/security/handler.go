package security

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

type Handler struct {
	service *Service
	events  *EventService
	audit   *audit.Service
	setup   *SetupService
	tasks   *taskrunner.Runner
}

func (h *Handler) SetSetupService(setup *SetupService, tasks *taskrunner.Runner) {
	h.setup, h.tasks = setup, tasks
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

func (h *Handler) Posture(w http.ResponseWriter, r *http.Request) {
	h.service.RefreshPosture(r.Context(), time.Now().UTC())
	httputil.JSON(w, http.StatusOK, h.service.Posture(r.Context()))
}

func (h *Handler) SetupAssessment(w http.ResponseWriter, r *http.Request) {
	assessment, err := h.setup.Assess(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, assessment)
}

func (h *Handler) SetupReview(w http.ResponseWriter, r *http.Request) {
	var request SetupRequest
	if err := httputil.DecodeJSON(r, &request); err != nil {
		httputil.HandleError(w, err)
		return
	}
	review, err := h.setup.Review(r.Context(), request)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, review)
}

func (h *Handler) SetupApply(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Request SetupRequest `json:"request"`
		Review  SetupReview  `json:"review"`
		Confirm bool         `json:"confirm"`
	}
	if err := httputil.DecodeJSON(r, &request); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if !request.Confirm {
		httputil.HandleError(w, model.NewValidationError("confirm the reviewed security setup before applying"))
		return
	}
	run, err := h.setup.CreateRun(r.Context(), request.Request, request.Review)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	taskID := h.tasks.RunFuncWithOptions(taskrunner.Options{Name: "Safe Security Setup", Module: "security", Timeout: 60 * time.Minute}, func(ctx context.Context, log func(string)) error { return h.setup.Execute(ctx, run.ID, log) })
	_ = h.setup.BindTask(r.Context(), run.ID, taskID)
	h.logSetup(r, "apply_security_setup", run.ID, "accepted reviewed setup hash "+run.Review.Hash+"; task:"+taskID)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"run_id": run.ID, "task_id": taskID})
}

func (h *Handler) SetupResume(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RunID string `json:"run_id"`
	}
	if err := httputil.DecodeJSON(r, &request); err != nil {
		httputil.HandleError(w, err)
		return
	}
	state, err := h.setup.State(r.Context(), request.RunID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	if state.Status == "completed" {
		httputil.HandleError(w, model.NewValidationError("security setup is already complete"))
		return
	}
	if state.Status == "running" && state.TaskID != "" {
		if task, ok := h.tasks.Get(state.TaskID); ok && task.Status == "running" {
			httputil.HandleError(w, model.NewValidationError("security setup is already running"))
			return
		}
	}
	taskID := h.tasks.RunFuncWithOptions(taskrunner.Options{Name: "Resume Safe Security Setup", Module: "security", Timeout: 60 * time.Minute}, func(ctx context.Context, log func(string)) error { return h.setup.Execute(ctx, state.ID, log) })
	_ = h.setup.BindTask(r.Context(), state.ID, taskID)
	h.logSetup(r, "resume_security_setup", state.ID, "resumed setup; task:"+taskID)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"run_id": state.ID, "task_id": taskID})
}

func (h *Handler) logSetup(r *http.Request, action, target, detail string) {
	if h.audit == nil {
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{UserID: user.ID, Action: action, Module: "security", Target: target, Detail: detail, IP: r.RemoteAddr})
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
