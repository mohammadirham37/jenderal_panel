package diskusage

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// Handler exposes the disk usage report and cleanup actions over HTTP.
type Handler struct {
	svc   *Service
	audit *audit.Service
	tasks *taskrunner.Runner
}

// NewHandler creates a new diskusage Handler.
func NewHandler(svc *Service, auditSvc *audit.Service, tasks *taskrunner.Runner) *Handler {
	return &Handler{svc: svc, audit: auditSvc, tasks: tasks}
}

// decodeOptions reads and bounds cleanup options from the request body.
func decodeOptions(r *http.Request) (CleanupOptions, error) {
	var opts CleanupOptions
	if err := httputil.DecodeJSON(r, &opts); err != nil {
		return opts, err
	}

	// Keep destructive parameters inside sane bounds regardless of input.
	if opts.JournalVacuumMB < 0 {
		opts.JournalVacuumMB = 0
	}
	if opts.JournalVacuumMB > 1024 {
		opts.JournalVacuumMB = 1024
	}
	if opts.TmpCleanDays < 0 {
		opts.TmpCleanDays = 0
	}
	if opts.TmpCleanDays > 365 {
		opts.TmpCleanDays = 365
	}
	return opts, nil
}

// Overview returns the disk usage report.
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.svc.Overview(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, overview)
}

// PlanCleanup previews the commands that would run for the given options,
// without executing anything.
func (h *Handler) PlanCleanup(w http.ResponseWriter, r *http.Request) {
	opts, err := decodeOptions(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, h.svc.CleanupPlan(opts))
}

// RunCleanup executes the selected cleanup actions as a background task.
func (h *Handler) RunCleanup(w http.ResponseWriter, r *http.Request) {
	opts, err := decodeOptions(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	commands := h.svc.CleanupCommands(opts)
	if len(commands) == 0 {
		httputil.HandleError(w, model.NewValidationError("no cleanup actions selected"))
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	taskID := h.tasks.RunMultiple("Disk cleanup", commands)

	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "disk_cleanup",
		Module: "disk",
		Target: "server",
		Detail: "cleanup started",
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"task_id": taskID})
}
