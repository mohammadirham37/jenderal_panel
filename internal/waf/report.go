package waf

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The protection report reads the website's nginx error log, where both
// ModSecurity ("ModSecurity: Access denied ...") and the DoS rate limit
// ("limiting requests ... zone \"jenderal_dos\"") record every request they
// act on, so no separate event store is needed.

const (
	reportLogLines   = 20000
	reportRecentSize = 30
	reportTopSize    = 5
)

// ReportEvent is one request WAF or DoS protection acted on.
type ReportEvent struct {
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"` // waf_blocked, waf_detected, dos_limited
	IP      string    `json:"ip"`
	Request string    `json:"request"`
	RuleID  string    `json:"rule_id,omitempty"`
	Message string    `json:"message,omitempty"`
}

// CountEntry is a ranked value with its occurrence count.
type CountEntry struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
	Count int    `json:"count"`
}

// SiteReport summarizes WAF and DoS activity for one website.
type SiteReport struct {
	Hours        int           `json:"hours"`
	LogAvailable bool          `json:"log_available"`
	WAFBlocked   int           `json:"waf_blocked"`
	WAFDetected  int           `json:"waf_detected"`
	DoSLimited   int           `json:"dos_limited"`
	TopRules     []CountEntry  `json:"top_rules"`
	TopIPs       []CountEntry  `json:"top_ips"`
	Recent       []ReportEvent `json:"recent"`
}

var (
	logTimePattern  = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}) `)
	clientPattern   = regexp.MustCompile(`, client: ([0-9A-Fa-f:.]+)`)
	modsecClient    = regexp.MustCompile(`\[client ([0-9A-Fa-f:.]+)\]`)
	requestPattern  = regexp.MustCompile(`, request: "([^"]*)"`)
	ruleIDPattern   = regexp.MustCompile(`\[id "(\d+)"\]`)
	ruleMsgPattern  = regexp.MustCompile(`\[msg "([^"]*)"\]`)
	modsecURIPatten = regexp.MustCompile(`\[uri "([^"]*)"\]`)
	uniqueIDPattern = regexp.MustCompile(`\[unique_id "([^"]*)"\]`)
)

// anomalyRules are the CRS scoring rules that deny a request once the
// individual attack rules have added up; the attack rule is the useful one
// to report.
var anomalyRules = map[string]bool{"949110": true, "959100": true, "980130": true, "980140": true}

// SiteReport reads the last part of errorLog and reports the activity of
// the last hours.
func (s *Service) SiteReport(ctx context.Context, errorLog string, hours int, now time.Time) (SiteReport, error) {
	if hours <= 0 || hours > 24*7 {
		hours = 24
	}
	report := SiteReport{Hours: hours, TopRules: []CountEntry{}, TopIPs: []CountEntry{}, Recent: []ReportEvent{}}
	if !strings.HasPrefix(errorLog, "/home/") || strings.Contains(errorLog, "..") {
		return report, fmt.Errorf("invalid log path")
	}
	res, err := s.exec.RunSudo(ctx, "tail", "-n", fmt.Sprint(reportLogLines), "--", errorLog)
	if err != nil {
		return report, fmt.Errorf("read error log: %w", err)
	}
	if res == nil || res.ExitCode != 0 {
		return report, nil // no log yet: an empty report
	}
	report.LogAvailable = true
	return parseSiteReport(res.Stdout, hours, now, time.Local, report), nil
}

func parseSiteReport(log string, hours int, now time.Time, loc *time.Location, report SiteReport) SiteReport {
	since := now.Add(-time.Duration(hours) * time.Hour)
	rules := map[string]*CountEntry{}
	ips := map[string]int{}
	var events []ReportEvent

	// ModSecurity logs one line per matching rule plus the final denial;
	// lines sharing a unique_id are one request.
	var parsed []ReportEvent
	byRequest := map[string]int{}
	for _, line := range strings.Split(log, "\n") {
		event, ok := parseReportLine(line, loc)
		if !ok || event.Time.Before(since) {
			continue
		}
		uid := ""
		if m := uniqueIDPattern.FindStringSubmatch(line); m != nil && event.Kind != "dos_limited" {
			uid = m[1]
		}
		if idx, seen := byRequest[uid]; uid != "" && seen {
			merged := &parsed[idx]
			if event.Kind == "waf_blocked" {
				merged.Kind = "waf_blocked"
			}
			if anomalyRules[merged.RuleID] && event.RuleID != "" && !anomalyRules[event.RuleID] {
				merged.RuleID, merged.Message = event.RuleID, event.Message
			}
			continue
		}
		if uid != "" {
			byRequest[uid] = len(parsed)
		}
		parsed = append(parsed, event)
	}

	for _, event := range parsed {
		switch event.Kind {
		case "waf_blocked":
			report.WAFBlocked++
		case "waf_detected":
			report.WAFDetected++
		case "dos_limited":
			report.DoSLimited++
		}
		if event.RuleID != "" {
			entry := rules[event.RuleID]
			if entry == nil {
				entry = &CountEntry{Key: event.RuleID, Label: event.Message}
				rules[event.RuleID] = entry
			}
			entry.Count++
		}
		if event.IP != "" {
			ips[event.IP]++
		}
		events = append(events, event)
	}

	for _, entry := range rules {
		report.TopRules = append(report.TopRules, *entry)
	}
	for ip, count := range ips {
		report.TopIPs = append(report.TopIPs, CountEntry{Key: ip, Count: count})
	}
	rank := func(list []CountEntry) []CountEntry {
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Count != list[j].Count {
				return list[i].Count > list[j].Count
			}
			return list[i].Key < list[j].Key
		})
		if len(list) > reportTopSize {
			list = list[:reportTopSize]
		}
		return list
	}
	report.TopRules = rank(report.TopRules)
	report.TopIPs = rank(report.TopIPs)

	for i := len(events) - 1; i >= 0 && len(report.Recent) < reportRecentSize; i-- {
		report.Recent = append(report.Recent, events[i])
	}
	return report
}

// parseReportLine recognizes a ModSecurity or DoS rate-limit entry.
func parseReportLine(line string, loc *time.Location) (ReportEvent, bool) {
	var event ReportEvent
	switch {
	case strings.Contains(line, "ModSecurity: Access denied"):
		event.Kind = "waf_blocked"
	case strings.Contains(line, "ModSecurity: Warning"):
		event.Kind = "waf_detected"
	case strings.Contains(line, "limiting requests") && strings.Contains(line, `zone "jenderal_dos"`) &&
		!strings.Contains(line, "dry run"):
		event.Kind = "dos_limited"
	default:
		return event, false
	}
	m := logTimePattern.FindStringSubmatch(line)
	if m == nil {
		return event, false
	}
	t, err := time.ParseInLocation("2006/01/02 15:04:05", m[1], loc)
	if err != nil {
		return event, false
	}
	event.Time = t

	if m := clientPattern.FindStringSubmatch(line); m != nil {
		event.IP = m[1]
	} else if m := modsecClient.FindStringSubmatch(line); m != nil {
		event.IP = m[1]
	}
	if m := requestPattern.FindStringSubmatch(line); m != nil {
		event.Request = truncate(m[1], 200)
	} else if m := modsecURIPatten.FindStringSubmatch(line); m != nil {
		event.Request = truncate(m[1], 200)
	}
	if event.Kind != "dos_limited" {
		if m := ruleIDPattern.FindStringSubmatch(line); m != nil {
			event.RuleID = m[1]
		}
		if m := ruleMsgPattern.FindStringSubmatch(line); m != nil {
			event.Message = truncate(m[1], 160)
		}
	}
	return event, true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
