package alert

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// NotificationSender is the interface the checker uses to send alerts.
type NotificationSender interface {
	SendAll(ctx context.Context, message string) error
}

// Checker periodically evaluates alert rules against current system metrics.
type Checker struct {
	alertSvc   *Service
	notifSvc   NotificationSender
	getMetrics func() model.ServerMetrics
}

// NewChecker creates a new Checker.
func NewChecker(alertSvc *Service, notifSvc NotificationSender, getMetrics func() model.ServerMetrics) *Checker {
	return &Checker{
		alertSvc:   alertSvc,
		notifSvc:   notifSvc,
		getMetrics: getMetrics,
	}
}

// Start begins the background checker loop with a 60-second ticker.
func (c *Checker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.check(ctx)
			}
		}
	}()
}

func (c *Checker) check(ctx context.Context) {
	rules, err := c.alertSvc.ListRules(ctx)
	if err != nil {
		log.Printf("[alert-checker] failed to load rules: %v", err)
		return
	}

	metrics := c.getMetrics()

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		value, ok := metricValue(metrics, rule.Metric)
		if !ok {
			continue
		}

		if evaluate(value, rule.Operator, rule.Threshold) {
			msg := fmt.Sprintf("Alert: %s is %.2f (threshold %s %.2f)", rule.Metric, value, rule.Operator, rule.Threshold)

			ev, err := c.alertSvc.LogAlert(ctx, rule.ID, rule.Metric, value, msg)
			if err != nil {
				log.Printf("[alert-checker] failed to log alert for rule %s: %v", rule.ID, err)
				continue
			}

			if c.notifSvc != nil {
				if sendErr := c.notifSvc.SendAll(ctx, msg); sendErr != nil {
					log.Printf("[alert-checker] failed to send notification for event %s: %v", ev.ID, sendErr)
				}
			}
		}
	}
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
