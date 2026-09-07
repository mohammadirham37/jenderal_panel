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

type ServerInfo struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	OS       string `json:"os"`
	Kernel   string `json:"kernel"`
	CPU      string `json:"cpu"`
	RAM      string `json:"ram"`
	Disk     string `json:"disk"`
	Uptime   string `json:"uptime"`
	Timezone string `json:"timezone"`
}

type ServiceStatus struct {
	Name    string        `json:"name"`
	Active  bool          `json:"active"`
	Running bool          `json:"running"`
	Enabled bool          `json:"enabled"`
	Uptime  time.Duration `json:"uptime"`
	PID     int           `json:"pid"`
}

type UserWithRoles struct {
	User        User         `json:"user"`
	Roles       []Role       `json:"roles"`
	Permissions []Permission `json:"permissions"`
}
