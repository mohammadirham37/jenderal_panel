package website

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

const sqliteExtensionMissingExitCode = 42

func (i *Installer) installLaravelSQLiteExtension(ctx context.Context, row websiteRow, progress func(string, string) error) error {
	if !supportedPHP(row.PHPVersion) {
		return model.NewValidationError("unsupported PHP version: " + row.PHPVersion)
	}
	php := "/usr/bin/php" + row.PHPVersion
	check := func(label string) (bool, error) {
		if err := progress("initializing Laravel", "\n=== "+label+" ===\n"); err != nil {
			return false, err
		}
		result, err := i.exec.RunSudo(ctx, "-u", row.WebUser, "--", php, "-r", `exit(extension_loaded('pdo_sqlite') ? 0 : 42);`)
		if err != nil {
			return false, fmt.Errorf("%s: %w", label, err)
		}
		if result == nil {
			return false, fmt.Errorf("%s returned no result", label)
		}
		if output := result.Stdout + result.Stderr; output != "" {
			if err := progress("initializing Laravel", output); err != nil {
				return false, err
			}
		}
		switch result.ExitCode {
		case 0:
			return true, nil
		case sqliteExtensionMissingExitCode:
			return false, nil
		default:
			return false, fmt.Errorf("%s failed (exit %d); see provisioning log", label, result.ExitCode)
		}
	}

	installed, err := check("check PHP SQLite extension")
	if err != nil || installed {
		return err
	}
	packageName := "php" + row.PHPVersion + "-sqlite3"
	if err := progress("initializing Laravel", "Installing "+packageName+" because pdo_sqlite is missing.\n"); err != nil {
		return err
	}
	installCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	result, err := i.exec.RunSudo(installCtx, "apt-get", "install", "-y",
		"-o", "DPkg::Lock::Timeout=120",
		"-o", "Acquire::Retries=2",
		"-o", "Acquire::http::Timeout=30",
		"-o", "Acquire::https::Timeout=30",
		packageName)
	if result != nil && result.Stdout+result.Stderr != "" {
		if progressErr := progress("initializing Laravel", result.Stdout+result.Stderr); progressErr != nil {
			return progressErr
		}
	}
	if err != nil {
		return fmt.Errorf("install %s: %w", packageName, err)
	}
	if result == nil || result.ExitCode != 0 {
		detail := "package manager returned no result"
		if result != nil {
			detail = strings.TrimSpace(result.Stderr)
			if detail == "" {
				detail = strings.TrimSpace(result.Stdout)
			}
		}
		return fmt.Errorf("install %s failed: %s", packageName, detail)
	}
	result, err = i.exec.RunSudo(ctx, "systemctl", "restart", "php"+row.PHPVersion+"-fpm")
	if err != nil {
		return fmt.Errorf("restart PHP %s FPM: %w", row.PHPVersion, err)
	}
	if result == nil || result.ExitCode != 0 {
		return fmt.Errorf("restart PHP %s FPM failed", row.PHPVersion)
	}
	installed, err = check("verify PHP SQLite extension")
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("%s installed but pdo_sqlite is still unavailable", packageName)
	}
	return nil
}

func (s *Service) laravelRepairTarget(ctx context.Context, id string) (websiteRow, string, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return websiteRow{}, "", err
	}
	if w.Status != "active" || w.Framework != "laravel" || w.SetupMode != SetupAutomatic {
		return websiteRow{}, "", model.NewValidationError("Repair Laravel is available for active automatically installed Laravel websites; use Retry for failed provisioning")
	}
	row := websiteRow{ID: w.ID, Domain: w.Domain, AppType: w.AppType, PHPVersion: w.PHPVersion, NodeVersion: w.NodeVersion, WebUser: w.WebUser, DocumentRoot: w.DocumentRoot, Framework: w.Framework, FrameworkVersion: w.FrameworkVersion, FrontendStack: w.FrontendStack, InertiaAdapter: w.InertiaAdapter, ProjectVariant: w.ProjectVariant, SetupMode: w.SetupMode}
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
	return NewInstaller(s.exec).bootstrapLaravel(ctx, row, root, false, true, func(stage, output string) error { log(output); return nil })
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
