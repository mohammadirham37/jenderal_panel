package alert

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// NotificationSender is the interface the checker uses to send alerts.
type NotificationSender interface {
	SendAll(ctx context.Context, message string) error
}

type ServiceStatusProvider interface {
	Status(context.Context, string) (*model.ServiceStatus, error)
}

type CertificateProvider interface {
	List(context.Context) ([]model.SSLCertificate, error)
}

// Checker periodically evaluates alert rules against current system metrics.
type Checker struct {
	alertSvc   *Service
	notifSvc   NotificationSender
	getMetrics func() model.ServerMetrics
	services   ServiceStatusProvider
	certs      CertificateProvider
	mu         sync.Mutex
	pending    map[string]pendingCondition
}

type pendingCondition struct {
	since     time.Time
	signature string
}

// NewChecker creates a new Checker.
func NewChecker(alertSvc *Service, notifSvc NotificationSender, getMetrics func() model.ServerMetrics, services ServiceStatusProvider, certs CertificateProvider) *Checker {
	return &Checker{
		alertSvc:   alertSvc,
		notifSvc:   notifSvc,
		getMetrics: getMetrics,
		services:   services,
		certs:      certs,
		pending:    make(map[string]pendingCondition),
	}
}

// Start begins the background checker loop with a 60-second ticker.
func (c *Checker) Start(ctx context.Context) {
	go func() {
		c.Check(ctx, time.Now().UTC())
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.Check(ctx, time.Now().UTC())
			}
		}
	}()
}

func (c *Checker) Check(ctx context.Context, now time.Time) {
	rules, err := c.alertSvc.ListRules(ctx)
	if err != nil {
		log.Printf("[alert-checker] failed to load rules: %v", err)
		return
	}

	for _, rule := range rules {
		if !rule.Enabled {
			c.clearPending(rule.ID)
			continue
		}

		value, ok, err := c.ruleValue(ctx, rule, now)
		if err != nil {
			c.clearPending(rule.ID)
			log.Printf("[alert-checker] failed to evaluate rule %s: %v", rule.ID, err)
			continue
		}
		if !ok {
			c.clearPending(rule.ID)
			continue
		}
		open, found, err := c.alertSvc.FindUnresolvedByRule(ctx, rule.ID)
		if err != nil {
			log.Printf("[alert-checker] failed to read alert state for rule %s: %v", rule.ID, err)
			continue
		}
		if !evaluate(value, rule.Operator, rule.Threshold) {
			c.clearPending(rule.ID)
			if found {
				resolved, err := c.alertSvc.ResolveAlertsByRule(ctx, rule.ID)
				if err != nil {
					log.Printf("[alert-checker] failed to resolve event %s: %v", open.ID, err)
					continue
				}
				if resolved > 0 {
					c.send(ctx, open.ID, fmt.Sprintf("Resolved: %s returned to normal (value %.2f)", ruleLabel(rule), value))
				}
			}
			continue
		}
		if found || !c.durationReached(rule, now) {
			continue
		}
		msg := fmt.Sprintf("Alert: %s is %.2f (threshold %s %.2f)", ruleLabel(rule), value, rule.Operator, rule.Threshold)
		event, err := c.alertSvc.LogAlert(ctx, rule.ID, rule.Metric, value, msg)
		if err != nil {
			log.Printf("[alert-checker] failed to log alert for rule %s: %v", rule.ID, err)
			continue
		}
		c.clearPending(rule.ID)
		c.send(ctx, event.ID, msg)
	}
}

func (c *Checker) send(ctx context.Context, eventID, message string) {
	if c.notifSvc != nil {
		if err := c.notifSvc.SendAll(ctx, message); err != nil {
			log.Printf("[alert-checker] failed to send notification for event %s: %v", eventID, err)
		}
	}
}

func (c *Checker) durationReached(rule model.AlertRule, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending, found := c.pending[rule.ID]
	signature := ruleConditionSignature(rule)
	if !found || pending.signature != signature {
		pending = pendingCondition{since: now, signature: signature}
		c.pending[rule.ID] = pending
	}
	return now.Sub(pending.since) >= time.Duration(rule.DurationS)*time.Second
}

func ruleConditionSignature(rule model.AlertRule) string {
	return fmt.Sprintf("%q\x00%q\x00%q\x00%.17g\x00%d", rule.Metric, rule.Target, rule.Operator, rule.Threshold, rule.DurationS)
}

func (c *Checker) clearPending(ruleID string) {
	c.mu.Lock()
	delete(c.pending, ruleID)
	c.mu.Unlock()
}

func (c *Checker) ruleValue(ctx context.Context, rule model.AlertRule, now time.Time) (float64, bool, error) {
	if value, ok := metricValue(c.getMetrics(), rule.Metric); ok {
		return value, true, nil
	}
	switch rule.Metric {
	case "service_down":
		if c.services == nil {
			return 0, false, fmt.Errorf("service status provider is unavailable")
		}
		status, err := c.services.Status(ctx, rule.Target)
		if err != nil {
			return 0, false, err
		}
		if status == nil || !status.Active {
			return 1, true, nil
		}
		return 0, true, nil
	case "ssl_expiry":
		if c.certs == nil {
			return 0, false, fmt.Errorf("certificate provider is unavailable")
		}
		certificates, err := c.certs.List(ctx)
		if err != nil {
			return 0, false, err
		}
		for _, certificate := range certificates {
			if certificate.Domain == rule.Target && certificate.Status == "active" {
				return certificate.ExpiresAt.Sub(now).Hours() / 24, true, nil
			}
		}
		return 0, false, fmt.Errorf("active certificate for %s was not found", rule.Target)
	default:
		return 0, false, nil
	}
}

func ruleLabel(rule model.AlertRule) string {
	if rule.Target == "" {
		return rule.Metric
	}
	return fmt.Sprintf("%s (%s)", rule.Metric, rule.Target)
}

// metricValue extracts the named metric from ServerMetrics.
func metricValue(m model.ServerMetrics, metric string) (float64, bool) {
	switch metric {
	case "cpu":
		return m.CPU, true
	case "ram":
		if m.RAMTotal == 0 {
			return 0, true
		}
		return float64(m.RAMUsed) / float64(m.RAMTotal) * 100, true
	case "disk":
		if m.DiskTotal == 0 {
			return 0, true
		}
		return float64(m.DiskUsed) / float64(m.DiskTotal) * 100, true
	case "load1":
		return m.Load1, true
	case "load5":
		return m.Load5, true
	case "load15":
		return m.Load15, true
	default:
		return 0, false
	}
}

// evaluate compares a value against a threshold using the given operator.
func evaluate(value float64, operator string, threshold float64) bool {
	switch operator {
	case "gt":
		return value > threshold
	case "lt":
		return value < threshold
	case "eq":
		return value == threshold
	default:
		return false
	}
}
