package dependency

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// Handler exposes developer dependency status and background operations.
type Handler struct {
	svc   *Service
	audit *audit.Service
	tasks *taskrunner.Runner
}

func NewHandler(svc *Service, auditSvc *audit.Service, tasks *taskrunner.Runner) *Handler {
	return &Handler{svc: svc, audit: auditSvc, tasks: tasks}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	composer, err := h.svc.ComposerStatus(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, []Status{composer})
}

func (h *Handler) InstallComposer(w http.ResponseWriter, r *http.Request) {
	taskID := h.tasks.RunMultiple("Install Composer", h.svc.ComposerInstallCommands())
	h.logAction(r, "install_composer", "installing Composer; task:"+taskID)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

func (h *Handler) UpdateComposer(w http.ResponseWriter, r *http.Request) {
	taskID := h.tasks.RunMultiple("Update Composer", h.svc.ComposerUpdateCommands())
	h.logAction(r, "update_composer", "updating Composer; task:"+taskID)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

func (h *Handler) logAction(r *http.Request, action, detail string) {
	if h.audit == nil {
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "services",
		Target: "composer",
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}
