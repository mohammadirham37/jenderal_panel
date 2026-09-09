package fail2ban

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/security"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

const operationTimeout = 20 * time.Minute

type Handler struct {
	service *Service
	tasks   *taskrunner.Runner
	audit   *audit.Service
	events  *security.EventService
}

func NewHandler(service *Service, tasks *taskrunner.Runner, auditService *audit.Service, events *security.EventService) *Handler {
	return &Handler{service: service, tasks: tasks, audit: auditService, events: events}
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, status)
}

func (h *Handler) Install(w http.ResponseWriter, r *http.Request) {
	taskID := h.tasks.RunFuncWithOptions(taskrunner.Options{
		Name: "Install Fail2ban", Module: "security", Timeout: operationTimeout,
	}, func(ctx context.Context, log func(string)) error {
		return h.service.InstallWithProgress(ctx, log)
	})
	h.logAction(r, "install_fail2ban", "fail2ban", "accepted Fail2ban installation; task:"+taskID)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

func (h *Handler) Apply(w http.ResponseWriter, r *http.Request) {
	var settings Settings
	if err := httputil.DecodeJSON(r, &settings); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if _, err := Render(settings); err != nil {
		httputil.HandleError(w, err)
		return
	}
	taskID := h.tasks.RunFuncWithOptions(taskrunner.Options{
		Name: "Apply Fail2ban settings", Module: "security", Timeout: operationTimeout,
	}, func(ctx context.Context, log func(string)) error {
		return h.service.ApplyWithProgress(ctx, settings, log)
	})
	h.logAction(r, "apply_fail2ban_settings", "fail2ban", "accepted validated settings; task:"+taskID)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

func (h *Handler) Start(w http.ResponseWriter, r *http.Request)   { h.serviceAction(w, r, "start") }
func (h *Handler) Stop(w http.ResponseWriter, r *http.Request)    { h.serviceAction(w, r, "stop") }
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) { h.serviceAction(w, r, "restart") }

func (h *Handler) serviceAction(w http.ResponseWriter, r *http.Request, action string) {
	var err error
	switch action {
	case "start":
		err = h.service.Start(r.Context())
	case "stop":
		err = h.service.Stop(r.Context())
	case "restart":
		err = h.service.Restart(r.Context())
	default:
		err = model.NewValidationError("unsupported service action")
	}
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, action+"_fail2ban", "fail2ban", action+" Fail2ban service")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Bans(w http.ResponseWriter, r *http.Request) {
	bans, err := h.service.Bans(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	if h.events != nil {
		for _, ban := range bans {
			evidence, _ := json.Marshal(map[string]string{"ip": ban.IP, "jail": ban.Jail})
			_, _, _ = h.events.Record(r.Context(), security.EventInput{
				Fingerprint: "fail2ban:" + ban.Jail + ":" + ban.IP, Category: "intrusion",
				Severity: security.SeverityHigh, Component: "fail2ban", Resource: ban.Jail,
				Evidence: string(evidence), RecommendedAction: "Review authentication logs and confirm whether the address is malicious.",
			}, time.Now().UTC())
		}
	}
	httputil.JSON(w, http.StatusOK, bans)
}

type banRequest struct {
	Jail            string `json:"jail"`
	IP              string `json:"ip"`
	DurationSeconds int    `json:"duration_seconds"`
}

func (h *Handler) Ban(w http.ResponseWriter, r *http.Request) {
	var request banRequest
	if err := httputil.DecodeJSON(r, &request); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if request.DurationSeconds < 60 {
		httputil.HandleError(w, model.NewValidationError("manual bans must be temporary and at least 60 seconds"))
		return
	}
	ban, err := h.service.Ban(r.Context(), request.Jail, request.IP, time.Duration(request.DurationSeconds)*time.Second)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "manual_fail2ban_ban", request.Jail+":"+request.IP,
		fmt.Sprintf("temporary ban for %d seconds", request.DurationSeconds))
	if h.events != nil {
		evidence, _ := json.Marshal(map[string]any{"ip": ban.IP, "jail": ban.Jail, "duration_seconds": request.DurationSeconds})
		_, _, _ = h.events.Record(r.Context(), security.EventInput{
			Fingerprint: "fail2ban:" + ban.Jail + ":" + ban.IP, Category: "intrusion",
			Severity: security.SeverityHigh, Component: "fail2ban", Resource: ban.Jail,
			Evidence: string(evidence), RecommendedAction: "Review authentication logs before extending any response.",
		}, time.Now().UTC())
	}
	httputil.JSON(w, http.StatusCreated, ban)
}

func (h *Handler) Unban(w http.ResponseWriter, r *http.Request) {
	ip, err := url.PathUnescape(chi.URLParam(r, "ip"))
	if err != nil {
		httputil.HandleError(w, model.NewValidationError("invalid IP path"))
		return
	}
	jail := r.URL.Query().Get("jail")
	if jail == "" {
		httputil.HandleError(w, model.NewValidationError("jail query parameter is required"))
		return
	}
	if err := h.service.Unban(r.Context(), jail, ip); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "manual_fail2ban_unban", jail+":"+ip, "removed Fail2ban ban")
	if h.events != nil {
		_ = h.events.ResolveFingerprint(r.Context(), "fail2ban:"+jail+":"+ip, time.Now().UTC())
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	if h.audit == nil {
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID, Action: action, Module: "security", Target: target,
		Detail: detail, IP: r.RemoteAddr,
	})
}
