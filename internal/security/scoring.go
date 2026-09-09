package security

import "sort"

type Condition struct {
	Level      string   `json:"level"`
	Reasons    []string `json:"reasons"`
	Additional int      `json:"additional"`
}

func Score(report PostureReport, events []Event) Condition {
	type reason struct {
		severity                 Severity
		component, code, summary string
	}
	items := make([]reason, 0, len(report.Findings)+len(events))
	for _, finding := range report.Findings {
		if finding.Severity == SeverityLow || finding.Severity == SeverityInfo {
			continue
		}
		items = append(items, reason{finding.Severity, finding.Component, finding.Code, finding.Summary})
	}
	for _, event := range events {
		if event.Status != StatusOpen && event.Status != StatusAcknowledged {
			continue
		}
		if event.Severity == SeverityLow || event.Severity == SeverityInfo {
			continue
		}
		summary := event.RecommendedAction
		if summary == "" {
			summary = event.Category + " security event requires review"
		}
		items = append(items, reason{event.Severity, event.Component, event.Fingerprint, summary})
	}
	level := ConditionGood
	for _, item := range items {
		if item.severity == SeverityCritical {
			level = ConditionCritical
			break
		}
		level = ConditionNeedsAttention
	}
	rank := func(s Severity) int {
		switch s {
		case SeverityCritical:
			return 0
		case SeverityHigh:
			return 1
		case SeverityMedium:
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if rank(items[i].severity) != rank(items[j].severity) {
			return rank(items[i].severity) < rank(items[j].severity)
		}
		if items[i].component != items[j].component {
			return items[i].component < items[j].component
		}
		return items[i].code < items[j].code
	})
	limit := len(items)
	if limit > 5 {
		limit = 5
	}
	reasons := make([]string, 0, limit)
	for _, item := range items[:limit] {
		reasons = append(reasons, item.summary)
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "No active security issues detected.")
	}
	return Condition{Level: level, Reasons: reasons, Additional: len(items) - limit}
}
