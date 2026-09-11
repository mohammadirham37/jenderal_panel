package model

import "time"

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Password  string    `json:"-"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Permission struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Module string `json:"module"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditEntry struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Action    string    `json:"action"`
	Module    string    `json:"module"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	IPAddress string    `json:"ip_address"`
	CreatedAt time.Time `json:"created_at"`
}

type Setting struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ServerMetrics struct {
	CPU       float64       `json:"cpu"`
	RAMUsed   uint64        `json:"ram_used"`
	RAMTotal  uint64        `json:"ram_total"`
	SwapUsed  uint64        `json:"swap_used"`
	SwapTotal uint64        `json:"swap_total"`
	DiskUsed  uint64        `json:"disk_used"`
	DiskTotal uint64        `json:"disk_total"`
	Load1     float64       `json:"load_1"`
	Load5     float64       `json:"load_5"`
	Load15    float64       `json:"load_15"`
	NetRx     uint64        `json:"net_rx"`
	NetTx     uint64        `json:"net_tx"`
	Uptime    time.Duration `json:"uptime"`
	Timestamp time.Time     `json:"timestamp"`
}

type ServiceStatus struct {
	Name      string        `json:"name"`
	Installed bool          `json:"installed"`
	Active    bool          `json:"active"`
	Running   bool          `json:"running"`
	Enabled   bool          `json:"enabled"`
	Uptime    time.Duration `json:"uptime"`
	PID       int           `json:"pid"`
}

type UserWithRoles struct {
	User        User         `json:"user"`
	Roles       []Role       `json:"roles"`
	Permissions []Permission `json:"permissions"`
}

type NginxStatus struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Enabled   bool   `json:"enabled"`
	Version   string `json:"version"`
	PID       int    `json:"pid"`
	ConfigOK  bool   `json:"config_ok"`
}

type SiteConfig struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
}

type FirewallStatus struct {
	Active  bool           `json:"active"`
	Default string         `json:"default"`
	Rules   []FirewallRule `json:"rules"`
}

type FirewallRule struct {
	Number  int    `json:"number"`
	To      string `json:"to"`
	Action  string `json:"action"`
	From    string `json:"from"`
	Comment string `json:"comment"`
}

type Process struct {
	PID     int     `json:"pid"`
	User    string  `json:"user"`
	CPU     float64 `json:"cpu"`
	RAM     float64 `json:"ram"`
	VSZ     uint64  `json:"vsz"`
	RSS     uint64  `json:"rss"`
	Command string  `json:"command"`
	Started string  `json:"started"`
}

type DiskPartition struct {
	Device     string `json:"device"`
	Mount      string `json:"mount"`
	Filesystem string `json:"filesystem"`
	Size       string `json:"size"`
	Used       string `json:"used"`
	Available  string `json:"available"`
	UsePct     string `json:"use_pct"`
}

type NetworkInterface struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	MAC  string `json:"mac"`
}

type Website struct {
	ID               string    `json:"id"`
	Domain           string    `json:"domain"`
	AppType          string    `json:"app_type"`
	PHPVersion       string    `json:"php_version"`
	NodeVersion      string    `json:"node_version"`
	DocumentRoot     string    `json:"document_root"`
	WebUser          string    `json:"web_user"`
	Status           string    `json:"status"`
	ErrorMessage     string    `json:"error_message"`
	SSLEnabled       bool      `json:"ssl_enabled"`
	Framework        string    `json:"framework"`
	FrameworkVersion string    `json:"framework_version"`
	FrontendStack    string    `json:"frontend_stack"`
	InertiaAdapter   string    `json:"inertia_adapter"`
	ProjectVariant   string    `json:"project_variant"`
	SetupMode        string    `json:"setup_mode"`
	ProvisionStage   string    `json:"provision_stage"`
	ProvisionLog     string    `json:"provision_log"`
	NginxProfile     string    `json:"nginx_profile"`
	CreatedBy        string    `json:"created_by"`
	OwnerEmail       string    `json:"owner_email,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Domains          []Domain  `json:"domains,omitempty"`
}

type Domain struct {
	ID        string    `json:"id"`
	WebsiteID string    `json:"website_id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

type SSLCertificate struct {
	ID           string    `json:"id"`
	WebsiteID    string    `json:"website_id"`
	Domain       string    `json:"domain"`
	Issuer       string    `json:"issuer"`
	Status       string    `json:"status"`
	ExpiresAt    time.Time `json:"expires_at"`
	AutoRenew    bool      `json:"auto_renew"`
	ErrorMessage string    `json:"error_message"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type PHPVersion struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Enabled   bool   `json:"enabled"`
}

type Deployment struct {
	ID         string    `json:"id"`
	WebsiteID  string    `json:"website_id"`
	CommitHash string    `json:"commit_hash"`
	Branch     string    `json:"branch"`
	Status     string    `json:"status"`
	DurationMs int       `json:"duration_ms"`
	Log        string    `json:"log"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CronJob struct {
	ID         string    `json:"id"`
	WebsiteID  string    `json:"website_id"`
	Command    string    `json:"command"`
	Schedule   string    `json:"schedule"`
	Enabled    bool      `json:"enabled"`
	LastRun    time.Time `json:"last_run"`
	LastStatus string    `json:"last_status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type QueueWorker struct {
	ID          string    `json:"id"`
	WebsiteID   string    `json:"website_id"`
	Command     string    `json:"command"`
	NumWorkers  int       `json:"num_workers"`
	AutoRestart bool      `json:"auto_restart"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type NodeApp struct {
	ID          string    `json:"id"`
	WebsiteID   string    `json:"website_id"`
	NodeVersion string    `json:"node_version"`
	PackageMgr  string    `json:"package_mgr"`
	BuildCmd    string    `json:"build_cmd"`
	StartCmd    string    `json:"start_cmd"`
	Port        int       `json:"port"`
	EnvVars     string    `json:"env_vars"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ManagedDatabase struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Engine    string    `json:"engine"`
	Charset   string    `json:"charset"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DBUser struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	Engine     string    `json:"engine"`
	Privileges string    `json:"privileges"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type DockerStatus struct {
	Installed  bool   `json:"installed"`
	Running    bool   `json:"running"`
	Version    string `json:"version"`
	Containers int    `json:"containers"`
	Images     int    `json:"images"`
}

type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	State   string `json:"state"`
	Ports   string `json:"ports"`
	Created string `json:"created"`
}

type DockerImage struct {
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Size       string `json:"size"`
	Created    string `json:"created"`
}

type DockerVolume struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
}

type DockerNetwork struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Scope  string `json:"scope"`
}

type EngineStatus struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Version   string `json:"version"`
}

type Backup struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Target     string    `json:"target"`
	Storage    string    `json:"storage"`
	Path       string    `json:"path"`
	SizeBytes  int64     `json:"size_bytes"`
	Status     string    `json:"status"`
	ErrorMsg   string    `json:"error_msg"`
	Kind       string    `json:"kind"` // manual | scheduled | safety
	CreatedBy  string    `json:"created_by"`
	TaskID     string    `json:"task_id,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type BackupSchedule struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	Target        string    `json:"target"`
	Storage       string    `json:"storage"`
	Schedule      string    `json:"schedule"`
	RetentionDays int       `json:"retention_days"`
	RetentionKeep int       `json:"retention_keep"`
	Enabled       bool      `json:"enabled"`
	LastRun       time.Time `json:"last_run"`
	LastRunStatus string    `json:"last_run_status"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type AlertRule struct {
	ID        string    `json:"id"`
	Metric    string    `json:"metric"`
	Target    string    `json:"target"`
	Operator  string    `json:"operator"`
	Threshold float64   `json:"threshold"`
	DurationS int       `json:"duration_s"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AlertEvent struct {
	ID        string    `json:"id"`
	RuleID    string    `json:"rule_id"`
	Metric    string    `json:"metric"`
	Value     float64   `json:"value"`
	Message   string    `json:"message"`
	Resolved  bool      `json:"resolved"`
	CreatedAt time.Time `json:"created_at"`
}

type NotificationChannel struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Config    string    `json:"config"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type APIToken struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	TokenHash string    `json:"-"`
	LastUsed  time.Time `json:"last_used"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type FileEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	IsDir       bool   `json:"is_dir"`
	Size        int64  `json:"size"`
	Permissions string `json:"permissions"`
	Owner       string `json:"owner"`
	ModTime     string `json:"mod_time"`
}

type UpdateInfo struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	UpdateAvail    bool   `json:"update_available"`
	ReleaseURL     string `json:"release_url"`
}

type ServerInfo struct {
	Hostname   string             `json:"hostname"`
	IP         string             `json:"ip"`
	OS         string             `json:"os"`
	Kernel     string             `json:"kernel"`
	CPU        string             `json:"cpu"`
	CPUModel   string             `json:"cpu_model"`
	CPUCores   int                `json:"cpu_cores"`
	RAM        string             `json:"ram"`
	Disk       string             `json:"disk"`
	Uptime     string             `json:"uptime"`
	Timezone   string             `json:"timezone"`
	Partitions []DiskPartition    `json:"partitions"`
	Interfaces []NetworkInterface `json:"interfaces"`
}
