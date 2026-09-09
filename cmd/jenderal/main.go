package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/alert"
	"github.com/mohammadirham37/jenderal_panel/internal/api"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/backup"
	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/cron"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/dbmanager"
	"github.com/mohammadirham37/jenderal_panel/internal/deployment"
	"github.com/mohammadirham37/jenderal_panel/internal/docker"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/filemanager"
	"github.com/mohammadirham37/jenderal_panel/internal/firewall"
	"github.com/mohammadirham37/jenderal_panel/internal/logging"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/nginx"
	"github.com/mohammadirham37/jenderal_panel/internal/nodejs"
	"github.com/mohammadirham37/jenderal_panel/internal/notification"
	"github.com/mohammadirham37/jenderal_panel/internal/php"
	"github.com/mohammadirham37/jenderal_panel/internal/process"
	"github.com/mohammadirham37/jenderal_panel/internal/queue"
	"github.com/mohammadirham37/jenderal_panel/internal/server"
	"github.com/mohammadirham37/jenderal_panel/internal/service"
	"github.com/mohammadirham37/jenderal_panel/internal/settings"
	"github.com/mohammadirham37/jenderal_panel/internal/ssl"
	"github.com/mohammadirham37/jenderal_panel/internal/system"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
	"github.com/mohammadirham37/jenderal_panel/internal/update"
	"github.com/mohammadirham37/jenderal_panel/internal/website"
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
		fmt.Printf("Jenderal Panel %s\n", buildVersion())
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
`, buildVersion())
}

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	return resolveVersion(version, info.Settings)
}

func resolveVersion(linkedVersion string, settings []debug.BuildSetting) string {
	for _, setting := range settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			return setting.Value
		}
	}
	return linkedVersion
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
	var acmeEmail string
	_ = db.QueryRow(`SELECT email FROM users WHERE is_active = 1 ORDER BY created_at LIMIT 1`).Scan(&acmeEmail)
	acmeClient := ssl.NewLegoClient(acmeEmail, "/var/lib/jenderal/acme")
	sslSvc := ssl.NewService(db, exec, auditSvc, acmeClient, "/etc/jenderal/ssl")
	renewalWorker := ssl.NewRenewalWorker(sslSvc)
	deploySvc := deployment.NewService(db, exec, auditSvc)
	cronSvc := cron.NewService(db, exec, auditSvc)
	queueSvc := queue.NewService(db, exec, auditSvc)
	nodeSvc := nodejs.NewService(db, exec, auditSvc)
	dbManagerSvc := dbmanager.NewService(db, exec, auditSvc)
	dockerSvc := docker.NewService(exec, auditSvc)
	backupSvc := backup.NewService(db, exec, auditSvc, "/var/lib/jenderal/backups")
	backupScheduler := backup.NewScheduler(backupSvc)
	alertSvc := alert.NewService(db, auditSvc)
	alertSvc.SetTargetProviders(serviceMgr, sslSvc)
	notifSvc := notification.NewService(db)
	alertChecker := alert.NewChecker(alertSvc, notifSvc, func() model.ServerMetrics {
		return metricsCollector.Buffer().Latest()
	}, serviceMgr, sslSvc)
	fileManagerSvc := filemanager.NewService(exec, auditSvc)
	tasks := taskrunner.New()
	updateSvc := update.NewService(exec, buildVersion(), tasks)
	phpSvc := php.NewService(exec, auditSvc)
	websiteSvc := website.NewService(db, exec, auditSvc)
	provisioner := website.NewProvisioner(db, exec, auditSvc)
	websiteSvc.SetProvisioner(provisioner)

	if err := rbac.Seed(context.Background()); err != nil {
		logger.Error("RBAC seed failed", "error", err)
		os.Exit(1)
	}

	router := api.NewRouter(api.Dependencies{
		Logger:         logger,
		AuthSvc:        authSvc,
		RBAC:           rbac,
		AuditSvc:       auditSvc,
		SystemInfo:     systemInfo,
		Metrics:        metricsCollector,
		ServiceMgr:     serviceMgr,
		SettingsSvc:    settingsSvc,
		NginxSvc:       nginxSvc,
		FirewallSvc:    firewallSvc,
		ProcessSvc:     processSvc,
		LogSvc:         logSvc,
		WebsiteSvc:     websiteSvc,
		PHPSvc:         phpSvc,
		SSLSvc:         sslSvc,
		DeploymentSvc:  deploySvc,
		CronSvc:        cronSvc,
		QueueSvc:       queueSvc,
		NodeSvc:        nodeSvc,
		DBManagerSvc:   dbManagerSvc,
		DockerSvc:      dockerSvc,
		BackupSvc:      backupSvc,
		AlertSvc:       alertSvc,
		NotifSvc:       notifSvc,
		FileManagerSvc: fileManagerSvc,
		UpdateSvc:      updateSvc,
		Exec:           exec,
		Tasks:          tasks,
		DB:             db,
		StaticHandler:  staticHandler(),
	})

	// Background workers use a context that cancels on process exit
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	metricsCollector.Start(bgCtx)
	provisioner.Start(bgCtx)
	renewalWorker.Start(bgCtx)
	deploySvc.Start(bgCtx)
	backupScheduler.Start(bgCtx)
	alertChecker.Start(bgCtx)

	// Server handles its own signal catching — blocks until shutdown
	if err := server.Run(cfg.Server, router, logger); err != nil {
		fmt.Fprintf(os.Stderr, "SERVER ERROR: %v\n", err)
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
