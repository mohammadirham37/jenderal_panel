package alert

import (
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestCreateRequestDefaultsEnabled(t *testing.T) {
	rule := (CreateRuleRequest{Metric: "cpu", Operator: "gt", Threshold: 80}).Rule()
	if !rule.Enabled {
		t.Fatal("new rule should default to enabled")
	}
}

func TestUpdateRequestMergesOnlyPresentFields(t *testing.T) {
	current := model.AlertRule{Metric: "cpu", Operator: "gt", Threshold: 80, DurationS: 60, Enabled: true}
	enabled := false
	got := (UpdateRuleRequest{Enabled: &enabled}).Merge(current)
	if got.Enabled || got.Metric != "cpu" || got.Operator != "gt" || got.Threshold != 80 || got.DurationS != 60 {
		t.Fatalf("Merge() = %#v", got)
	}
}
