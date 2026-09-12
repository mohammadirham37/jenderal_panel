package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
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
	"github.com/mohammadirham37/jenderal_panel/internal/dependency"
	"github.com/mohammadirham37/jenderal_panel/internal/deployment"
	"github.com/mohammadirham37/jenderal_panel/internal/docker"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/fail2ban"
	"github.com/mohammadirham37/jenderal_panel/internal/filemanager"
	"github.com/mohammadirham37/jenderal_panel/internal/firewall"
	"github.com/mohammadirham37/jenderal_panel/internal/frankenphp"
	"github.com/mohammadirham37/jenderal_panel/internal/goruntime"
	"github.com/mohammadirham37/jenderal_panel/internal/logging"
	"github.com/mohammadirham37/jenderal_panel/internal/malware"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/nginx"
	"github.com/mohammadirham37/jenderal_panel/internal/nodejs"
	"github.com/mohammadirham37/jenderal_panel/internal/notification"
	"github.com/mohammadirham37/jenderal_panel/internal/paneldomain"
	"github.com/mohammadirham37/jenderal_panel/internal/runtimebin"
	"github.com/mohammadirham37/jenderal_panel/internal/php"
	"github.com/mohammadirham37/jenderal_panel/internal/process"
	"github.com/mohammadirham37/jenderal_panel/internal/queue"
	"github.com/mohammadirham37/jenderal_panel/internal/security"
	"github.com/mohammadirham37/jenderal_panel/internal/server"
	"github.com/mohammadirham37/jenderal_panel/internal/service"
	"github.com/mohammadirham37/jenderal_panel/internal/settings"
	"github.com/mohammadirham37/jenderal_panel/internal/sshaccount"
	"github.com/mohammadirham37/jenderal_panel/internal/sshserver"
	"github.com/mohammadirham37/jenderal_panel/internal/ssl"
	"github.com/mohammadirham37/jenderal_panel/internal/system"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
	"github.com/mohammadirham37/jenderal_panel/internal/trafficguard"
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
	case "restart":
		if err := restartService(os.Geteuid(), runSystemCommand); err != nil {
			fmt.Fprintf(os.Stderr, "Restart failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Jenderal Panel restarted successfully.")
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
  jenderal restart            Restart the systemd service (requires root)
`, buildVersion())
}

type systemCommandRunner func(context.Context, string, ...string) ([]byte, error)

func runSystemCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func restartService(euid int, run systemCommandRunner) error {
	if euid != 0 {
		return fmt.Errorf("root privileges required; run: sudo /opt/jenderal/jenderal restart")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := run(ctx, "/usr/bin/systemctl", "restart", "jenderal.service")
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("systemctl restart jenderal.service: %s", detail)
	}

	output, err = run(ctx, "/usr/bin/systemctl", "is-active", "jenderal.service")
	status := strings.TrimSpace(string(output))
	if err != nil || status != "active" {
		if status == "" {
			status = "unknown"
		}
		return fmt.Errorf("jenderal.service is %s; inspect logs with: sudo journalctl -u jenderal -n 100 --no-pager", status)
	}

	return nil
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
	tasks, err := taskrunner.NewPersistent(db)
	if err != nil {
		logger.Error("initialize task runner failed", "error", err)
		os.Exit(1)
	}
	backupSvc.SetTaskRunner(tasks)
	securityEvents := security.NewEventService(db, notifSvc)
	fail2banSvc := fail2ban.NewService(exec, nil, db, securityEvents)
	malwareRepo := malware.NewRepository(db)
	if err := malwareRepo.RecoverInterruptedScans(context.Background(), time.Now().UTC()); err != nil {
		logger.Error("recover interrupted malware scans failed", "error", err)
		os.Exit(1)
	}
	malwareFiles := malware.NewSudoFileOperator(exec)
	malwareResolver := malware.NewTargetResolverWithFiles(db, malwareFiles)
	malwareSvc := malware.NewService(malwareRepo, malwareResolver, exec, malwareFiles, "/var/lib/jenderal/quarantine", securityEvents)
	malwareSvc.UseWalker(malware.NewSudoTargetWalker(exec))
	malwareScheduler := malware.NewScheduler(malwareRepo, tasks, malwareSvc, time.Local)
	trafficRepo := trafficguard.NewRepository(db)
	cloudflareUpdater := trafficguard.NewCloudflareUpdater(trafficRepo, nil, securityEvents)
	trafficNginx := trafficguard.NewNginxManager(exec, nil, trafficRepo)
	trafficSvc := trafficguard.NewService(db, trafficRepo, trafficNginx, cloudflareUpdater)
	trafficCollector := trafficguard.NewCollector(db, trafficRepo, exec, securityEvents)
	trafficWorker := trafficguard.NewWorker(trafficCollector, cloudflareUpdater)
	securitySvc := security.NewService(securityEvents, tasks, fail2banSvc, malwareSvc, trafficSvc)
	postureChecker := security.NewPostureChecker(exec)
	securitySvc.SetPostureChecker(postureChecker)
	securityWorker := security.NewWorker(securitySvc, securityEvents)
	updateSvc := update.NewService(exec, buildVersion(), tasks)
	dependencySvc := dependency.NewService(exec)
	phpSvc := php.NewService(exec, auditSvc)
	sshAccountSvc := sshaccount.NewService(db, exec, auditSvc)
	sshServerSvc := sshserver.NewService(db, exec, auditSvc, cfg.Server.Port)
	frankenphpSvc := frankenphp.NewService(exec, auditSvc)
	goRuntimeSvc := goruntime.NewService(exec, auditSvc)
	goRuntimeSvc.SetTaskRunner(tasks)
	runtimeBinSvc := runtimebin.NewService(exec, auditSvc)
	runtimeBinSvc.SetTaskRunner(tasks)
	websiteSvc := website.NewService(db, exec, auditSvc)
	provisioner := website.NewProvisioner(db, exec, auditSvc)
	websiteSvc.SetProvisioner(provisioner)
	websiteSvc.SetTaskRunner(tasks)
	panelDomainSvc := paneldomain.NewService(db, exec, auditSvc)
	panelDomainSvc.SetTaskRunner(tasks)
	healthChecker := website.NewHealthChecker(websiteSvc, func(message string) {
		if err := notifSvc.SendAll(context.Background(), message); err != nil {
			logger.Error("send health check notification failed", "error", err)
		}
	})
	frankenphpSvc.SetTaskRunner(tasks)
	securitySetup := security.NewSetupService(db, security.SetupActions{
		InstallFail2ban: fail2banSvc.InstallWithProgress,
		ConfigureFail2ban: func(ctx context.Context, cidrs []string, log func(string)) error {
			settings := fail2ban.SafeSettings()
			settings.IgnoreIPs = append(settings.IgnoreIPs, cidrs...)
			return fail2banSvc.ApplyWithProgress(ctx, settings, log)
		},
		InstallMalware:   malwareSvc.Install,
		UpdateSignatures: malwareSvc.UpdateSignatures,
		ScheduleMalware: func(ctx context.Context, localTime string) error {
			schedule := malware.Schedule{Enabled: true, LocalTime: localTime, Mode: malware.ScanModeQuick}
			existing, err := malwareRepo.ListSchedules(ctx)
			if err != nil {
				return err
			}
			if len(existing) > 0 {
				schedule.ID = existing[0].ID
				schedule.CreatedAt = existing[0].CreatedAt
			}
			_, err = malwareRepo.SaveSchedule(ctx, schedule)
			return err
		},
		ObserveTraffic: func(ctx context.Context, websiteIDs []string, log func(string)) error {
			for _, id := range websiteIDs {
				profile, err := trafficRepo.Profile(ctx, id, time.Now().UTC())
				if err != nil {
					return err
				}
				profile.Mode = "observe"
				if log != nil {
					log("Applying Observe Mode to website " + id + "\n")
				}
				if err := trafficSvc.Apply(ctx, id, profile, false); err != nil {
					return err
				}
			}
			return nil
		},
	}, securitySvc)

	if err := rbac.Seed(context.Background()); err != nil {
		logger.Error("RBAC seed failed", "error", err)
		os.Exit(1)
	}

	router := api.NewRouter(api.Dependencies{
		Logger:          logger,
		AuthSvc:         authSvc,
		RBAC:            rbac,
		AuditSvc:        auditSvc,
		SystemInfo:      systemInfo,
		Metrics:         metricsCollector,
		ServiceMgr:      serviceMgr,
		SettingsSvc:     settingsSvc,
		NginxSvc:        nginxSvc,
		FirewallSvc:     firewallSvc,
		ProcessSvc:      processSvc,
		LogSvc:          logSvc,
		WebsiteSvc:      websiteSvc,
		PHPSvc:          phpSvc,
		SSLSvc:          sslSvc,
		DeploymentSvc:   deploySvc,
		DependencySvc:   dependencySvc,
		CronSvc:         cronSvc,
		QueueSvc:        queueSvc,
		NodeSvc:         nodeSvc,
		DBManagerSvc:    dbManagerSvc,
		DockerSvc:       dockerSvc,
		BackupSvc:       backupSvc,
		AlertSvc:        alertSvc,
		NotifSvc:        notifSvc,
		FileManagerSvc:  fileManagerSvc,
		UpdateSvc:       updateSvc,
		Exec:            exec,
		Tasks:           tasks,
		SecuritySvc:     securitySvc,
		SecurityEvents:  securityEvents,
		Fail2banSvc:     fail2banSvc,
		MalwareSvc:      malwareSvc,
		MalwareRepo:     malwareRepo,
		TrafficGuardSvc: trafficSvc,
		SecuritySetup:   securitySetup,
		SSHAccountSvc:   sshAccountSvc,
		SSHServerSvc:    sshServerSvc,
		FrankenphpSvc:   frankenphpSvc,
		GoRuntimeSvc:    goRuntimeSvc,
		RuntimeBinSvc:   runtimeBinSvc,
		DB:              db,
		StaticHandler:   staticHandler(),
	})

	// Background workers use a context that cancels on process exit
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	metricsCollector.Start(bgCtx)
	if err := provisioner.RepairServingPermissions(bgCtx); err != nil {
		logger.Warn("website permission reconciliation failed", "error", err)
	}
	provisioner.Start(bgCtx)
	if err := provisioner.Recover(bgCtx); err != nil {
		logger.Warn("website provisioning recovery failed", "error", err)
	}
	renewalWorker.Start(bgCtx)
	deploySvc.Start(bgCtx)
	backupScheduler.Start(bgCtx)
	healthChecker.Start(bgCtx)
	panelDomainSvc.Start(bgCtx)
	alertChecker.Start(bgCtx)
	fail2banSvc.StartReconciler(bgCtx)
	malwareScheduler.Start(bgCtx)
	trafficWorker.Start(bgCtx)
	securityWorker.Start(bgCtx)

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
