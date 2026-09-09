package alert

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateRule(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, nil)
	ctx := context.Background()

	rule, err := svc.CreateRule(ctx, model.AlertRule{
		Metric:    "cpu",
		Operator:  "gt",
		Threshold: 90,
		DurationS: 60,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if rule.ID == "" {
		t.Error("expected non-empty ID")
	}
	if rule.Metric != "cpu" {
		t.Errorf("expected metric=cpu, got %s", rule.Metric)
	}
	if rule.Operator != "gt" {
		t.Errorf("expected operator=gt, got %s", rule.Operator)
	}
	if rule.Threshold != 90 {
		t.Errorf("expected threshold=90, got %f", rule.Threshold)
	}
	if !rule.Enabled {
		t.Error("expected enabled=true")
	}

	// Validation: empty metric
	_, err = svc.CreateRule(ctx, model.AlertRule{
		Operator:  "gt",
		Threshold: 90,
	})
	if err == nil {
		t.Error("expected validation error for empty metric")
	}

	// Validation: invalid operator
	_, err = svc.CreateRule(ctx, model.AlertRule{
		Metric:    "cpu",
		Operator:  "invalid",
		Threshold: 90,
	})
	if err == nil {
		t.Error("expected validation error for invalid operator")
	}
}

func TestListRules(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, nil)
	ctx := context.Background()

	// Empty list
	rules, err := svc.ListRules(ctx)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(rules))
	}

	// Create two rules
	_, err = svc.CreateRule(ctx, model.AlertRule{
		Metric: "cpu", Operator: "gt", Threshold: 80, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create rule 1: %v", err)
	}
	_, err = svc.CreateRule(ctx, model.AlertRule{
		Metric: "ram", Operator: "gt", Threshold: 70, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create rule 2: %v", err)
	}

	rules, err = svc.ListRules(ctx)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(rules))
	}
}

func TestGetRule(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, nil)
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, model.AlertRule{
		Metric: "disk", Operator: "gt", Threshold: 95, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := svc.GetRule(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, got.ID)
	}
	if got.Metric != "disk" {
		t.Errorf("expected metric=disk, got %s", got.Metric)
	}

	// Not found
	_, err = svc.GetRule(ctx, "nonexistent")
	if err == nil {
		t.Error("expected not found error")
	}
}

func TestUpdateAndDeleteRule(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, nil)
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, model.AlertRule{
		Metric: "cpu", Operator: "gt", Threshold: 80, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Update
	err = svc.UpdateRule(ctx, created.ID, model.AlertRule{
		Metric: "cpu", Operator: "gt", Threshold: 95, Enabled: false,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := svc.GetRule(ctx, created.ID)
	if got.Threshold != 95 {
		t.Errorf("expected threshold=95, got %f", got.Threshold)
	}
	if got.Enabled {
		t.Error("expected enabled=false")
	}

	// Update non-existent
	err = svc.UpdateRule(ctx, "nonexistent", model.AlertRule{
		Metric: "cpu", Operator: "gt", Threshold: 50,
	})
	if err == nil {
		t.Error("expected not found error")
	}

	// Delete
	err = svc.DeleteRule(ctx, created.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = svc.GetRule(ctx, created.ID)
	if err == nil {
		t.Error("expected not found after delete")
	}

	// Delete non-existent
	err = svc.DeleteRule(ctx, "nonexistent")
	if err == nil {
		t.Error("expected not found error")
	}
}

func TestLogAlert(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, nil)
	ctx := context.Background()

	// Create a rule first (for FK)
	rule, err := svc.CreateRule(ctx, model.AlertRule{
		Metric: "cpu", Operator: "gt", Threshold: 90, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	ev, err := svc.LogAlert(ctx, rule.ID, "cpu", 95.5, "CPU exceeded 90%")
	if err != nil {
		t.Fatalf("log alert: %v", err)
	}
	if ev.ID == "" {
		t.Error("expected non-empty event ID")
	}
	if ev.Value != 95.5 {
		t.Errorf("expected value=95.5, got %f", ev.Value)
	}
	if ev.Resolved {
		t.Error("expected resolved=false")
	}
}

func TestListHistory(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, nil)
	ctx := context.Background()

	rule, err := svc.CreateRule(ctx, model.AlertRule{
		Metric: "cpu", Operator: "gt", Threshold: 90, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	// Insert several alerts
	for i := 0; i < 5; i++ {
		_, err := svc.LogAlert(ctx, rule.ID, "cpu", float64(91+i), "CPU alert")
		if err != nil {
			t.Fatalf("log alert %d: %v", i, err)
		}
	}

	events, err := svc.ListHistory(ctx, 3)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 events (limited), got %d", len(events))
	}

	// Full list
	all, err := svc.ListHistory(ctx, 0)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 5 {
		t.Errorf("expected 5 events, got %d", len(all))
	}
}

func TestResolveAlert(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, nil)
	ctx := context.Background()

	rule, err := svc.CreateRule(ctx, model.AlertRule{
		Metric: "cpu", Operator: "gt", Threshold: 90, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	ev, err := svc.LogAlert(ctx, rule.ID, "cpu", 95, "CPU high")
	if err != nil {
		t.Fatalf("log alert: %v", err)
	}

	err = svc.ResolveAlert(ctx, ev.ID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Verify resolved
	events, _ := svc.ListHistory(ctx, 10)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if !events[0].Resolved {
		t.Error("expected event to be resolved")
	}

	// Resolve non-existent
	err = svc.ResolveAlert(ctx, "nonexistent")
	if err == nil {
		t.Error("expected not found error")
	}
}

func TestRuleTargetRoundTripAndUnresolvedLookup(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, nil)
	ctx := context.Background()
	rule, err := svc.CreateRule(ctx, model.AlertRule{Metric: "service_down", Operator: "eq", Threshold: 1, Target: "nginx", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetRule(ctx, rule.ID)
	if err != nil || got.Target != "nginx" {
		t.Fatalf("GetRule() = %#v, %v; want target nginx", got, err)
	}
	if _, found, err := svc.FindUnresolvedByRule(ctx, rule.ID); err != nil || found {
		t.Fatalf("empty unresolved lookup = found %v, err %v", found, err)
	}
	event, err := svc.LogAlert(ctx, rule.ID, rule.Metric, 1, "nginx is down")
	if err != nil {
		t.Fatal(err)
	}
	open, found, err := svc.FindUnresolvedByRule(ctx, rule.ID)
	if err != nil || !found || open.ID != event.ID {
		t.Fatalf("unresolved lookup = %#v, %v, %v", open, found, err)
	}
}

func TestTargetValidationUsesConfiguredProviders(t *testing.T) {
	svc := NewService(setupTestDB(t), nil)
	now := time.Now().UTC()
	serviceProvider := checkerServices{statuses: map[string]*model.ServiceStatus{"nginx": {Name: "nginx"}}}
	svc.SetTargetProviders(
		serviceProvider,
		checkerCertificates{certificates: []model.SSLCertificate{{Domain: "example.com", Status: "active", ExpiresAt: now.Add(24 * time.Hour)}}},
	)
	ctx := context.Background()
	if _, err := svc.CreateRule(ctx, model.AlertRule{Metric: "service_down", Target: "missing", Operator: "eq", Threshold: 1}); err == nil {
		t.Fatal("missing service target was accepted")
	}
	if _, err := svc.CreateRule(ctx, model.AlertRule{Metric: "ssl_expiry", Target: "missing.example", Operator: "lt", Threshold: 14}); err == nil {
		t.Fatal("missing certificate target was accepted")
	}
	rule, err := svc.CreateRule(ctx, model.AlertRule{Metric: "ssl_expiry", Target: "example.com", Operator: "lt", Threshold: 14})
	if err != nil {
		t.Fatalf("active certificate target rejected: %v", err)
	}
	if err := svc.UpdateRule(ctx, rule.ID, model.AlertRule{Metric: "service_down", Target: "missing", Operator: "eq", Threshold: 1}); err == nil {
		t.Fatal("missing service target was accepted during update")
	}
	serviceRule, err := svc.CreateRule(ctx, model.AlertRule{Metric: "service_down", Target: "nginx", Operator: "eq", Threshold: 1, Enabled: true})
	if err != nil {
		t.Fatalf("available service target rejected: %v", err)
	}
	delete(serviceProvider.statuses, "nginx")
	serviceRule.Enabled = false
	if err := svc.UpdateRule(ctx, serviceRule.ID, serviceRule); err != nil {
		t.Fatalf("disable rule with stale target: %v", err)
	}
}
