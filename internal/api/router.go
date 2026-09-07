package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/cron"
	"github.com/mohammadirham37/jenderal_panel/internal/deployment"
	"github.com/mohammadirham37/jenderal_panel/internal/firewall"
	"github.com/mohammadirham37/jenderal_panel/internal/nodejs"
	"github.com/mohammadirham37/jenderal_panel/internal/nginx"
	"github.com/mohammadirham37/jenderal_panel/internal/php"
	"github.com/mohammadirham37/jenderal_panel/internal/process"
	"github.com/mohammadirham37/jenderal_panel/internal/queue"
	"github.com/mohammadirham37/jenderal_panel/internal/service"
	"github.com/mohammadirham37/jenderal_panel/internal/ssl"
	"github.com/mohammadirham37/jenderal_panel/internal/website"
	"github.com/mohammadirham37/jenderal_panel/internal/settings"
	"github.com/mohammadirham37/jenderal_panel/internal/system"
	"github.com/mohammadirham37/jenderal_panel/internal/user"
)

type Dependencies struct {
	Logger        *slog.Logger
	AuthSvc       *auth.Service
	RBAC          *auth.RBAC
	AuditSvc      *audit.Service
	SystemInfo    *system.Info
	Metrics       *system.MetricsCollector
	ServiceMgr    service.ServiceManager
	SettingsSvc   *settings.Service
	NginxSvc      *nginx.Service
	FirewallSvc   *firewall.Service
	ProcessSvc    *process.Service
	LogSvc        *system.LogService
	WebsiteSvc    *website.Service
	PHPSvc        *php.Service
	SSLSvc         *ssl.Service
	DeploymentSvc  *deployment.Service
	CronSvc        *cron.Service
	QueueSvc       *queue.Service
	NodeSvc        *nodejs.Service
	StaticHandler  http.Handler
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RealIP)
	r.Use(RequestIDMiddleware)
	r.Use(RecovererMiddleware)
	r.Use(LoggingMiddleware(deps.Logger))

	// Handlers
	authHandler := auth.NewHandler(deps.AuthSvc, deps.RBAC, deps.AuditSvc)
	systemHandler := system.NewHandler(deps.SystemInfo, deps.Metrics, deps.AuditSvc, deps.LogSvc)
	serviceHandler := service.NewHandler(deps.ServiceMgr, deps.AuditSvc)
	userHandler := user.NewHandler(deps.AuthSvc, deps.RBAC, deps.AuditSvc)
	auditHandler := audit.NewHandler(deps.AuditSvc)
	settingsHandler := settings.NewHandler(deps.SettingsSvc)
	nginxHandler := nginx.NewHandler(deps.NginxSvc, deps.AuditSvc)
	firewallHandler := firewall.NewHandler(deps.FirewallSvc, deps.AuditSvc)
	processHandler := process.NewHandler(deps.ProcessSvc, deps.AuditSvc)
	websiteHandler := website.NewHandler(deps.WebsiteSvc, deps.AuditSvc)
	phpHandler := php.NewHandler(deps.PHPSvc, deps.AuditSvc)
	sslHandler := ssl.NewHandler(deps.SSLSvc, deps.AuditSvc)
	deployHandler := deployment.NewHandler(deps.DeploymentSvc, deps.AuditSvc)
	cronHandler := cron.NewHandler(deps.CronSvc, deps.AuditSvc)
	queueHandler := queue.NewHandler(deps.QueueSvc, deps.AuditSvc)
	nodeHandler := nodejs.NewHandler(deps.NodeSvc, deps.AuditSvc)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Public
		r.Post("/auth/login", authHandler.Login)

		// Authenticated
		r.Group(func(r chi.Router) {
			r.Use(auth.SessionMiddleware(deps.AuthSvc))
			r.Use(auth.CSRFMiddleware)

			r.Post("/auth/logout", authHandler.Logout)
			r.Get("/auth/me", authHandler.Me)

			// Dashboard
			r.Get("/dashboard", systemHandler.Dashboard)
			r.Get("/dashboard/metrics", systemHandler.DashboardMetrics)

			// Server
			r.Get("/server/info", systemHandler.GetInfo)
			r.With(auth.RequirePermission(deps.RBAC, "server.reboot")).
				Post("/server/reboot", systemHandler.Reboot)
			r.With(auth.RequirePermission(deps.RBAC, "server.hostname")).
				Post("/server/hostname", systemHandler.SetHostname)
			r.With(auth.RequirePermission(deps.RBAC, "server.timezone")).
				Post("/server/timezone", systemHandler.SetTimezone)

			// Services
			r.Get("/services", serviceHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "services.manage")).
				Post("/services/{name}/start", serviceHandler.Start)
			r.With(auth.RequirePermission(deps.RBAC, "services.manage")).
				Post("/services/{name}/stop", serviceHandler.Stop)
			r.With(auth.RequirePermission(deps.RBAC, "services.manage")).
				Post("/services/{name}/restart", serviceHandler.Restart)
			r.With(auth.RequirePermission(deps.RBAC, "services.manage")).
				Post("/services/{name}/reload", serviceHandler.Reload)

			// Users
			r.With(auth.RequirePermission(deps.RBAC, "users.view")).
				Get("/users", userHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "users.create")).
				Post("/users", userHandler.Create)
			r.With(auth.RequirePermission(deps.RBAC, "users.view")).
				Get("/users/{id}", userHandler.Get)
			r.With(auth.RequirePermission(deps.RBAC, "users.update")).
				Put("/users/{id}", userHandler.Update)
			r.With(auth.RequirePermission(deps.RBAC, "users.update")).
				Put("/users/{id}/password", userHandler.UpdatePassword)
			r.With(auth.RequirePermission(deps.RBAC, "users.delete")).
				Delete("/users/{id}", userHandler.Delete)

			// Audit logs
			r.With(auth.RequirePermission(deps.RBAC, "audit.view")).
				Get("/audit-logs", auditHandler.List)

			// Settings
			r.With(auth.RequirePermission(deps.RBAC, "settings.view")).
				Get("/settings", settingsHandler.Get)
			r.With(auth.RequirePermission(deps.RBAC, "settings.update")).
				Put("/settings", settingsHandler.Update)

			// Nginx
			r.With(auth.RequirePermission(deps.RBAC, "nginx.view")).
				Get("/nginx/status", nginxHandler.Status)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.manage")).
				Post("/nginx/install", nginxHandler.Install)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.manage")).
				Post("/nginx/start", nginxHandler.Start)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.manage")).
				Post("/nginx/stop", nginxHandler.Stop)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.manage")).
				Post("/nginx/restart", nginxHandler.Restart)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.manage")).
				Post("/nginx/reload", nginxHandler.Reload)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.manage")).
				Post("/nginx/test", nginxHandler.TestConfig)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.view")).
				Get("/nginx/config", nginxHandler.GetConfig)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.config")).
				Put("/nginx/config", nginxHandler.SaveConfig)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.view")).
				Get("/nginx/sites", nginxHandler.ListSites)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.view")).
				Get("/nginx/sites/{name}", nginxHandler.GetSiteConfig)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.config")).
				Put("/nginx/sites/{name}", nginxHandler.SaveSiteConfig)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.manage")).
				Post("/nginx/sites/{name}/enable", nginxHandler.EnableSite)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.manage")).
				Post("/nginx/sites/{name}/disable", nginxHandler.DisableSite)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.manage")).
				Delete("/nginx/sites/{name}", nginxHandler.DeleteSite)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.view")).
				Get("/nginx/logs/access", nginxHandler.AccessLog)
			r.With(auth.RequirePermission(deps.RBAC, "nginx.view")).
				Get("/nginx/logs/error", nginxHandler.ErrorLog)

			// Firewall
			r.With(auth.RequirePermission(deps.RBAC, "firewall.view")).
				Get("/firewall/status", firewallHandler.Status)
			r.With(auth.RequirePermission(deps.RBAC, "firewall.manage")).
				Post("/firewall/enable", firewallHandler.Enable)
			r.With(auth.RequirePermission(deps.RBAC, "firewall.manage")).
				Post("/firewall/disable", firewallHandler.Disable)
			r.With(auth.RequirePermission(deps.RBAC, "firewall.view")).
				Get("/firewall/rules", firewallHandler.ListRules)
			r.With(auth.RequirePermission(deps.RBAC, "firewall.manage")).
				Post("/firewall/rules", firewallHandler.AddRule)
			r.With(auth.RequirePermission(deps.RBAC, "firewall.manage")).
				Delete("/firewall/rules/{number}", firewallHandler.DeleteRule)

			// Processes
			r.With(auth.RequirePermission(deps.RBAC, "processes.view")).
				Get("/processes", processHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "processes.kill")).
				Post("/processes/{pid}/kill", processHandler.Kill)

			// Logs
			r.With(auth.RequirePermission(deps.RBAC, "logs.view")).
				Get("/logs", systemHandler.ReadLog)

			// Websites
			r.With(auth.RequirePermission(deps.RBAC, "websites.view")).
				Get("/websites", websiteHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "websites.create")).
				Post("/websites", websiteHandler.Create)
			r.With(auth.RequirePermission(deps.RBAC, "websites.view")).
				Get("/websites/{id}", websiteHandler.Get)
			r.With(auth.RequirePermission(deps.RBAC, "websites.update")).
				Put("/websites/{id}", websiteHandler.Update)
			r.With(auth.RequirePermission(deps.RBAC, "websites.delete")).
				Delete("/websites/{id}", websiteHandler.Delete)
			r.With(auth.RequirePermission(deps.RBAC, "websites.suspend")).
				Post("/websites/{id}/suspend", websiteHandler.Suspend)
			r.With(auth.RequirePermission(deps.RBAC, "websites.update")).
				Post("/websites/{id}/enable", websiteHandler.Enable)
			r.With(auth.RequirePermission(deps.RBAC, "websites.update")).
				Post("/websites/{id}/retry", websiteHandler.Retry)
			r.With(auth.RequirePermission(deps.RBAC, "websites.view")).
				Get("/websites/{id}/config", websiteHandler.GetConfig)
			r.With(auth.RequirePermission(deps.RBAC, "websites.update")).
				Put("/websites/{id}/config", websiteHandler.SaveConfig)
			r.With(auth.RequirePermission(deps.RBAC, "websites.view")).
				Get("/websites/{id}/logs/access", websiteHandler.AccessLog)
			r.With(auth.RequirePermission(deps.RBAC, "websites.view")).
				Get("/websites/{id}/logs/error", websiteHandler.ErrorLog)
			r.With(auth.RequirePermission(deps.RBAC, "websites.update")).
				Post("/websites/{id}/domains", websiteHandler.AddDomain)
			r.With(auth.RequirePermission(deps.RBAC, "websites.update")).
				Delete("/websites/{id}/domains/{did}", websiteHandler.RemoveDomain)

			// PHP
			r.With(auth.RequirePermission(deps.RBAC, "php.view")).
				Get("/php", phpHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "php.manage")).
				Post("/php/{version}/install", phpHandler.Install)
			r.With(auth.RequirePermission(deps.RBAC, "php.manage")).
				Post("/php/{version}/uninstall", phpHandler.Uninstall)
			r.With(auth.RequirePermission(deps.RBAC, "php.manage")).
				Post("/php/{version}/restart", phpHandler.Restart)
			r.With(auth.RequirePermission(deps.RBAC, "php.view")).
				Get("/php/{version}/config", phpHandler.GetConfig)
			r.With(auth.RequirePermission(deps.RBAC, "php.config")).
				Put("/php/{version}/config", phpHandler.SaveConfig)

			// SSL
			r.With(auth.RequirePermission(deps.RBAC, "ssl.manage")).
				Post("/ssl/issue", sslHandler.Issue)
			r.With(auth.RequirePermission(deps.RBAC, "ssl.view")).
				Get("/ssl", sslHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "ssl.view")).
				Get("/ssl/{id}", sslHandler.Get)
			r.With(auth.RequirePermission(deps.RBAC, "ssl.manage")).
				Post("/ssl/{id}/renew", sslHandler.Renew)
			r.With(auth.RequirePermission(deps.RBAC, "ssl.manage")).
				Post("/ssl/{id}/revoke", sslHandler.Revoke)
			r.With(auth.RequirePermission(deps.RBAC, "ssl.manage")).
				Delete("/ssl/{id}", sslHandler.Delete)

			// Deployments
			r.With(auth.RequirePermission(deps.RBAC, "deployments.deploy")).
				Post("/websites/{id}/deploy", deployHandler.Deploy)
			r.With(auth.RequirePermission(deps.RBAC, "deployments.view")).
				Get("/websites/{id}/deployments", deployHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "deployments.view")).
				Get("/deployments/{id}", deployHandler.Get)

			// Cron Jobs
			r.With(auth.RequirePermission(deps.RBAC, "cron.view")).
				Get("/cron-jobs", cronHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "cron.manage")).
				Post("/cron-jobs", cronHandler.Create)
			r.With(auth.RequirePermission(deps.RBAC, "cron.view")).
				Get("/cron-jobs/{id}", cronHandler.Get)
			r.With(auth.RequirePermission(deps.RBAC, "cron.manage")).
				Put("/cron-jobs/{id}", cronHandler.Update)
			r.With(auth.RequirePermission(deps.RBAC, "cron.manage")).
				Delete("/cron-jobs/{id}", cronHandler.Delete)
			r.With(auth.RequirePermission(deps.RBAC, "cron.manage")).
				Post("/cron-jobs/{id}/enable", cronHandler.Enable)
			r.With(auth.RequirePermission(deps.RBAC, "cron.manage")).
				Post("/cron-jobs/{id}/disable", cronHandler.Disable)

			// Queue Workers
			r.With(auth.RequirePermission(deps.RBAC, "queue.view")).
				Get("/queue-workers", queueHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "queue.manage")).
				Post("/queue-workers", queueHandler.Create)
			r.With(auth.RequirePermission(deps.RBAC, "queue.view")).
				Get("/queue-workers/{id}", queueHandler.Get)
			r.With(auth.RequirePermission(deps.RBAC, "queue.manage")).
				Delete("/queue-workers/{id}", queueHandler.Delete)
			r.With(auth.RequirePermission(deps.RBAC, "queue.manage")).
				Post("/queue-workers/{id}/start", queueHandler.Start)
			r.With(auth.RequirePermission(deps.RBAC, "queue.manage")).
				Post("/queue-workers/{id}/stop", queueHandler.Stop)
			r.With(auth.RequirePermission(deps.RBAC, "queue.manage")).
				Post("/queue-workers/{id}/restart", queueHandler.Restart)

			// Node.js
			r.With(auth.RequirePermission(deps.RBAC, "nodejs.view")).
				Get("/nodejs/versions", nodeHandler.ListVersions)
			r.With(auth.RequirePermission(deps.RBAC, "nodejs.manage")).
				Post("/nodejs/install", nodeHandler.Install)
			r.With(auth.RequirePermission(deps.RBAC, "nodejs.view")).
				Get("/nodejs/apps", nodeHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "nodejs.manage")).
				Post("/nodejs/apps", nodeHandler.CreateApp)
			r.With(auth.RequirePermission(deps.RBAC, "nodejs.view")).
				Get("/nodejs/apps/{id}", nodeHandler.Get)
			r.With(auth.RequirePermission(deps.RBAC, "nodejs.manage")).
				Delete("/nodejs/apps/{id}", nodeHandler.Delete)
			r.With(auth.RequirePermission(deps.RBAC, "nodejs.manage")).
				Post("/nodejs/apps/{id}/start", nodeHandler.Start)
			r.With(auth.RequirePermission(deps.RBAC, "nodejs.manage")).
				Post("/nodejs/apps/{id}/stop", nodeHandler.Stop)
			r.With(auth.RequirePermission(deps.RBAC, "nodejs.manage")).
				Post("/nodejs/apps/{id}/restart", nodeHandler.Restart)
		})
	})

	// WebSocket (auth via session middleware, no CSRF needed)
	r.Group(func(r chi.Router) {
		r.Use(auth.SessionMiddleware(deps.AuthSvc))
		r.Get("/ws/metrics", systemHandler.WSMetrics)
		r.Get("/ws/logs", systemHandler.StreamLog)
	})

	// Static files (SPA frontend)
	if deps.StaticHandler != nil {
		r.Handle("/*", deps.StaticHandler)
	}

	return r
}
