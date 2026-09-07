package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/service"
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
	StaticHandler http.Handler
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
	systemHandler := system.NewHandler(deps.SystemInfo, deps.Metrics, deps.AuditSvc)
	serviceHandler := service.NewHandler(deps.ServiceMgr, deps.AuditSvc)
	userHandler := user.NewHandler(deps.AuthSvc, deps.RBAC, deps.AuditSvc)
	auditHandler := audit.NewHandler(deps.AuditSvc)
	settingsHandler := settings.NewHandler(deps.SettingsSvc)

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
		})
	})

	// WebSocket (auth via session middleware, no CSRF needed)
	r.Group(func(r chi.Router) {
		r.Use(auth.SessionMiddleware(deps.AuthSvc))
		r.Get("/ws/metrics", systemHandler.WSMetrics)
	})

	// Static files (SPA frontend)
	if deps.StaticHandler != nil {
		r.Handle("/*", deps.StaticHandler)
	}

	return r
}
