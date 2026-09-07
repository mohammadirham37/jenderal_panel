package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Metrics  MetricsConfig  `yaml:"metrics"`
	Logging  LoggingConfig  `yaml:"logging"`
	Services ServicesConfig `yaml:"services"`
}

type ServerConfig struct {
	Host string    `yaml:"host"`
	Port int       `yaml:"port"`
	TLS  TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Enabled bool   `yaml:"enabled"`
	Cert    string `yaml:"cert"`
	Key     string `yaml:"key"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type AuthConfig struct {
	SessionTTL     time.Duration `yaml:"-"`
	SessionTTLRaw  string        `yaml:"session_ttl"`
	RateLimitLogin int           `yaml:"rate_limit_login"`
	RateLimitAPI   int           `yaml:"rate_limit_api"`
	Argon2         Argon2Config  `yaml:"argon2"`
}

type Argon2Config struct {
	Memory      uint32 `yaml:"memory"`
	Iterations  uint32 `yaml:"iterations"`
	Parallelism uint8  `yaml:"parallelism"`
}

type MetricsConfig struct {
	CollectInterval    time.Duration `yaml:"-"`
	CollectIntervalRaw string        `yaml:"collect_interval"`
	StoreInterval      time.Duration `yaml:"-"`
	StoreIntervalRaw   string        `yaml:"store_interval"`
	RetentionDays      int           `yaml:"retention_days"`
}

type LoggingConfig struct {
	Level      string `yaml:"level"`
	File       string `yaml:"file"`
	MaxSizeMB  int    `yaml:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups"`
}

type ServicesConfig struct {
	Allowed []string `yaml:"allowed"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(cfg)
	applyEnvOverrides(cfg)

	if err := parseDurations(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8443
	}
	if cfg.Auth.SessionTTLRaw == "" {
		cfg.Auth.SessionTTLRaw = "24h"
	}
	if cfg.Auth.RateLimitLogin == 0 {
		cfg.Auth.RateLimitLogin = 5
	}
	if cfg.Auth.RateLimitAPI == 0 {
		cfg.Auth.RateLimitAPI = 100
	}
	if cfg.Auth.Argon2.Memory == 0 {
		cfg.Auth.Argon2.Memory = 65536
	}
	if cfg.Auth.Argon2.Iterations == 0 {
		cfg.Auth.Argon2.Iterations = 3
	}
	if cfg.Auth.Argon2.Parallelism == 0 {
		cfg.Auth.Argon2.Parallelism = 2
	}
	if cfg.Metrics.CollectIntervalRaw == "" {
		cfg.Metrics.CollectIntervalRaw = "5s"
	}
	if cfg.Metrics.StoreIntervalRaw == "" {
		cfg.Metrics.StoreIntervalRaw = "60s"
	}
	if cfg.Metrics.RetentionDays == 0 {
		cfg.Metrics.RetentionDays = 7
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.MaxSizeMB == 0 {
		cfg.Logging.MaxSizeMB = 100
	}
	if cfg.Logging.MaxBackups == 0 {
		cfg.Logging.MaxBackups = 3
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("JENDERAL_SERVER_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("JENDERAL_SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = port
		}
	}
	if v := os.Getenv("JENDERAL_DATABASE_PATH"); v != "" {
		cfg.Database.Path = v
	}
	if v := os.Getenv("JENDERAL_AUTH_SESSION_TTL"); v != "" {
		cfg.Auth.SessionTTLRaw = v
	}
	if v := os.Getenv("JENDERAL_LOGGING_LEVEL"); v != "" {
		cfg.Logging.Level = strings.ToLower(v)
	}
	if v := os.Getenv("JENDERAL_LOGGING_FILE"); v != "" {
		cfg.Logging.File = v
	}
}

func parseDurations(cfg *Config) error {
	var err error
	cfg.Auth.SessionTTL, err = time.ParseDuration(cfg.Auth.SessionTTLRaw)
	if err != nil {
		return fmt.Errorf("parse auth.session_ttl %q: %w", cfg.Auth.SessionTTLRaw, err)
	}
	cfg.Metrics.CollectInterval, err = time.ParseDuration(cfg.Metrics.CollectIntervalRaw)
	if err != nil {
		return fmt.Errorf("parse metrics.collect_interval %q: %w", cfg.Metrics.CollectIntervalRaw, err)
	}
	cfg.Metrics.StoreInterval, err = time.ParseDuration(cfg.Metrics.StoreIntervalRaw)
	if err != nil {
		return fmt.Errorf("parse metrics.store_interval %q: %w", cfg.Metrics.StoreIntervalRaw, err)
	}
	return nil
}
