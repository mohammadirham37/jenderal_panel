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
	// In blocking mode the site error log only keeps the final denial (the
	// CRS anomaly-score rule); the rules that actually matched are logged
	// at "warn" and reach only the ModSecurity audit log.
	causes := map[string][]ruleCause{}
	if audit, err := s.exec.RunSudo(ctx, "tail", "-n", fmt.Sprint(auditLogLines), "--", auditLog); err == nil && audit != nil && audit.ExitCode == 0 {
		causes = parseAuditCauses(audit.Stdout)
	}
	return parseSiteReport(res.Stdout, hours, now, time.Local, report, causes), nil
}

const auditLogLines = 50000

// ruleCause is one rule that matched a request.
type ruleCause struct{ id, msg string }

// isScoringRule reports CRS rules that only add up or report anomaly
// scores (949xxx/959xxx blocking evaluation, 980xxx correlation): they are
// never the reason a request was suspicious, and excluding them would turn
// blocking off for the whole website.
func isScoringRule(id string) bool {
	return anomalyRules[id] || strings.HasPrefix(id, "949") || strings.HasPrefix(id, "959") || strings.HasPrefix(id, "980")
}

// parseAuditCauses maps each request (unique_id) to the attack rules that
// matched it, from ModSecurity audit log messages.
func parseAuditCauses(audit string) map[string][]ruleCause {
	causes := map[string][]ruleCause{}
	for _, line := range strings.Split(audit, "\n") {
		if !strings.Contains(line, "[unique_id \"") || !strings.Contains(line, "[id \"") {
			continue
		}
		uid := uniqueIDPattern.FindStringSubmatch(line)
		id := ruleIDPattern.FindStringSubmatch(line)
		if uid == nil || id == nil || isScoringRule(id[1]) {
			continue
		}
		msg := ""
		if m := ruleMsgPattern.FindStringSubmatch(line); m != nil {
			msg = truncate(m[1], 160)
		}
		dup := false
		for _, c := range causes[uid[1]] {
			if c.id == id[1] {
				dup = true
			}
		}
		if !dup {
			causes[uid[1]] = append(causes[uid[1]], ruleCause{id: id[1], msg: msg})
		}
	}
	return causes
}

func parseSiteReport(log string, hours int, now time.Time, loc *time.Location, report SiteReport, causes map[string][]ruleCause) SiteReport {
	since := now.Add(-time.Duration(hours) * time.Hour)
	rules := map[string]*CountEntry{}
	ips := map[string]int{}
	var events []ReportEvent

	// ModSecurity logs one line per matching rule plus the final denial;
	// lines sharing a unique_id are one request.
	var parsed []ReportEvent
	var parsedUID []string
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
		parsedUID = append(parsedUID, uid)
	}

	for i, event := range parsed {
		// Report the attack rules from the audit log instead of the
		// anomaly-score rule that issued the denial.
		if found := causes[parsedUID[i]]; len(found) > 0 && (event.RuleID == "" || isScoringRule(event.RuleID)) {
			event.RuleID, event.Message = found[0].id, found[0].msg
			parsed[i] = event
			for _, extra := range found[1:] {
				entry := rules[extra.id]
				if entry == nil {
					entry = &CountEntry{Key: extra.id, Label: extra.msg}
					rules[extra.id] = entry
				}
				entry.Count++
			}
		}
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
