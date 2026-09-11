package website

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// HealthCheck is the per-website HTTP probe configuration and last result.
type HealthCheck struct {
	WebsiteID           string `json:"website_id"`
	URL                 string `json:"url"`
	ExpectedStatus      int    `json:"expected_status"`
	Enabled             bool   `json:"enabled"`
	LastStatus          int    `json:"last_status"`
	LastLatencyMS       int64  `json:"last_latency_ms"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	LastCheckedAt       string `json:"last_checked_at"`
}

// Notifier sends health-check state changes to the configured channels.
type Notifier interface {
	SendAll(ctx context.Context, message string) error
}

// healthAlertAfter is the number of consecutive failures (or the recovery
// after that many failures) that triggers a notification.
const healthAlertAfter = 2

// probeHealth performs one HTTP check against target and reports the
// observed status code, latency, and whether the check passed.
func (s *Service) probeHealth(ctx context.Context, target string, expected int) (status int, latencyMS int64, ok bool) {
	result, err := s.exec.RunSudo(ctx, "curl", "-fsSL",
		"--max-time", "10",
		"-o", "/dev/null",
		"-w", "%{http_code} %{time_total}",
		target)
	latencyMS = parseCurlLatency(result.Stdout)
	if err != nil {
		return 0, latencyMS, false
	}
	fields := strings.Fields(strings.TrimSpace(result.Stdout))
	if len(fields) != 2 {
		return 0, latencyMS, false
	}
	status, convErr := strconv.Atoi(fields[0])
	if convErr != nil {
		return 0, latencyMS, false
	}
	return status, latencyMS, status == expected && result.ExitCode == 0
}

func parseCurlLatency(stdout string) int64 {
	fields := strings.Fields(strings.TrimSpace(stdout))
	if len(fields) == 2 {
		if f, err := strconv.ParseFloat(fields[1], 64); err == nil {
			return int64(f * 1000)
		}
	}
	return 0
}

// healthCheckRow is the stored state for one site's probe.
type healthCheckRow struct {
	WebsiteID           string
	URL                 string
	ExpectedStatus      int
	Enabled             bool
	ConsecutiveFailures int
}

func healthCheckURL(domain, configured string) string {
	if configured != "" {
		return configured
	}
	return "http://" + domain
}

// loadEnabledHealthChecks returns the probe configuration for every enabled
// health check.
func (s *Service) loadEnabledHealthChecks(ctx context.Context) ([]healthCheckRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT h.website_id, h.url, h.expected_status, w.domain
		 FROM website_health_checks h JOIN websites w ON w.id = h.website_id
		 WHERE h.enabled = 1`)
	if err != nil {
		return nil, fmt.Errorf("load health checks: %w", err)
	}
	defer rows.Close()

	var checks []healthCheckRow
	for rows.Next() {
		var c healthCheckRow
		var domain string
		if err := rows.Scan(&c.WebsiteID, &c.URL, &c.ExpectedStatus, &domain); err != nil {
			return nil, err
		}
		c.URL = healthCheckURL(domain, c.URL)
		checks = append(checks, c)
	}
	return checks, rows.Err()
}

// GetHealthCheck returns the health configuration and last result for a site.
func (s *Service) GetHealthCheck(ctx context.Context, websiteID string) (HealthCheck, error) {
	hc := HealthCheck{WebsiteID: websiteID, ExpectedStatus: 200}
	var lastStatus, lastChecked sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT url, expected_status, enabled, last_status, last_latency_ms, consecutive_failures, last_checked_at
		 FROM website_health_checks WHERE website_id = ?`, websiteID,
	).Scan(&hc.URL, &hc.ExpectedStatus, &hc.Enabled, &lastStatus, &hc.LastLatencyMS,
		&hc.ConsecutiveFailures, &lastChecked)
	if err == sql.ErrNoRows {
		return hc, nil
	}
	if err != nil {
		return hc, fmt.Errorf("read health check: %w", err)
	}
	if lastStatus.Valid {
		hc.LastStatus, _ = strconv.Atoi(lastStatus.String)
	}
	hc.LastCheckedAt = lastChecked.String
	return hc, nil
}

var healthURLRegex = regexp.MustCompile(`^https?://[^\s]+$`)

// SaveHealthCheck validates and stores the health check configuration.
// Changing the configuration resets the consecutive-failure counter.
func (s *Service) SaveHealthCheck(ctx context.Context, websiteID string, hc HealthCheck) error {
	if hc.URL != "" && !healthURLRegex.MatchString(hc.URL) {
		return fmt.Errorf("health URL must start with http:// or https://")
	}
	if hc.ExpectedStatus < 100 || hc.ExpectedStatus > 599 {
		return fmt.Errorf("expected status must be between 100 and 599")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO website_health_checks (website_id, url, expected_status, enabled, last_checked_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(website_id) DO UPDATE SET url=excluded.url, expected_status=excluded.expected_status,
		   enabled=excluded.enabled, updated_at=excluded.updated_at`,
		websiteID, hc.URL, hc.ExpectedStatus, boolToInt(hc.Enabled), now, now,
	)
	if err != nil {
		return fmt.Errorf("save health check: %w", err)
	}
	return nil
}

// ProbeNow runs one health check immediately (manual "check now").
func (s *Service) ProbeNow(ctx context.Context, websiteID string) (HealthCheck, error) {
	hc, err := s.GetHealthCheck(ctx, websiteID)
	if err != nil {
		return hc, err
	}
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return hc, err
	}
	target := healthCheckURL(w.Domain, hc.URL)
	st, latency, ok := s.probeHealth(ctx, target, hc.ExpectedStatus)
	hc.LastStatus = st
	hc.LastLatencyMS = latency
	hc.LastCheckedAt = time.Now().UTC().Format(time.RFC3339)
	if ok {
		hc.ConsecutiveFailures = 0
	} else {
		hc.ConsecutiveFailures++
	}
	_, _ = s.db.ExecContext(ctx,
		`UPDATE website_health_checks SET last_status = ?, last_latency_ms = ?,
		 consecutive_failures = ?, last_checked_at = ?, updated_at = ? WHERE website_id = ?`,
		st, latency, hc.ConsecutiveFailures, hc.LastCheckedAt, time.Now().UTC().Format(time.RFC3339), websiteID)
	return hc, nil
}

// runHealthProbe performs one probe and records the outcome for the site.
// The previous consecutive-failure count decides whether the state change
// (down or recovered) deserves a notification.
func (s *Service) runHealthProbe(ctx context.Context, c healthCheckRow, notify func(string)) {
	expected := c.ExpectedStatus
	if expected == 0 {
		expected = 200
	}
	status, latency, ok := s.probeHealth(ctx, c.URL, expected)

	var prevFailures int
	_ = s.db.QueryRowContext(ctx,
		`SELECT consecutive_failures FROM website_health_checks WHERE website_id = ?`,
		c.WebsiteID).Scan(&prevFailures)

	failures := 0
	now := time.Now().UTC().Format(time.RFC3339)
	if ok {
		if prevFailures >= healthAlertAfter {
			notify(fmt.Sprintf("Health check RECOVERED for %s (%s) — responding %d after %d failures.",
				c.WebsiteID, c.URL, status, prevFailures))
		}
	} else {
		failures = prevFailures + 1
		if failures == healthAlertAfter {
			notify(fmt.Sprintf("Health check DOWN for %s (%s) — %d consecutive failures (last status %d).",
				c.WebsiteID, c.URL, failures, status))
		}
	}

	_, _ = s.db.ExecContext(ctx,
		`UPDATE website_health_checks SET last_status = ?, last_latency_ms = ?,
		 consecutive_failures = ?, last_checked_at = ?, updated_at = ?
		 WHERE website_id = ?`,
		status, latency, failures, now, now, c.WebsiteID)
}

// ProbeAllWebsiteHealth runs every enabled health check once. It returns the
// number of probes performed.
func (s *Service) ProbeAllWebsiteHealth(ctx context.Context, notify func(string)) (int, error) {
	checks, err := s.loadEnabledHealthChecks(ctx)
	if err != nil {
		return 0, err
	}
	for _, c := range checks {
		if ctx.Err() != nil {
			break
		}
		s.runHealthProbe(ctx, c, notify)
	}
	return len(checks), nil
}
