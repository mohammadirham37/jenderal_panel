package nodejs

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// securityBaselines is the lowest patch release per Node.js major that
// carries the latest known security fixes. `nvm install <major>` always
// resolves to the newest patch, so updating a runtime reaches at least this.
//
// 2026-10-21: v22.23.4 / v24.22.1 / v26.12.1 fix 10 CVEs, including the
// Brotli decoder out-of-bounds write (CVE-2026-111497, RCE) and TLS hostname
// verification bypasses (CVE-2026-105483, CVE-2026-106069).
// https://nodejs.org/en/blog/vulnerability/october-2026-security-releases
var securityBaselines = map[string]string{
	"22": "22.23.4",
	"24": "24.22.1",
}

const securityAdvisoryURL = "https://nodejs.org/en/blog/vulnerability/october-2026-security-releases"

// endOfLifeMajors no longer receive security releases.
var endOfLifeMajors = map[string]bool{"20": true}

// annotateSecurity marks an installed runtime that is older than the security
// baseline of its major, and majors that are end of life.
func annotateSecurity(r *RuntimeInfo) {
	r.EndOfLife = endOfLifeMajors[r.SelectedVersion]
	baseline, ok := securityBaselines[r.SelectedVersion]
	if !ok {
		return
	}
	r.SecurityBaseline = baseline
	if r.Installed && r.InstalledVersion != "" && compareNodeVersions(strings.TrimPrefix(r.InstalledVersion, "v"), baseline) < 0 {
		r.Vulnerable = true
		r.AdvisoryURL = securityAdvisoryURL
	}
}

// compareNodeVersions compares dotted numeric versions; -1, 0, or 1.
func compareNodeVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// websiteAppUnit is the systemd unit of a Node website's own app service
// (internal/website appUnitName); it runs through nvm-exec with the major
// version, so a restart picks up the newly installed patch.
func websiteAppUnit(websiteID string) string {
	return "jenderal-app-" + websiteID + ".service"
}

// UpdateRuntime installs the newest patch of the website's selected Node.js
// major, regenerates and restarts its Node apps (ChangeRuntime), then
// restarts the website's own app service if it is running.
func (s *Service) UpdateRuntime(ctx context.Context, websiteID string, log func(string)) error {
	website, err := s.loadWebsiteRuntime(ctx, websiteID)
	if err != nil {
		return err
	}
	if website.NodeVersion == "" {
		return model.NewValidationError(website.Domain + " has no Node.js runtime selected")
	}
	if err := s.ChangeRuntime(ctx, websiteID, website.NodeVersion, log); err != nil {
		return err
	}
	// try-restart is a no-op for units that are not running or do not exist.
	if err := s.runSudoOK(ctx, "systemctl", "try-restart", websiteAppUnit(websiteID)); err != nil {
		return fmt.Errorf("restart website app: %w", err)
	}
	if status, err := s.runtime.Detect(ctx, website.WebUser, website.NodeVersion); err == nil && log != nil {
		log(fmt.Sprintf("%s now runs Node.js %s", website.Domain, status.NodeVersion))
	}
	return nil
}

// UpdateAllRuntimes updates every website that has a Node.js runtime
// selected, continuing past failures and naming the sites that failed.
func (s *Service) UpdateAllRuntimes(ctx context.Context, log func(string)) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, domain FROM websites WHERE COALESCE(node_version, '') != '' ORDER BY domain`)
	if err != nil {
		return fmt.Errorf("list Node.js websites: %w", err)
	}
	type site struct{ id, domain string }
	var sites []site
	for rows.Next() {
		var st site
		if err := rows.Scan(&st.id, &st.domain); err != nil {
			rows.Close()
			return fmt.Errorf("scan Node.js website: %w", err)
		}
		sites = append(sites, st)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("read Node.js websites: %w", err)
	}

	var failed []string
	for _, st := range sites {
		if log != nil {
			log("==> " + st.domain)
		}
		if err := s.UpdateRuntime(ctx, st.id, log); err != nil {
			failed = append(failed, st.domain)
			if log != nil {
				log("ERROR: " + err.Error())
			}
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("Node.js update failed for %s", strings.Join(failed, ", "))
	}
	return nil
}
