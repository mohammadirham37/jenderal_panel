package website

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Diagnostic check statuses.
const (
	DiagOK   = "ok"
	DiagWarn = "warn"
	DiagFail = "fail"
)

// DiagnosticCheck is one probe of the diagnose report. ID maps to a frontend
// label; Hint is an i18n key describing the suggested fix.
type DiagnosticCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

// DiagnosticsReport is the outcome of a per-website diagnose run.
type DiagnosticsReport struct {
	WebsiteID string            `json:"website_id"`
	Overall   string            `json:"overall"`
	Checks    []DiagnosticCheck `json:"checks"`
	ErrorLog  string            `json:"error_log,omitempty"`
}

// Diagnose runs server-side checks that explain why a website answers with
// 404 ("File not found") or 403: vhost presence, nginx config, document root
// content, the nginx worker's filesystem access, PHP-FPM, and Laravel
// project layout. Read-only.
func (s *Service) Diagnose(ctx context.Context, websiteID string) (DiagnosticsReport, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return DiagnosticsReport{}, err
	}
	report := s.diagnoseWebsite(ctx, w)
	report.WebsiteID = websiteID
	report.Overall = DiagOK
	for _, c := range report.Checks {
		if c.Status == DiagFail {
			report.Overall = DiagFail
			break
		}
		if c.Status == DiagWarn {
			report.Overall = DiagWarn
		}
	}
	return report, nil
}

// RepairServing re-applies the nginx worker's filesystem access to the site
// (boundaries, document root, Laravel public storage). Use after diagnosing
// a permission-caused 404/403.
func (s *Service) RepairServing(ctx context.Context, websiteID string) error {
	if s.prov == nil {
		return model.NewDomainError("PROVISIONER_UNAVAILABLE", "website provisioner is not available", nil)
	}
	unlock := s.mutations.Lock(websiteID)
	defer unlock()
	return s.prov.RestoreServingAccess(ctx, websiteID)
}

func (s *Service) diagnoseWebsite(ctx context.Context, w model.Website) DiagnosticsReport {
	homeDir := "/home/" + w.WebUser
	docRoot := filepath.Clean(w.DocumentRoot)
	logDir := homeDir + "/logs"
	isLaravel := w.Framework == "laravel"

	var checks []DiagnosticCheck
	add := func(id, status, detail, hint string) {
		checks = append(checks, DiagnosticCheck{ID: id, Status: status, Detail: detail, Hint: hint})
	}

	// Vhost enabled?
	res, err := s.exec.RunSudo(ctx, "test", "-f", "/etc/nginx/sites-enabled/"+w.Domain)
	if err != nil {
		add("vhost", DiagFail, err.Error(), "wd.diag.hint_reload")
	} else if res.ExitCode != 0 {
		add("vhost", DiagFail, "/etc/nginx/sites-enabled/"+w.Domain+" is missing", "wd.diag.hint_vhost")
	} else {
		add("vhost", DiagOK, "/etc/nginx/sites-enabled/"+w.Domain, "")
	}

	// nginx config valid?
	if tRes, tErr := s.exec.RunSudo(ctx, "nginx", "-t"); tErr != nil {
		add("nginx_config", DiagFail, tErr.Error(), "wd.diag.hint_nginx_config")
	} else if tRes.ExitCode != 0 {
		add("nginx_config", DiagFail, strings.TrimSpace(tRes.Stderr), "wd.diag.hint_nginx_config")
	} else {
		add("nginx_config", DiagOK, "nginx -t passed", "")
	}

	// Document root and index file present?
	indexFile := "index.html"
	if isLaravel || w.AppType == "php" {
		indexFile = "index.php"
	}
	indexPath := docRoot + "/" + indexFile
	if res, err := s.exec.RunSudo(ctx, "test", "-d", docRoot); err != nil {
		add("docroot", DiagFail, err.Error(), "wd.diag.hint_deploy")
	} else if res.ExitCode != 0 {
		add("docroot", DiagFail, docRoot+" does not exist", "wd.diag.hint_deploy")
	} else if res, err := s.exec.RunSudo(ctx, "test", "-f", indexPath); err != nil {
		add("docroot", DiagFail, err.Error(), "wd.diag.hint_deploy")
	} else if res.ExitCode != 0 {
		add("docroot", DiagFail, indexPath+" does not exist", "wd.diag.hint_deploy")
	} else {
		add("docroot", DiagOK, indexPath, "")
	}

	// Can the nginx worker account actually traverse to and read the entry
	// point? This reproduces "stat() ... failed (13: Permission denied)" —
	// the cause of nginx's "File not found".
	accessStatus, accessDetail, accessHint := s.checkNginxAccess(ctx, w, homeDir, docRoot, indexPath)
	add("nginx_access", accessStatus, accessDetail, accessHint)

	// End-to-end HTTP answer from the local nginx for this vhost.
	httpStatus, httpDetail, httpHint := s.checkHTTPResponse(ctx, w.Domain)
	add("http_response", httpStatus, httpDetail, httpHint)

	// PHP-FPM pool running?
	if w.AppType != "static" && w.PHPVersion != "" {
		fpmService := "php" + w.PHPVersion + "-fpm"
		poolPath := "/etc/php/" + w.PHPVersion + "/fpm/pool.d/" + w.Domain + ".conf"
		if res, err := s.exec.RunSudo(ctx, "systemctl", "is-active", fpmService); err != nil {
			add("php_fpm", DiagFail, err.Error(), "wd.diag.hint_fpm")
		} else if res.ExitCode != 0 || strings.TrimSpace(res.Stdout) != "active" {
			add("php_fpm", DiagFail, fpmService+" is "+strings.TrimSpace(res.Stdout+" "+res.Stderr), "wd.diag.hint_fpm")
		} else if res, err := s.exec.RunSudo(ctx, "test", "-f", poolPath); err != nil || res.ExitCode != 0 {
			add("php_fpm", DiagWarn, poolPath+" is missing", "wd.diag.hint_fpm_pool")
		} else {
			add("php_fpm", DiagOK, fpmService+" active, pool "+poolPath, "")
		}
	}

	// Laravel project sanity: artisan, .env, public/storage symlink.
	if isLaravel {
		artisan := homeDir + "/app/artisan"
		if res, err := s.exec.RunSudo(ctx, "test", "-f", artisan); err != nil || res.ExitCode != 0 {
			add("laravel_project", DiagFail, artisan+" does not exist", "wd.diag.hint_deploy")
		} else if res, err := s.exec.RunSudo(ctx, "test", "-f", homeDir+"/app/.env"); err != nil || res.ExitCode != 0 {
			add("laravel_project", DiagWarn, homeDir+"/app/.env does not exist", "wd.diag.hint_env")
		} else if res, err := s.exec.RunSudo(ctx, "test", "-L", docRoot+"/storage"); err != nil || res.ExitCode != 0 {
			add("laravel_project", DiagWarn, docRoot+"/storage symlink is missing (uploaded files 404)", "wd.diag.hint_storage_link")
		} else {
			add("laravel_project", DiagOK, "artisan, .env, and public/storage symlink present", "")
		}
	}

	report := DiagnosticsReport{Checks: checks}

	// Recent site error log: it names the exact failing path and errno.
	if res, err := s.exec.RunSudo(ctx, "tail", "-n", "20", logDir+"/error.log"); err == nil && res.ExitCode == 0 {
		logText := strings.TrimSpace(res.Stdout)
		if len(logText) > 4096 {
			logText = logText[len(logText)-4096:]
		}
		report.ErrorLog = logText
	}
	return report
}

// checkNginxAccess walks every path component of the document root and
// verifies the nginx worker account can traverse it and read the entry file.
// All findings collapse into one check; the first blocker is reported.
func (s *Service) checkNginxAccess(ctx context.Context, w model.Website, homeDir, docRoot, indexPath string) (string, string, string) {
	user := nginxWorkerUser(ctx, s.exec)

	components := []string{"/home", homeDir}
	if rel, err := filepath.Rel(homeDir, docRoot); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		current := homeDir
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			components = append(components, current)
		}
	}
	for _, dir := range components {
		res, err := s.exec.RunSudo(ctx, "-u", user, "--", "/usr/bin/test", "-x", dir)
		if err != nil {
			return DiagFail, dir + ": " + err.Error(), "wd.diag.hint_permissions"
		}
		if res.ExitCode != 0 {
			return DiagFail, fmt.Sprintf("%s cannot enter %s (permission denied) — nginx answers \"File not found\"", user, dir), "wd.diag.hint_permissions"
		}
	}
	if res, err := s.exec.RunSudo(ctx, "-u", user, "--", "/usr/bin/test", "-r", indexPath); err == nil && res.ExitCode != 0 {
		return DiagFail, fmt.Sprintf("%s cannot read %s", user, indexPath), "wd.diag.hint_permissions"
	}
	return DiagOK, user + " can traverse " + docRoot, ""
}

// checkHTTPResponse asks the local nginx for the site over HTTP(S) with DNS
// pinned to 127.0.0.1, so CDN/DNS issues cannot mask the server's answer.
func (s *Service) checkHTTPResponse(ctx context.Context, domain string) (string, string, string) {
	res, err := s.exec.RunSudo(ctx, "curl", "-skL",
		"--max-time", "10",
		"--resolve", domain+":80:127.0.0.1",
		"--resolve", domain+":443:127.0.0.1",
		"-o", "/dev/null", "-w", "%{http_code}",
		"http://"+domain+"/")
	if err != nil {
		return DiagFail, err.Error(), "wd.diag.hint_nginx_down"
	}
	code := strings.TrimSpace(res.Stdout)
	switch {
	case code == "" || code == "000":
		return DiagFail, "no response from local nginx (connection refused or timeout)", "wd.diag.hint_nginx_down"
	case code == "403" || code == "404":
		return DiagFail, "local nginx answered HTTP " + code, "wd.diag.hint_permissions"
	case strings.HasPrefix(code, "5"):
		return DiagWarn, "local nginx answered HTTP " + code + " (application error)", "wd.diag.hint_app_error"
	default:
		return DiagOK, "local nginx answered HTTP " + code, ""
	}
}
