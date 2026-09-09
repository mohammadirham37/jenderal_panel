package website

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func (s *Service) laravelRepairTarget(ctx context.Context, id string) (websiteRow, string, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return websiteRow{}, "", err
	}
	if w.Status != "active" || w.Framework != "laravel" || w.SetupMode != SetupAutomatic {
		return websiteRow{}, "", model.NewValidationError("Repair Laravel is available for active automatically installed Laravel websites; use Retry for failed provisioning")
	}
	row := websiteRow{ID: w.ID, Domain: w.Domain, AppType: w.AppType, PHPVersion: w.PHPVersion, WebUser: w.WebUser, DocumentRoot: w.DocumentRoot, Framework: w.Framework, FrameworkVersion: w.FrameworkVersion, FrontendStack: w.FrontendStack, InertiaAdapter: w.InertiaAdapter, ProjectVariant: w.ProjectVariant, SetupMode: w.SetupMode}
	root, _, err := installerFinalPaths(row)
	return row, root, err
}

func (s *Service) RepairLaravel(ctx context.Context, id string, log func(string)) error {
	unlock := s.mutations.Lock(id)
	defer unlock()
	row, root, err := s.laravelRepairTarget(ctx, id)
	if err != nil {
		return err
	}
	return NewInstaller(s.exec).bootstrapLaravel(ctx, row, root, false, func(stage, output string) error { log(output); return nil })
}

func (h *Handler) RepairLaravel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if !req.Confirm {
		httputil.HandleError(w, model.NewValidationError("confirm Laravel SQLite repair before running pending migrations"))
		return
	}
	id := chi.URLParam(r, "id")
	row, _, err := h.svc.laravelRepairTarget(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	if h.tasks == nil {
		httputil.JSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task service unavailable"})
		return
	}
	taskID := h.tasks.RunFunc("Repair Laravel "+row.Domain, func(ctx context.Context, log func(string)) error { return h.svc.RepairLaravel(ctx, id, log) })
	h.logAction(r, "repair_laravel", id, "task:"+taskID)
	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}
