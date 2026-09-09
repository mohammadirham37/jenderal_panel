package security

import (
	"reflect"
	"testing"
)

func TestScoreReturnsCriticalWithOrderedReasons(t *testing.T) {
	condition := Score(PostureReport{Findings: []Finding{{Code: "nginx_invalid", Component: "nginx", Severity: SeverityCritical, Summary: "Nginx configuration is invalid"}}}, nil)
	if condition.Level != ConditionCritical || !reflect.DeepEqual(condition.Reasons, []string{"Nginx configuration is invalid"}) {
		t.Fatalf("condition=%#v", condition)
	}
}

func TestScoreLimitsReasonsAndReportsRemainder(t *testing.T) {
	findings := make([]Finding, 7)
	for i := range findings {
		findings[i] = Finding{Code: string(rune('a' + i)), Component: "test", Severity: SeverityMedium, Summary: "needs review"}
	}
	condition := Score(PostureReport{Findings: findings}, nil)
	if condition.Level != ConditionNeedsAttention || len(condition.Reasons) != 5 || condition.Additional != 2 {
		t.Fatalf("condition=%#v", condition)
	}
}
