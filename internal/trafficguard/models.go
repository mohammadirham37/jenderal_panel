package trafficguard

import (
	"net/netip"
	"time"
)

type LogEntry struct {
	IP                     netip.Addr
	Time                   time.Time
	Method, Path, Protocol string
	Status                 int
	Bytes                  int64
	Referrer, UserAgent    string
}
type LogCursor struct {
	WebsiteID string    `json:"website_id"`
	Inode     uint64    `json:"inode"`
	Offset    int64     `json:"offset"`
	UpdatedAt time.Time `json:"updated_at"`
}
type MinuteBucket struct {
	WebsiteID string         `json:"website_id"`
	BucketAt  time.Time      `json:"bucket_at"`
	Requests  int            `json:"requests"`
	Status4xx int            `json:"status_4xx"`
	Status5xx int            `json:"status_5xx"`
	Status429 int            `json:"status_429"`
	Bytes     int64          `json:"bytes"`
	PeakRPS   int            `json:"peak_rps"`
	TopIPs    map[string]int `json:"top_ips"`
	TopPaths  map[string]int `json:"top_paths"`
	TopAgents map[string]int `json:"top_agents"`
}
type HourlyBucket struct {
	WebsiteID                                 string    `json:"website_id"`
	BucketAt                                  time.Time `json:"bucket_at"`
	Requests, Status4xx, Status5xx, Status429 int
	Bytes                                     int64
	PeakRPS                                   int
}
type Baseline struct {
	WebsiteID      string `json:"website_id"`
	SampleCount    int    `json:"sample_count"`
	MeanRPM, M2RPM float64
	FirstSampleAt  *time.Time
	UpdatedAt      time.Time
}
type WebsiteProfile struct {
	WebsiteID         string    `json:"website_id"`
	Mode              string    `json:"mode"`
	ProxyMode         string    `json:"proxy_mode"`
	ProxyHeader       string    `json:"proxy_header"`
	ProxyCIDRs        []string  `json:"proxy_cidrs"`
	RequestsPerSecond int       `json:"requests_per_second"`
	Burst             int       `json:"burst"`
	Connections       int       `json:"connections"`
	ObserveStartedAt  time.Time `json:"observe_started_at"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
type ProxySnapshot struct {
	IPv4, IPv6          []string
	FetchedAt           time.Time
	ConsecutiveFailures int
}
type Anomaly struct {
	Signal, Severity, Message string
	Value, Threshold          float64
}

func DefaultProfile(id string, now time.Time) WebsiteProfile {
	return WebsiteProfile{WebsiteID: id, Mode: "observe", ProxyMode: "direct", RequestsPerSecond: 10, Burst: 20, Connections: 20, ObserveStartedAt: now, CreatedAt: now, UpdatedAt: now}
}
