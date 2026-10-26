package waf

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

const sampleErrorLog = `2026/10/26 09:00:00 [error] 811#811: *1 limiting requests, excess: 20.900 by zone "jenderal_dos", client: 203.0.113.9, server: shop.test, request: "GET /old HTTP/1.1", host: "shop.test"
2026/10/26 11:58:01 [error] 811#811: *2 limiting requests, excess: 20.500 by zone "jenderal_dos", client: 203.0.113.9, server: shop.test, request: "GET / HTTP/1.1", host: "shop.test"
2026/10/26 11:58:02 [error] 811#811: *3 limiting requests, excess: 21.000 by zone "jenderal_dos", client: 203.0.113.9, server: shop.test, request: "GET /cart HTTP/1.1", host: "shop.test"
2026/10/26 11:58:03 [error] 811#811: *4 limiting requests, dry run, excess: 5.000 by zone "jenderal_dos", client: 198.51.100.1, server: shop.test, request: "GET / HTTP/1.1", host: "shop.test"
2026/10/26 11:58:04 [error] 811#811: *5 limiting requests, excess: 9.000 by zone "jenderal_balanced", client: 198.51.100.2, server: shop.test, request: "GET / HTTP/1.1", host: "shop.test"
2026/10/26 11:59:00 [warn] 811#811: *6 [client 198.51.100.7] ModSecurity: Warning. detected SQLi using libinjection. [file "/usr/share/modsecurity-crs/rules/REQUEST-942-APPLICATION-ATTACK-SQLI.conf"] [line "65"] [id "942100"] [rev ""] [msg "SQL Injection Attack Detected via libinjection"] [data "Matched Data"] [severity "2"] [hostname "shop.test"] [uri "/product"] [unique_id "AAA111"] [ref "v28,7"], client: 198.51.100.7, server: shop.test, request: "GET /product?id=1%27%20or%201=1 HTTP/1.1", host: "shop.test"
2026/10/26 11:59:00 [error] 811#811: *6 [client 198.51.100.7] ModSecurity: Access denied with code 403 (phase 2). Matched "Operator Ge' with parameter 5" [id "949110"] [msg "Inbound Anomaly Score Exceeded (Total Score: 5)"] [hostname "shop.test"] [uri "/product"] [unique_id "AAA111"], client: 198.51.100.7, server: shop.test, request: "GET /product?id=1%27%20or%201=1 HTTP/1.1", host: "shop.test"
2026/10/26 11:59:30 [warn] 811#811: *7 [client 198.51.100.8] ModSecurity: Warning. Matched "Operator Rx" [id "920350"] [msg "Host header is a numeric IP address"] [uri "/"] [unique_id "BBB222"], client: 198.51.100.8, server: shop.test, request: "GET / HTTP/1.1", host: "1.2.3.4"
2026/10/26 11:59:40 [error] 811#811: *8 open() "/home/web_shop/app/public/favicon.ico" failed (2: No such file or directory), client: 198.51.100.9, server: shop.test
`

func TestParseSiteReport(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	report := parseSiteReport(sampleErrorLog, 1, now, time.UTC, SiteReport{Hours: 1})

	// The 09:00 entry is outside the hour, the dry run and the Traffic Guard
	// zone are not DoS blocks.
	if report.DoSLimited != 2 {
		t.Errorf("dos limited = %d, want 2", report.DoSLimited)
	}
	// Warning + denial with one unique_id is one blocked request.
	if report.WAFBlocked != 1 || report.WAFDetected != 1 {
		t.Errorf("waf blocked=%d detected=%d, want 1 and 1", report.WAFBlocked, report.WAFDetected)
	}
	var blocked *ReportEvent
	for i := range report.Recent {
		if report.Recent[i].Kind == "waf_blocked" {
			blocked = &report.Recent[i]
		}
	}
	if blocked == nil || blocked.RuleID != "942100" || !strings.Contains(blocked.Message, "SQL Injection") {
		t.Errorf("the attack rule, not the anomaly score, must be reported: %+v", blocked)
	}
	if len(report.TopIPs) == 0 || report.TopIPs[0].Key != "203.0.113.9" || report.TopIPs[0].Count != 2 {
		t.Errorf("top ips = %+v", report.TopIPs)
	}
	if len(report.Recent) != 4 || !report.Recent[0].Time.After(report.Recent[len(report.Recent)-1].Time) {
		t.Errorf("recent must be newest first: %+v", report.Recent)
	}
}

func TestSiteReportReadsOnlyAWebsiteLog(t *testing.T) {
	svc := &Service{exec: &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 1}, nil
		},
	}}
	if _, err := svc.SiteReport(context.Background(), "/etc/shadow", 24, time.Now()); err == nil {
		t.Error("a non-website log path must be refused")
	}
	report, err := svc.SiteReport(context.Background(), "/home/web_shop/logs/error.log", 24, time.Now())
	if err != nil || report.LogAvailable {
		t.Errorf("a missing log is an empty report: %+v %v", report, err)
	}
}
