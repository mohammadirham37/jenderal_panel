package security

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/depaudit"
)

func TestDependencySinkRecordsOnlyHighProductionFindings(t *testing.T) {
	events, _ := newEventTestService(t, nil)
	sink := NewDependencyEventSink(events)
	ctx := context.Background()
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	findings := []depaudit.Finding{
		{WebsiteID: "a", Domain: "shop.example.com", Ecosystem: "Packagist", Package: "laravel/framework", Version: "11.9.2", VulnID: "GHSA-1", Severity: "high", FixedVersion: "11.9.3"},
		{WebsiteID: "a", Domain: "shop.example.com", Ecosystem: "npm", Package: "vite", Version: "5.2.0", VulnID: "GHSA-2", Severity: "critical", Dev: true},
		{WebsiteID: "a", Domain: "shop.example.com", Ecosystem: "npm", Package: "axios", Version: "1.6.0", VulnID: "GHSA-3", Severity: "medium"},
	}
	if err := sink.SyncDependencyFindings(ctx, findings, now); err != nil {
		t.Fatal(err)
	}
	list, _, err := events.List(ctx, EventFilter{Component: DependencyComponent, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Fingerprint != "deps:a:Packagist:laravel/framework:GHSA-1" || list[0].Severity != SeverityHigh {
		t.Fatalf("events = %+v", list)
	}
	if !strings.Contains(list[0].RecommendedAction, "composer update laravel/framework") || !strings.Contains(list[0].RecommendedAction, "11.9.3") {
		t.Errorf("action = %q", list[0].RecommendedAction)
	}

	// The next scan no longer finds it: the event is resolved.
	if err := sink.SyncDependencyFindings(ctx, nil, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	list, _, _ = events.List(ctx, EventFilter{Component: DependencyComponent, Limit: 50})
	if len(list) != 1 || list[0].Status != StatusResolved {
		t.Errorf("event not resolved: %+v", list)
	}
}

type countingScanner struct{ calls int }

func (c *countingScanner) ScanIfDue(context.Context, time.Time) { c.calls++ }

func TestWorkerTickAsksForADueDependencyScan(t *testing.T) {
	events, _ := newEventTestService(t, nil)
	w := NewWorker(NewService(events, nil, nil, nil, nil), events)
	scanner := &countingScanner{}
	w.SetDependencyScanner(scanner)
	w.Tick(context.Background(), time.Now().UTC())
	if scanner.calls != 1 {
		t.Errorf("ScanIfDue calls = %d", scanner.calls)
	}
}
