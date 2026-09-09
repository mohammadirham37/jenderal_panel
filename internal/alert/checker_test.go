package alert

import (
	"context"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type checkerSender struct{ messages []string }

func (s *checkerSender) SendAll(_ context.Context, message string) error {
	s.messages = append(s.messages, message)
	return nil
}

type checkerServices struct {
	statuses map[string]*model.ServiceStatus
}

func (s checkerServices) Status(_ context.Context, name string) (*model.ServiceStatus, error) {
	return s.statuses[name], nil
}

type checkerCertificates struct{ certificates []model.SSLCertificate }

func (c checkerCertificates) List(context.Context) ([]model.SSLCertificate, error) {
	return c.certificates, nil
}

func TestCheckerHonorsDurationDeduplicatesAndResolves(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, nil)
	rule, err := svc.CreateRule(context.Background(), model.AlertRule{Metric: "cpu", Operator: "gt", Threshold: 80, DurationS: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	sender := &checkerSender{}
	metrics := model.ServerMetrics{CPU: 90}
	checker := NewChecker(svc, sender, func() model.ServerMetrics { return metrics }, checkerServices{}, checkerCertificates{})
	now := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)

	checker.Check(context.Background(), now)
	checker.Check(context.Background(), now.Add(59*time.Second))
	if events, _ := svc.ListHistory(context.Background(), 10); len(events) != 0 {
		t.Fatalf("alert fired before duration: %d events", len(events))
	}
	checker.Check(context.Background(), now.Add(60*time.Second))
	checker.Check(context.Background(), now.Add(120*time.Second))
	if events, _ := svc.ListHistory(context.Background(), 10); len(events) != 1 {
		t.Fatalf("alert was not deduplicated: %d events", len(events))
	}
	if _, err := svc.LogAlert(context.Background(), rule.ID, rule.Metric, metrics.CPU, "historical duplicate"); err != nil {
		t.Fatal(err)
	}
	metrics.CPU = 20
	checker.Check(context.Background(), now.Add(180*time.Second))
	events, _ := svc.ListHistory(context.Background(), 10)
	if len(events) != 2 || !events[0].Resolved || !events[1].Resolved || len(sender.messages) != 2 {
		t.Fatalf("recovery = events %#v, messages %d", events, len(sender.messages))
	}
}

func TestCheckerEvaluatesServiceAndSSLTargets(t *testing.T) {
	now := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	services := checkerServices{statuses: map[string]*model.ServiceStatus{"nginx": {Name: "nginx", Active: false}}}
	certs := checkerCertificates{certificates: []model.SSLCertificate{{Domain: "example.com", Status: "active", ExpiresAt: now.Add(5 * 24 * time.Hour)}}}
	checker := NewChecker(nil, nil, func() model.ServerMetrics { return model.ServerMetrics{} }, services, certs)

	value, ok, err := checker.ruleValue(context.Background(), model.AlertRule{Metric: "service_down", Target: "nginx"}, now)
	if err != nil || !ok || value != 1 {
		t.Fatalf("service value = %v, %v, %v", value, ok, err)
	}
	value, ok, err = checker.ruleValue(context.Background(), model.AlertRule{Metric: "ssl_expiry", Target: "example.com"}, now)
	if err != nil || !ok || value != 5 {
		t.Fatalf("ssl value = %v, %v, %v", value, ok, err)
	}
}
