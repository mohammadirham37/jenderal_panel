package trafficguard

import (
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
	"net/http"
	"time"
)

type Handler struct {
	service *Service
	tasks   *taskrunner.Runner
	audit   *audit.Service
}

func NewHandler(s *Service, t *taskrunner.Runner, a *audit.Service) *Handler {
	return &Handler{service: s, tasks: t, audit: a}
}
func (h *Handler) Profiles(w http.ResponseWriter, r *http.Request) {
	v, e := h.service.Profiles(r.Context())
	if e != nil {
		httputil.HandleError(w, e)
		return
	}
	httputil.JSON(w, http.StatusOK, v)
}
func (h *Handler) Buckets(w http.ResponseWriter, r *http.Request) {
	v, e := h.service.Buckets(r.Context(), chi.URLParam(r, "id"))
	if e != nil {
		httputil.HandleError(w, e)
		return
	}
	httputil.JSON(w, http.StatusOK, v)
}
func (h *Handler) Apply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WebsiteProfile
		Confirm bool `json:"confirm"`
	}
	if e := httputil.DecodeJSON(r, &req); e != nil {
		httputil.HandleError(w, e)
		return
	}
	if req.Mode != "observe" && !req.Confirm {
		httputil.HandleError(w, model.NewValidationError("HTTP enforcement requires explicit confirmation"))
		return
	}
	id := chi.URLParam(r, "id")
	task := h.tasks.RunFuncWithOptions(taskrunner.Options{Name: "Apply Traffic Guard", Module: "security", Timeout: 10 * time.Minute}, func(ctx context.Context, log func(string)) error {
		log("Validating and applying Nginx Traffic Guard configuration\n")
		return h.service.Apply(ctx, id, req.WebsiteProfile, req.Confirm)
	})
	h.log(r, "apply_traffic_guard", id, "accepted Traffic Guard apply; task:"+task)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": task})
}
func (h *Handler) Reset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task := h.tasks.RunFuncWithOptions(taskrunner.Options{Name: "Reset Traffic Guard to Observe", Module: "security", Timeout: 10 * time.Minute}, func(ctx context.Context, log func(string)) error {
		log("Restoring Observe Mode\n")
		return h.service.ResetObserve(ctx, id)
	})
	h.log(r, "reset_traffic_guard", id, "accepted Observe recovery; task:"+task)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": task})
}
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	task := h.tasks.RunFuncWithOptions(taskrunner.Options{Name: "Refresh Cloudflare CIDRs", Module: "security", Timeout: time.Minute}, func(ctx context.Context, log func(string)) error {
		log("Downloading official Cloudflare IPv4 and IPv6 CIDRs\n")
		return h.service.RefreshCloudflare(ctx)
	})
	h.log(r, "refresh_cloudflare_cidrs", "cloudflare", "accepted refresh; task:"+task)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": task})
}
func (h *Handler) log(r *http.Request, action, target, detail string) {
	if h.audit == nil {
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{UserID: u.ID, Action: action, Module: "security", Target: target, Detail: detail, IP: r.RemoteAddr})
}
