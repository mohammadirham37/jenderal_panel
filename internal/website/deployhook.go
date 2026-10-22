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
	// go-binary uploads always restart (there is nothing to build); go-build
	// sites can opt out and build from the Deployment tab.
	if w.AppRuntime != "go-binary" && !s.appAutoBuild(ctx, websiteID) {
		write("auto build is off: code updated, app not rebuilt; use Build & Restart on the Deployment tab")
		return nil
	}
	return s.deployGoApp(ctx, websiteID, write)
}

// deployGoApp builds (go-build), verifies the start executable, refreshes the
// unit, restarts it, and waits for the app port. Shared by git deploys and
// the manual Build & Restart.
func (s *Service) deployGoApp(ctx context.Context, websiteID string, write func(string)) error {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return err
	}
	if w.AppType != "go" {
		return model.NewValidationError("build & restart is only available for Go websites")
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

	if err := s.checkAppExecutable(ctx, w, root, write); err != nil {
		return err
	}

	// Rewrite the unit on every deploy so sites created before a unit
	// template fix (e.g. the PORT override) pick it up without re-provisioning.
	if err := writeAppUnitFiles(ctx, s.exec, w); err != nil {
		return err
	}
	// Same for the vhost: older app-proxy vhosts answered 403 on "/".
	if err := s.regenerateConfig(ctx, w, ""); err != nil {
		return fmt.Errorf("regenerate vhost: %w", err)
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
		if ctx.Err() != nil {
			return err
		}
		report := s.appStartupReport(ctx, unit)
		if len(report.Logs) > 0 {
			write("==> app log (last lines)")
			for _, line := range report.Logs {
				write(line)
			}
		}
		return model.NewDomainError("APP_HEALTH_CHECK_FAILED", report.explain(w.AppPort), nil)
	}
	write(fmt.Sprintf("app healthy on port %d", w.AppPort))
	return nil
}
