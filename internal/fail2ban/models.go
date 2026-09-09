package fail2ban

import "time"

type Settings struct {
	SSHDEnabled     bool     `json:"sshd_enabled"`
	EnabledJails    []string `json:"enabled_jails,omitempty"`
	MaxRetry        int      `json:"max_retry"`
	FindTimeSeconds int      `json:"find_time_seconds"`
	BanTimeSeconds  int      `json:"ban_time_seconds"`
	IgnoreIPs       []string `json:"ignore_ips"`
}

type Status struct {
	Installed bool      `json:"installed"`
	Running   bool      `json:"running"`
	Enabled   bool      `json:"enabled"`
	Healthy   bool      `json:"healthy"`
	State     string    `json:"state"`
	Version   string    `json:"version,omitempty"`
	Message   string    `json:"message,omitempty"`
	SSHPort   int       `json:"ssh_port,omitempty"`
	Jails     []Jail    `json:"jails"`
	CheckedAt time.Time `json:"checked_at"`
}

type Jail struct {
	Name            string   `json:"name"`
	CurrentlyFailed int      `json:"currently_failed"`
	TotalFailed     int      `json:"total_failed"`
	CurrentlyBanned int      `json:"currently_banned"`
	TotalBanned     int      `json:"total_banned"`
	BanTimeSeconds  int      `json:"ban_time_seconds"`
	BannedIPs       []string `json:"banned_ips"`
	FilterAvailable bool     `json:"filter_available"`
	SourceAvailable bool     `json:"source_available"`
}

type Ban struct {
	ID        string     `json:"id,omitempty"`
	Jail      string     `json:"jail"`
	IP        string     `json:"ip"`
	Reason    string     `json:"reason,omitempty"`
	StartedAt time.Time  `json:"started_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Manual    bool       `json:"manual"`
}
