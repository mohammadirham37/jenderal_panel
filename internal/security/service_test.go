package security

import (
	"context"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

type fixedProbe struct {
	status ComponentStatus
}

func (p fixedProbe) Name() string { return p.status.Name }
func (p fixedProbe) Check(context.Context) ComponentStatus {
	return p.status
}

func TestOverviewConditionUsesEventsAndEnabledComponentHealth(t *testing.T) {
	events, _ := newEventTestService(t, nil)
	tasks := taskrunner.New()
	svc := NewService(events, tasks, fixedProbe{status: ComponentStatus{
		Name: "fail2ban", State: "stopped", Installed: true, Enabled: true, Healthy: false,
	}})

	overview, err := svc.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if overview.Condition != ConditionNeedsAttention || len(overview.Reasons) == 0 {
		t.Fatalf("overview = %#v", overview)
	}

	input := validEventInput()
	input.Severity = SeverityCritical
	if _, _, err := events.Record(context.Background(), input, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	overview, err = svc.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if overview.Condition != ConditionCritical {
		t.Fatalf("condition = %q, want %q", overview.Condition, ConditionCritical)
	}
}

func TestOverviewTreatsMissingOptionalComponentAsNotInstalled(t *testing.T) {
	events, _ := newEventTestService(t, nil)
	svc := NewService(events, taskrunner.New(), fixedProbe{status: ComponentStatus{
		Name: "fail2ban", State: "not_installed", Installed: false, Enabled: false, Healthy: false,
	}})
	overview, err := svc.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if overview.Condition != ConditionGood {
		t.Fatalf("overview = %#v", overview)
	}
}
