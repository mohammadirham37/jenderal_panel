package security

import (
	"fmt"
	"sort"
	"strings"
)

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
	// Posture events mirror report.Findings (the worker records them), so
	// they are skipped here. Other events are grouped per component and
	// category: ten blocked addresses are one item to review, not ten.
	type group struct {
		severity Severity
		count    int
		summary  string
	}
	groups := map[string]*group{}
	var groupKeys []string
	for _, event := range events {
		if event.Status != StatusOpen && event.Status != StatusAcknowledged {
			continue
		}
		if event.Severity == SeverityLow || event.Severity == SeverityInfo || event.Component == "posture" {
			continue
		}
		key := event.Component + ":" + event.Category
		g, ok := groups[key]
		if !ok {
			g = &group{severity: event.Severity, summary: event.RecommendedAction}
			groups[key] = g
			groupKeys = append(groupKeys, key)
		}
		g.count++
		if severityRank(event.Severity) < severityRank(g.severity) {
			g.severity = event.Severity
		}
	}
	for _, key := range groupKeys {
		g := groups[key]
		component, category, _ := strings.Cut(key, ":")
		summary := g.summary
		if summary == "" {
			summary = category + " security event requires review"
		}
		if g.count > 1 {
			summary = fmt.Sprintf("%d %s events from %s need review. %s", g.count, category, component, summary)
		}
		items = append(items, reason{g.severity, component, key, summary})
	}
	level := ConditionGood
	for _, item := range items {
		if item.severity == SeverityCritical {
			level = ConditionCritical
			break
		}
		level = ConditionNeedsAttention
	}
	sort.SliceStable(items, func(i, j int) bool {
		if severityRank(items[i].severity) != severityRank(items[j].severity) {
			return severityRank(items[i].severity) < severityRank(items[j].severity)
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

// severityRank orders severities from most (0) to least urgent.
func severityRank(s Severity) int {
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
