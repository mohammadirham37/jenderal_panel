package website

import (
	"context"
	"fmt"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/siteops"
)

// RunDeployHook executes the post-deploy steps for app-service sites after
// the deployment worker finished cloning: build (go-build sites), restart
// the systemd unit, and wait for the app to answer on its loopback port.
// The old binary keeps serving when the build fails — the unit is only
// touched after a successful build. Non-go sites are a no-op; the worker
// gates on app_type too, this is the second guard.
func (s *Service) RunDeployHook(ctx context.Context, websiteID string, write func(string)) error {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return err
	}
	if w.AppType != "go" {
		return nil
	}
	if w.AppPort <= 0 {
		return model.NewValidationError("app port is not allocated yet; finish provisioning first")
	}
	runtime := w.AppRuntime
	if runtime == "" {
		// Legacy rows predate app_runtime; go-build is the safe default.
		runtime = "go"
		w.AppRuntime = runtime
	}
	root, _ := siteops.AppProjectRoot(ctx, s.exec, "/home/"+w.WebUser, w.DocumentRoot)
	if root == "" {
		root = w.DocumentRoot
	}

	if runtime == "go" {
		if err := s.runGoBuild(ctx, w, root, write); err != nil {
			return err
		}
	} else {
		write("go-binary site: skipping build")
	}

	unit := appUnitName(w.ID)
	action := "start"
	if res, err := s.exec.RunSudo(ctx, "systemctl", "is-active", unit); err == nil && res.ExitCode == 0 {
		action = "restart"
	}
	if err := s.AppAction(ctx, websiteID, action); err != nil {
		return err
	}
	write("app service " + action + "ed")

	if err := s.waitAppHealthy(ctx, w.AppPort); err != nil {
		return err
	}
	write(fmt.Sprintf("app healthy on port %d", w.AppPort))
	return nil
}
