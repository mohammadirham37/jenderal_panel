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

func TestScoreGroupsEventsAndIgnoresBlockedAttacks(t *testing.T) {
	events := []Event{
		{EventInput: EventInput{Fingerprint: "fail2ban:sshd:203.0.113.1", Component: "fail2ban", Category: "intrusion_blocked", Severity: SeverityLow}, Status: StatusOpen},
		{EventInput: EventInput{Fingerprint: "posture:ufw_inactive", Component: "posture", Category: "posture", Severity: SeverityMedium, RecommendedAction: "duplicate of finding"}, Status: StatusOpen},
		{EventInput: EventInput{Fingerprint: "malware:a", Component: "malware", Category: "malware", Severity: SeverityHigh, RecommendedAction: "Review the quarantined sample."}, Status: StatusOpen},
		{EventInput: EventInput{Fingerprint: "malware:b", Component: "malware", Category: "malware", Severity: SeverityHigh, RecommendedAction: "Review the quarantined sample."}, Status: StatusAcknowledged},
		{EventInput: EventInput{Fingerprint: "malware:c", Component: "malware", Category: "malware", Severity: SeverityHigh}, Status: StatusResolved},
	}
	condition := Score(PostureReport{}, events)
	want := []string{"2 malware events from malware need review. Review the quarantined sample."}
	if condition.Level != ConditionNeedsAttention || !reflect.DeepEqual(condition.Reasons, want) || condition.Additional != 0 {
		t.Fatalf("condition=%#v", condition)
	}

	onlyBlocked := Score(PostureReport{}, events[:1])
	if onlyBlocked.Level != ConditionGood {
		t.Fatalf("blocked attacks alone must not need attention: %#v", onlyBlocked)
	}
}
