package depaudit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Scan statuses and triggers.
const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"

	TriggerSchedule = "schedule"
	TriggerManual   = "manual"

	scanInterval = 24 * time.Hour
	scanTimeout  = 30 * time.Minute
)

// Finding is one vulnerable package version used by a website.
type Finding struct {
	WebsiteID    string   `json:"website_id"`
	Domain       string   `json:"domain"`
	Ecosystem    string   `json:"ecosystem"`
	Package      string   `json:"package"`
	Version      string   `json:"version"`
	Dev          bool     `json:"dev"`
	Lockfile     string   `json:"lockfile"`
	VulnID       string   `json:"vuln_id"`
	Aliases      []string `json:"aliases"`
	Severity     string   `json:"severity"`
	Summary      string   `json:"summary"`
	FixedVersion string   `json:"fixed_version"`
	URL          string   `json:"url"`
}

// Scan is one run of the audit.
type Scan struct {
	ID         string `json:"id"`
	Trigger    string `json:"trigger"`
	Status     string `json:"status"`
	Error      string `json:"error"`
	Sites      int    `json:"sites"`
	Packages   int    `json:"packages"`
	Findings   int    `json:"findings"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

// Report is the latest scan and the findings of the latest completed one.
type Report struct {
	Scan      *Scan     `json:"scan"`
	Completed *Scan     `json:"completed"`
	Findings  []Finding `json:"findings"`
}

// EventSink receives the findings of every completed scan.
type EventSink interface {
	SyncDependencyFindings(ctx context.Context, findings []Finding, now time.Time) error
}

// Service runs dependency audits.
type Service struct {
	db   *sql.DB
	exec executor.CommandExecutor
	osv  *osvClient
	sink EventSink
	now  func() time.Time

	mu      sync.Mutex
	running bool
	// done is closed when the running scan finishes (tests wait on it).
	done chan struct{}
}

// NewService creates the audit service. A scan left running by a stopped
// panel is marked failed.
func NewService(db *sql.DB, exec executor.CommandExecutor) *Service {
	s := &Service{db: db, exec: exec, osv: newOSVClient(db), now: func() time.Time { return time.Now().UTC() }}
	if db != nil {
		_, _ = db.Exec(`UPDATE dependency_scans SET status = ?, error = ?, finished_at = ? WHERE status = ?`,
			StatusFailed, "the panel stopped during the scan", s.now().Format(time.RFC3339), StatusRunning)
	}
	return s
}

// SetEventSink sets where completed scans report their findings.
func (s *Service) SetEventSink(sink EventSink) { s.sink = sink }

// Latest returns the latest scan and the latest completed scan's findings.
func (s *Service) Latest(ctx context.Context) (*Report, error) {
	r := &Report{Findings: []Finding{}}
	var err error
	if r.Scan, err = s.scanWhere(ctx, ``); err != nil {
		return nil, err
	}
	if r.Completed, err = s.scanWhere(ctx, `WHERE status = '`+StatusCompleted+`'`); err != nil {
		return nil, err
	}
	if r.Completed == nil {
		return r, nil
	}
	r.Findings, err = s.findings(ctx, r.Completed.ID)
	return r, err
}

func (s *Service) scanWhere(ctx context.Context, where string) (*Scan, error) {
	var sc Scan
	err := s.db.QueryRowContext(ctx, `SELECT id, trigger, status, error, sites, packages, findings, started_at, finished_at
		FROM dependency_scans `+where+` ORDER BY started_at DESC LIMIT 1`).
		Scan(&sc.ID, &sc.Trigger, &sc.Status, &sc.Error, &sc.Sites, &sc.Packages, &sc.Findings, &sc.StartedAt, &sc.FinishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load dependency scan: %w", err)
	}
	return &sc, nil
}

var severityRank = map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "unknown": 4}

func (s *Service) findings(ctx context.Context, scanID string) ([]Finding, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT website_id, domain, ecosystem, package, version, dev, lockfile,
		vuln_id, aliases, severity, summary, fixed_version, url FROM dependency_findings WHERE scan_id = ?`, scanID)
	if err != nil {
		return nil, fmt.Errorf("load dependency findings: %w", err)
	}
	defer rows.Close()
	out := []Finding{}
	for rows.Next() {
		var f Finding
		var dev int
		var aliases string
		if err := rows.Scan(&f.WebsiteID, &f.Domain, &f.Ecosystem, &f.Package, &f.Version, &dev, &f.Lockfile,
			&f.VulnID, &aliases, &f.Severity, &f.Summary, &f.FixedVersion, &f.URL); err != nil {
			return nil, err
		}
		f.Dev = dev == 1
		f.Aliases = []string{}
		if aliases != "" {
			f.Aliases = strings.Split(aliases, ",")
		}
		out = append(out, f)
	}
	sortFindings(out)
	return out, rows.Err()
}

func sortFindings(f []Finding) {
	sort.SliceStable(f, func(i, j int) bool {
		a, b := f[i], f[j]
		if severityRank[a.Severity] != severityRank[b.Severity] {
			return severityRank[a.Severity] < severityRank[b.Severity]
		}
		if a.Domain != b.Domain {
			return a.Domain < b.Domain
		}
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		return a.VulnID < b.VulnID
	})
}

// StartScan starts a scan in the background and returns its id.
func (s *Service) StartScan(trigger string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return "", model.NewValidationError("a dependency scan is already running")
	}
	id := newScanID()
	if _, err := s.db.Exec(`INSERT INTO dependency_scans (id, trigger, status, started_at) VALUES (?, ?, ?, ?)`,
		id, trigger, StatusRunning, s.now().Format(time.RFC3339Nano)); err != nil {
		return "", fmt.Errorf("record dependency scan: %w", err)
	}
	s.running = true
	s.done = make(chan struct{})
	done := s.done
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
		defer cancel()
		s.runScan(ctx, id)
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
	return id, nil
}

// ScanIfDue starts a scheduled scan when none completed in the last 24
// hours and none is running. It never blocks on the scan.
func (s *Service) ScanIfDue(ctx context.Context, now time.Time) {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if running {
		return
	}
	last, err := s.scanWhere(ctx, `WHERE status = '`+StatusCompleted+`'`)
	if err != nil {
		return
	}
	if last != nil {
		if started, err := time.Parse(time.RFC3339Nano, last.StartedAt); err == nil && now.Sub(started) < scanInterval {
			return
		}
	}
	if _, err := s.StartScan(TriggerSchedule); err != nil {
		log.Printf("dependency audit: %v", err)
	}
}

// wait blocks until the running scan (if any) has finished.
func (s *Service) wait() {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}

type siteDep struct {
	site Site
	dep  Dependency
}

func (s *Service) runScan(ctx context.Context, id string) {
	findings, sites, packages, err := s.collect(ctx)
	finished := s.now().Format(time.RFC3339Nano)
	if err != nil {
		_, _ = s.db.Exec(`UPDATE dependency_scans SET status = ?, error = ?, sites = ?, packages = ?, finished_at = ? WHERE id = ?`,
			StatusFailed, err.Error(), sites, packages, finished, id)
		log.Printf("dependency audit failed: %v", err)
		return
	}
	if err := s.store(id, findings, sites, packages, finished); err != nil {
		_, _ = s.db.Exec(`UPDATE dependency_scans SET status = ?, error = ?, finished_at = ? WHERE id = ?`,
			StatusFailed, err.Error(), finished, id)
		return
	}
	if s.sink != nil {
		if err := s.sink.SyncDependencyFindings(ctx, findings, s.now()); err != nil {
			log.Printf("dependency audit events: %v", err)
		}
	}
}

func (s *Service) collect(ctx context.Context) ([]Finding, int, int, error) {
	siteList, err := s.sites(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	var all []siteDep
	unique := map[pkgKey]bool{}
	var keys []pkgKey
	for _, site := range siteList {
		deps, _, err := siteDependencies(ctx, s.exec, site)
		if err != nil {
			continue
		}
		for _, d := range dedupe(deps) {
			all = append(all, siteDep{site, d})
			k := pkgKey{d.Ecosystem, d.Name, d.Version}
			if !unique[k] {
				unique[k] = true
				keys = append(keys, k)
			}
		}
	}
	if len(keys) == 0 {
		return []Finding{}, len(siteList), 0, nil
	}
	affected, err := s.osv.query(ctx, keys)
	if err != nil {
		return nil, len(siteList), len(keys), err
	}
	vulns := map[string]*Vuln{}
	findings := []Finding{}
	for _, sd := range all {
		k := pkgKey{sd.dep.Ecosystem, sd.dep.Name, sd.dep.Version}
		for _, ref := range affected[k] {
			v, ok := vulns[ref.ID]
			if !ok {
				if v, err = s.osv.vuln(ctx, ref); err != nil {
					return nil, len(siteList), len(keys), err
				}
				vulns[ref.ID] = v
			}
			aliases := v.cveAliases()
			if aliases == nil {
				aliases = []string{}
			}
			findings = append(findings, Finding{
				WebsiteID: sd.site.ID, Domain: sd.site.Domain,
				Ecosystem: sd.dep.Ecosystem, Package: sd.dep.Name, Version: sd.dep.Version,
				Dev: sd.dep.Dev, Lockfile: sd.dep.Lockfile,
				VulnID: v.ID, Aliases: aliases, Severity: v.severity(), Summary: v.Summary,
				FixedVersion: v.fixedFor(sd.dep.Ecosystem, sd.dep.Name, sd.dep.Version), URL: v.advisoryURL(),
			})
		}
	}
	sortFindings(findings)
	return findings, len(siteList), len(keys), nil
}

func (s *Service) sites(ctx context.Context) ([]Site, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, domain, web_user, document_root FROM websites ORDER BY domain`)
	if err != nil {
		return nil, fmt.Errorf("list websites: %w", err)
	}
	defer rows.Close()
	var out []Site
	for rows.Next() {
		var site Site
		if err := rows.Scan(&site.ID, &site.Domain, &site.WebUser, &site.DocumentRoot); err != nil {
			return nil, err
		}
		out = append(out, site)
	}
	return out, rows.Err()
}

// store saves a completed scan and drops older scans and their findings.
func (s *Service) store(id string, findings []Finding, sites, packages int, finished string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO dependency_findings (scan_id, website_id, domain, ecosystem, package, version, dev,
		lockfile, vuln_id, aliases, severity, summary, fixed_version, url) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, f := range findings {
		dev := 0
		if f.Dev {
			dev = 1
		}
		if _, err := stmt.Exec(id, f.WebsiteID, f.Domain, f.Ecosystem, f.Package, f.Version, dev, f.Lockfile,
			f.VulnID, strings.Join(f.Aliases, ","), f.Severity, f.Summary, f.FixedVersion, f.URL); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE dependency_scans SET status = ?, sites = ?, packages = ?, findings = ?, finished_at = ? WHERE id = ?`,
		StatusCompleted, sites, packages, len(findings), finished, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM dependency_findings WHERE scan_id IN (SELECT id FROM dependency_scans WHERE id != ? AND status != ?)`, id, StatusRunning); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM dependency_scans WHERE id != ? AND status != ?`, id, StatusRunning); err != nil {
		return err
	}
	return tx.Commit()
}

func newScanID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
