package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/api"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/firewall"
	"github.com/mohammadirham37/jenderal_panel/internal/logging"
	"github.com/mohammadirham37/jenderal_panel/internal/nginx"
	"github.com/mohammadirham37/jenderal_panel/internal/php"
	"github.com/mohammadirham37/jenderal_panel/internal/process"
	"github.com/mohammadirham37/jenderal_panel/internal/server"
	"github.com/mohammadirham37/jenderal_panel/internal/website"
	"github.com/mohammadirham37/jenderal_panel/internal/service"
	"github.com/mohammadirham37/jenderal_panel/internal/settings"
	"github.com/mohammadirham37/jenderal_panel/internal/system"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe()
	case "migrate":
		cmdMigrate()
	case "admin":
		if len(os.Args) < 3 || os.Args[2] != "create" {
			fmt.Println("Usage: jenderal admin create")
			os.Exit(1)
		}
		cmdAdminCreate()
	case "version":
		fmt.Printf("Jenderal Panel %s\n", version)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`Jenderal Panel %s

Usage:
  jenderal serve              Start the HTTP server
  jenderal migrate            Run database migrations
  jenderal admin create       Create admin user
  jenderal version            Print version
`, version)
}

func getConfigPath() string {
	for i, arg := range os.Args {
		if arg == "--config" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return "/etc/jenderal/jenderal.yaml"
}

func loadConfig() *config.Config {
	cfg, err := config.Load(getConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

func openDB(cfg *config.Config) *sql.DB {
	db, err := database.Open(cfg.Database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	return db
}

func cmdServe() {
	cfg := loadConfig()
	logger := logging.New(cfg.Logging)

	db := openDB(cfg)
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}

	exec := executor.NewExecutor(30 * time.Second)
	serviceMgr := service.NewSystemd(exec, cfg.Services.Allowed)
	auditSvc := audit.NewService(db)
	authSvc := auth.NewService(db, cfg.Auth)
	rbac := auth.NewRBAC(db)
	systemInfo := system.NewInfo(exec)
	metricsCollector := system.NewMetricsCollector(db, cfg.Metrics)
	settingsSvc := settings.NewService(db)
	nginxSvc := nginx.NewService(exec, auditSvc)
	firewallSvc := firewall.NewService(exec, auditSvc)
	processSvc := process.NewService(exec)
	logSvc := system.NewLogService(exec)
	phpSvc := php.NewService(exec, auditSvc)
	websiteSvc := website.NewService(db, exec, auditSvc)
	provisioner := website.NewProvisioner(db, exec, auditSvc)
	websiteSvc.SetProvisioner(provisioner)

	if err := rbac.Seed(context.Background()); err != nil {
		logger.Error("RBAC seed failed", "error", err)
		os.Exit(1)
	}

	router := api.NewRouter(api.Dependencies{
		Logger:        logger,
		AuthSvc:       authSvc,
		RBAC:          rbac,
		AuditSvc:      auditSvc,
		SystemInfo:    systemInfo,
		Metrics:       metricsCollector,
		ServiceMgr:    serviceMgr,
		SettingsSvc:   settingsSvc,
		NginxSvc:      nginxSvc,
		FirewallSvc:   firewallSvc,
		ProcessSvc:    processSvc,
		LogSvc:        logSvc,
		WebsiteSvc:    websiteSvc,
		PHPSvc:        phpSvc,
		StaticHandler: staticHandler(),
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	metricsCollector.Start(ctx)
	provisioner.Start(ctx)

	srv := server.New(cfg.Server, router, logger)
	if err := server.ListenAndServe(ctx, srv, cfg.Server, logger); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func cmdMigrate() {
	cfg := loadConfig()
	db := openDB(cfg)
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Migrations completed successfully.")
}

func cmdAdminCreate() {
	cfg := loadConfig()
	db := openDB(cfg)
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
		os.Exit(1)
	}

	rbac := auth.NewRBAC(db)
	rbac.Seed(context.Background())

	authSvc := auth.NewService(db, cfg.Auth)

	var username, email, password string
	fmt.Print("Username: ")
	fmt.Scanln(&username)
	fmt.Print("Email: ")
	fmt.Scanln(&email)
	fmt.Print("Password: ")
	fmt.Scanln(&password)

	if username == "" || email == "" || password == "" {
		fmt.Fprintln(os.Stderr, "All fields required.")
		os.Exit(1)
	}

	user, err := authSvc.CreateUser(context.Background(), username, email, password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating user: %v\n", err)
		os.Exit(1)
	}

	if err := rbac.AssignRole(context.Background(), user.ID, "admin"); err != nil {
		fmt.Fprintf(os.Stderr, "Error assigning role: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Admin user '%s' created successfully.\n", username)
}
