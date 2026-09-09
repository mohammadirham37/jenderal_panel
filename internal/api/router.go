package api

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/mohammadirham37/jenderal_panel/internal/alert"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/backup"
	"github.com/mohammadirham37/jenderal_panel/internal/cron"
	"github.com/mohammadirham37/jenderal_panel/internal/dbmanager"
	"github.com/mohammadirham37/jenderal_panel/internal/dependency"
	"github.com/mohammadirham37/jenderal_panel/internal/deployment"
	"github.com/mohammadirham37/jenderal_panel/internal/docker"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/filemanager"
	"github.com/mohammadirham37/jenderal_panel/internal/firewall"
	"github.com/mohammadirham37/jenderal_panel/internal/nginx"
	"github.com/mohammadirham37/jenderal_panel/internal/nodejs"
	"github.com/mohammadirham37/jenderal_panel/internal/notification"
	"github.com/mohammadirham37/jenderal_panel/internal/php"
	"github.com/mohammadirham37/jenderal_panel/internal/process"
	"github.com/mohammadirham37/jenderal_panel/internal/queue"
	"github.com/mohammadirham37/jenderal_panel/internal/service"
	"github.com/mohammadirham37/jenderal_panel/internal/settings"
	"github.com/mohammadirham37/jenderal_panel/internal/ssl"
	"github.com/mohammadirham37/jenderal_panel/internal/system"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
	"github.com/mohammadirham37/jenderal_panel/internal/terminal"
	"github.com/mohammadirham37/jenderal_panel/internal/update"
	"github.com/mohammadirham37/jenderal_panel/internal/user"
	"github.com/mohammadirham37/jenderal_panel/internal/website"
)

type Dependencies struct {
	DB             *sql.DB
	Logger         *slog.Logger
	AuthSvc        *auth.Service
	RBAC           *auth.RBAC
	AuditSvc       *audit.Service
	SystemInfo     *system.Info
	Metrics        *system.MetricsCollector
	ServiceMgr     service.ServiceManager
	SettingsSvc    *settings.Service
	NginxSvc       *nginx.Service
	FirewallSvc    *firewall.Service
	ProcessSvc     *process.Service
	LogSvc         *system.LogService
	WebsiteSvc     *website.Service
	PHPSvc         *php.Service
	SSLSvc         *ssl.Service
	DeploymentSvc  *deployment.Service
	DependencySvc  *dependency.Service
	CronSvc        *cron.Service
	QueueSvc       *queue.Service
	NodeSvc        *nodejs.Service
	DBManagerSvc   *dbmanager.Service
	DockerSvc      *docker.Service
	BackupSvc      *backup.Service
	AlertSvc       *alert.Service
	NotifSvc       *notification.Service
	FileManagerSvc *filemanager.Service
	UpdateSvc      *update.Service
	Exec           executor.CommandExecutor
	Tasks          *taskrunner.Runner
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
	dependencyHandler := dependency.NewHandler(deps.DependencySvc, deps.AuditSvc, deps.Tasks)
	userHandler := user.NewHandler(deps.AuthSvc, deps.RBAC, deps.AuditSvc)
	auditHandler := audit.NewHandler(deps.AuditSvc)
	settingsHandler := settings.NewHandler(deps.SettingsSvc)
	nginxHandler := nginx.NewHandler(deps.NginxSvc, deps.AuditSvc)
	firewallHandler := firewall.NewHandler(deps.FirewallSvc, deps.AuditSvc)
	processHandler := process.NewHandler(deps.ProcessSvc, deps.AuditSvc)
	websiteHandler := website.NewHandler(deps.WebsiteSvc, deps.AuditSvc)
	phpHandler := php.NewHandler(deps.PHPSvc, deps.AuditSvc, deps.Tasks)
	sslHandler := ssl.NewHandler(deps.SSLSvc, deps.AuditSvc)
	deployHandler := deployment.NewHandler(deps.DeploymentSvc, deps.AuditSvc)
	cronHandler := cron.NewHandler(deps.CronSvc, deps.AuditSvc)
	queueHandler := queue.NewHandler(deps.QueueSvc, deps.AuditSvc)
	nodeHandler := nodejs.NewHandler(deps.NodeSvc, deps.AuditSvc, deps.Tasks)
	taskHandler := taskrunner.NewHandler(deps.Tasks)
	dbHandler := dbmanager.NewHandler(deps.DBManagerSvc, deps.AuditSvc, deps.Tasks)
	dockerHandler := docker.NewHandler(deps.DockerSvc, deps.AuditSvc, deps.Tasks)
	backupHandler := backup.NewHandler(deps.BackupSvc, deps.AuditSvc)
	alertHandler := alert.NewHandler(deps.AlertSvc, deps.AuditSvc)
	notifHandler := notification.NewHandler(deps.NotifSvc, deps.AuditSvc)
	fileHandler := filemanager.NewHandler(deps.FileManagerSvc, deps.DB, deps.AuditSvc)
	terminalHandler := terminal.NewHandler(deps.Exec, deps.AuditSvc)
	updateHandler := update.NewHandler(deps.UpdateSvc, deps.AuditSvc)

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
			r.Get("/services/dependencies", dependencyHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "services.manage")).
				Post("/services/composer/install", dependencyHandler.InstallComposer)
			r.With(auth.RequirePermission(deps.RBAC, "services.manage")).
				Post("/services/composer/update", dependencyHandler.UpdateComposer)
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
				Get("/websites/options", websiteHandler.Options)
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
			r.With(auth.RequirePermission(deps.RBAC, "ssl.manage")).
				Post("/ssl/custom", sslHandler.InstallCustom)
			r.With(auth.RequirePermission(deps.RBAC, "ssl.view")).
				Get("/ssl", sslHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "ssl.view")).
				Get("/ssl/{id}", sslHandler.Get)
			r.With(auth.RequirePermission(deps.RBAC, "ssl.manage")).
				Put("/ssl/{id}", sslHandler.Update)
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

			// Database Management
			r.With(auth.RequirePermission(deps.RBAC, "databases.view")).
				Get("/databases/engines", dbHandler.ListEngines)
			r.With(auth.RequirePermission(deps.RBAC, "databases.create")).
				Post("/databases/engines/{engine}/install", dbHandler.InstallEngine)
			r.With(auth.RequirePermission(deps.RBAC, "databases.create")).
				Post("/databases/engines/{engine}/start", dbHandler.StartEngine)
			r.With(auth.RequirePermission(deps.RBAC, "databases.create")).
				Post("/databases/engines/{engine}/stop", dbHandler.StopEngine)
			r.With(auth.RequirePermission(deps.RBAC, "databases.create")).
				Post("/databases/engines/{engine}/restart", dbHandler.RestartEngine)
			r.With(auth.RequirePermission(deps.RBAC, "databases.view")).
				Get("/databases", dbHandler.ListDatabases)
			r.With(auth.RequirePermission(deps.RBAC, "databases.create")).
				Post("/databases", dbHandler.CreateDatabase)
			r.With(auth.RequirePermission(deps.RBAC, "databases.delete")).
				Delete("/databases/{id}", dbHandler.DropDatabase)
			r.With(auth.RequirePermission(deps.RBAC, "databases.view")).
				Get("/databases/users", dbHandler.ListDBUsers)
			r.With(auth.RequirePermission(deps.RBAC, "databases.users")).
				Post("/databases/users", dbHandler.CreateDBUser)
			r.With(auth.RequirePermission(deps.RBAC, "databases.users")).
				Delete("/databases/users/{id}", dbHandler.DropDBUser)
			r.With(auth.RequirePermission(deps.RBAC, "databases.users")).
				Post("/databases/users/{id}/password", dbHandler.ResetPassword)
			r.With(auth.RequirePermission(deps.RBAC, "databases.users")).
				Post("/databases/users/{id}/grant", dbHandler.GrantPrivileges)

			// Docker
			r.With(auth.RequirePermission(deps.RBAC, "docker.view")).
				Get("/docker/status", dockerHandler.Status)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Post("/docker/install", dockerHandler.Install)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Post("/docker/start", dockerHandler.Start)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Post("/docker/stop", dockerHandler.Stop)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Post("/docker/restart", dockerHandler.Restart)
			r.With(auth.RequirePermission(deps.RBAC, "docker.view")).
				Get("/docker/containers", dockerHandler.ListContainers)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Post("/docker/containers/{id}/start", dockerHandler.StartContainer)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Post("/docker/containers/{id}/stop", dockerHandler.StopContainer)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Post("/docker/containers/{id}/restart", dockerHandler.RestartContainer)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Delete("/docker/containers/{id}", dockerHandler.RemoveContainer)
			r.With(auth.RequirePermission(deps.RBAC, "docker.view")).
				Get("/docker/containers/{id}/logs", dockerHandler.ContainerLogs)
			r.With(auth.RequirePermission(deps.RBAC, "docker.view")).
				Get("/docker/containers/{id}/inspect", dockerHandler.InspectContainer)
			r.With(auth.RequirePermission(deps.RBAC, "docker.view")).
				Get("/docker/images", dockerHandler.ListImages)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Post("/docker/images/pull", dockerHandler.PullImage)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Delete("/docker/images/{id}", dockerHandler.RemoveImage)
			r.With(auth.RequirePermission(deps.RBAC, "docker.view")).
				Get("/docker/volumes", dockerHandler.ListVolumes)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Post("/docker/volumes", dockerHandler.CreateVolume)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Delete("/docker/volumes/{name}", dockerHandler.RemoveVolume)
			r.With(auth.RequirePermission(deps.RBAC, "docker.view")).
				Get("/docker/networks", dockerHandler.ListNetworks)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Post("/docker/networks", dockerHandler.CreateNetwork)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Delete("/docker/networks/{name}", dockerHandler.RemoveNetwork)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Post("/docker/compose/up", dockerHandler.ComposeUp)
			r.With(auth.RequirePermission(deps.RBAC, "docker.manage")).
				Post("/docker/compose/down", dockerHandler.ComposeDown)
			r.With(auth.RequirePermission(deps.RBAC, "docker.view")).
				Get("/docker/compose/status", dockerHandler.ComposeStatus)

			// Backups
			r.With(auth.RequirePermission(deps.RBAC, "backups.create")).
				Post("/backups", backupHandler.CreateBackup)
			r.With(auth.RequirePermission(deps.RBAC, "backups.view")).
				Get("/backups", backupHandler.List)
			r.With(auth.RequirePermission(deps.RBAC, "backups.view")).
				Get("/backups/{id}", backupHandler.Get)
			r.With(auth.RequirePermission(deps.RBAC, "backups.delete")).
				Delete("/backups/{id}", backupHandler.Delete)
			r.With(auth.RequirePermission(deps.RBAC, "backups.restore")).
				Post("/backups/{id}/restore", backupHandler.Restore)
			r.With(auth.RequirePermission(deps.RBAC, "backups.view")).
				Get("/backup-schedules", backupHandler.ListSchedules)
			r.With(auth.RequirePermission(deps.RBAC, "backups.create")).
				Post("/backup-schedules", backupHandler.CreateSchedule)
			r.With(auth.RequirePermission(deps.RBAC, "backups.create")).
				Put("/backup-schedules/{id}", backupHandler.UpdateSchedule)
			r.With(auth.RequirePermission(deps.RBAC, "backups.delete")).
				Delete("/backup-schedules/{id}", backupHandler.DeleteSchedule)
			r.With(auth.RequirePermission(deps.RBAC, "backups.create")).
				Post("/backup-schedules/{id}/enable", backupHandler.EnableSchedule)
			r.With(auth.RequirePermission(deps.RBAC, "backups.create")).
				Post("/backup-schedules/{id}/disable", backupHandler.DisableSchedule)

			// Tasks (background operations)
			r.Get("/tasks", taskHandler.List)
			r.Get("/tasks/{id}", taskHandler.Get)

			// Alerts
			r.With(auth.RequirePermission(deps.RBAC, "alerts.view")).
				Get("/alert-rules", alertHandler.ListRules)
			r.With(auth.RequirePermission(deps.RBAC, "alerts.manage")).
				Post("/alert-rules", alertHandler.CreateRule)
			r.With(auth.RequirePermission(deps.RBAC, "alerts.view")).
				Get("/alert-rules/{id}", alertHandler.GetRule)
			r.With(auth.RequirePermission(deps.RBAC, "alerts.manage")).
				Put("/alert-rules/{id}", alertHandler.UpdateRule)
			r.With(auth.RequirePermission(deps.RBAC, "alerts.manage")).
				Delete("/alert-rules/{id}", alertHandler.DeleteRule)
			r.With(auth.RequirePermission(deps.RBAC, "alerts.view")).
				Get("/alert-history", alertHandler.ListHistory)
			r.With(auth.RequirePermission(deps.RBAC, "alerts.view")).
				Get("/alerts/rules", alertHandler.ListRules)
			r.With(auth.RequirePermission(deps.RBAC, "alerts.manage")).
				Post("/alerts/rules", alertHandler.CreateRule)
			r.With(auth.RequirePermission(deps.RBAC, "alerts.manage")).
				Put("/alerts/rules/{id}", alertHandler.UpdateRule)
			r.With(auth.RequirePermission(deps.RBAC, "alerts.manage")).
				Delete("/alerts/rules/{id}", alertHandler.DeleteRule)
			r.With(auth.RequirePermission(deps.RBAC, "alerts.view")).
				Get("/alerts/history", alertHandler.ListHistory)

			// Notifications
			r.With(auth.RequirePermission(deps.RBAC, "notifications.view")).
				Get("/notification-channels", notifHandler.ListChannels)
			r.With(auth.RequirePermission(deps.RBAC, "notifications.manage")).
				Post("/notification-channels", notifHandler.CreateChannel)
			r.With(auth.RequirePermission(deps.RBAC, "notifications.manage")).
				Put("/notification-channels/{id}", notifHandler.UpdateChannel)
			r.With(auth.RequirePermission(deps.RBAC, "notifications.manage")).
				Delete("/notification-channels/{id}", notifHandler.DeleteChannel)
			r.With(auth.RequirePermission(deps.RBAC, "notifications.manage")).
				Post("/notification-channels/{id}/test", notifHandler.TestChannel)
			r.With(auth.RequirePermission(deps.RBAC, "notifications.view")).
				Get("/notifications/channels", notifHandler.ListChannels)
			r.With(auth.RequirePermission(deps.RBAC, "notifications.manage")).
				Post("/notifications/channels", notifHandler.CreateChannel)
			r.With(auth.RequirePermission(deps.RBAC, "notifications.manage")).
				Put("/notifications/channels/{id}", notifHandler.UpdateChannel)
			r.With(auth.RequirePermission(deps.RBAC, "notifications.manage")).
				Delete("/notifications/channels/{id}", notifHandler.DeleteChannel)
			r.With(auth.RequirePermission(deps.RBAC, "notifications.manage")).
				Post("/notifications/channels/{id}/test", notifHandler.TestChannel)

			// TOTP
			r.Post("/auth/totp/setup", authHandler.TOTPSetup)
			r.Post("/auth/totp/enable", authHandler.TOTPEnable)
			r.Post("/auth/totp/disable", authHandler.TOTPDisable)

			// File Manager
			r.With(auth.RequirePermission(deps.RBAC, "files.view")).
				Get("/websites/{id}/files", fileHandler.Browse)
			r.With(auth.RequirePermission(deps.RBAC, "files.view")).
				Get("/websites/{id}/files/read", fileHandler.ReadFile)
			r.With(auth.RequirePermission(deps.RBAC, "files.manage")).
				Post("/websites/{id}/files/write", fileHandler.WriteFile)
			r.With(auth.RequirePermission(deps.RBAC, "files.manage")).
				Post("/websites/{id}/files/delete", fileHandler.DeleteFile)
			r.With(auth.RequirePermission(deps.RBAC, "files.manage")).
				Delete("/websites/{id}/files", fileHandler.DeleteFile)
			r.With(auth.RequirePermission(deps.RBAC, "files.manage")).
				Post("/websites/{id}/files/rename", fileHandler.Rename)
			r.With(auth.RequirePermission(deps.RBAC, "files.manage")).
				Post("/websites/{id}/files/mkdir", fileHandler.CreateDir)
			r.With(auth.RequirePermission(deps.RBAC, "files.manage")).
				Post("/websites/{id}/files/chmod", fileHandler.Chmod)
			r.With(auth.RequirePermission(deps.RBAC, "files.manage")).
				Post("/websites/{id}/files/upload", fileHandler.Upload)
			r.With(auth.RequirePermission(deps.RBAC, "files.view")).
				Get("/websites/{id}/files/download", fileHandler.Download)

			// Self-Update
			r.With(auth.RequirePermission(deps.RBAC, "update.view")).
				Get("/update/check", updateHandler.Check)
			r.With(auth.RequirePermission(deps.RBAC, "update.perform")).
				Post("/update/perform", updateHandler.Perform)
		})
	})

	// WebSocket (auth via session middleware, no CSRF needed)
	r.Group(func(r chi.Router) {
		r.Use(auth.SessionMiddleware(deps.AuthSvc))
		r.Get("/ws/metrics", systemHandler.WSMetrics)
		r.Get("/ws/logs", systemHandler.StreamLog)
		r.Get("/ws/terminal", terminalHandler.HandleWS)
	})

	// Static files (SPA frontend)
	if deps.StaticHandler != nil {
		r.Handle("/*", deps.StaticHandler)
	}

	return r
}
