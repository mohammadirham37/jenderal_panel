package depaudit

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	osvBaseURL      = "https://api.osv.dev"
	osvBatchSize    = 1000
	osvMaxResponse  = 32 << 20
	osvTimeout      = 30 * time.Second
	osvMaxPageTurns = 20
)

// Vuln is the part of an OSV record the audit uses.
type Vuln struct {
	ID       string   `json:"id"`
	Modified string   `json:"modified"`
	Summary  string   `json:"summary"`
	Aliases  []string `json:"aliases"`
	Severity []struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	} `json:"severity"`
	DatabaseSpecific struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
	Affected []struct {
		Package struct {
			Ecosystem string `json:"ecosystem"`
			Name      string `json:"name"`
		} `json:"package"`
		Ranges []struct {
			Type   string              `json:"type"`
			Events []map[string]string `json:"events"`
		} `json:"ranges"`
	} `json:"affected"`
	References []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"references"`
}

type pkgKey struct{ Ecosystem, Name, Version string }

type vulnRef struct{ ID, Modified string }

type osvClient struct {
	http    *http.Client
	baseURL string
	db      *sql.DB
	now     func() time.Time
}

func newOSVClient(db *sql.DB) *osvClient {
	return &osvClient{
		http:    &http.Client{Timeout: osvTimeout},
		baseURL: osvBaseURL,
		db:      db,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

type osvQuery struct {
	Package struct {
		Ecosystem string `json:"ecosystem"`
		Name      string `json:"name"`
	} `json:"package"`
	Version   string `json:"version"`
	PageToken string `json:"page_token,omitempty"`
}

type osvBatchResponse struct {
	Results []struct {
		Vulns         []vulnRef `json:"vulns"`
		NextPageToken string    `json:"next_page_token"`
	} `json:"results"`
}

// query returns the vulnerabilities affecting each package version.
func (c *osvClient) query(ctx context.Context, pkgs []pkgKey) (map[pkgKey][]vulnRef, error) {
	out := map[pkgKey][]vulnRef{}
	for start := 0; start < len(pkgs); start += osvBatchSize {
		end := min(start+osvBatchSize, len(pkgs))
		pending := map[pkgKey]string{}
		for _, p := range pkgs[start:end] {
			pending[p] = ""
		}
		for turn := 0; len(pending) > 0; turn++ {
			if turn > osvMaxPageTurns {
				return nil, fmt.Errorf("OSV pagination did not finish")
			}
			keys := make([]pkgKey, 0, len(pending))
			queries := make([]osvQuery, 0, len(pending))
			for _, p := range pkgs[start:end] {
				token, ok := pending[p]
				if !ok {
					continue
				}
				var q osvQuery
				q.Package.Ecosystem, q.Package.Name, q.Version, q.PageToken = p.Ecosystem, p.Name, p.Version, token
				keys = append(keys, p)
				queries = append(queries, q)
			}
			var resp osvBatchResponse
			if err := c.post(ctx, "/v1/querybatch", map[string]any{"queries": queries}, &resp); err != nil {
				return nil, err
			}
			if len(resp.Results) != len(keys) {
				return nil, fmt.Errorf("OSV answered %d results for %d queries", len(resp.Results), len(keys))
			}
			next := map[pkgKey]string{}
			for i, r := range resp.Results {
				out[keys[i]] = append(out[keys[i]], r.Vulns...)
				if r.NextPageToken != "" {
					next[keys[i]] = r.NextPageToken
				}
			}
			pending = next
		}
	}
	return out, nil
}

// vuln returns an OSV record, from the cache when it has not changed.
func (c *osvClient) vuln(ctx context.Context, ref vulnRef) (*Vuln, error) {
	var modified, data string
	err := c.db.QueryRowContext(ctx, `SELECT modified, data FROM osv_vulns WHERE id = ?`, ref.ID).Scan(&modified, &data)
	if err == nil && (ref.Modified == "" || modified == ref.Modified) {
		var v Vuln
		if json.Unmarshal([]byte(data), &v) == nil {
			return &v, nil
		}
	}
	var v Vuln
	raw, err := c.get(ctx, "/v1/vulns/"+url.PathEscape(ref.ID))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("decode OSV %s: %w", ref.ID, err)
	}
	if v.Modified == "" {
		v.Modified = ref.Modified
	}
	stored, _ := json.Marshal(v)
	_, _ = c.db.ExecContext(ctx, `INSERT INTO osv_vulns (id, modified, data, fetched_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET modified = excluded.modified, data = excluded.data, fetched_at = excluded.fetched_at`,
		ref.ID, v.Modified, string(stored), c.now().Format(time.RFC3339))
	return &v, nil
}

func (c *osvClient) post(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	raw, err := c.do(req)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode OSV response: %w", err)
	}
	return nil
}

func (c *osvClient) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *osvClient) do(req *http.Request) ([]byte, error) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "jenderal-panel-depaudit")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OSV request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, osvMaxResponse))
	if err != nil {
		return nil, fmt.Errorf("read OSV response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OSV %s: HTTP %d", req.URL.Path, resp.StatusCode)
	}
	return raw, nil
}

// severity prefers the GitHub advisory label, then a CVSS v3 vector.
func (v *Vuln) severity() string {
	switch strings.ToUpper(v.DatabaseSpecific.Severity) {
	case "CRITICAL":
		return "critical"
	case "HIGH":
		return "high"
	case "MODERATE", "MEDIUM":
		return "medium"
	case "LOW":
		return "low"
	}
	best := -1.0
	for _, s := range v.Severity {
		if score, ok := CVSS3BaseScore(s.Score); ok && score > best {
			best = score
		}
	}
	if best >= 0 {
		return severityFromScore(best)
	}
	return "unknown"
}

// fixedFor returns the version that fixes the installed one: the "fixed"
// event of the range containing it, else the smallest fixed version above
// it, else "".
func (v *Vuln) fixedFor(ecosystem, name, version string) string {
	smallest := ""
	for _, a := range v.Affected {
		if a.Package.Ecosystem != ecosystem || !strings.EqualFold(a.Package.Name, name) {
			continue
		}
		for _, r := range a.Ranges {
			if r.Type != "ECOSYSTEM" && r.Type != "SEMVER" {
				continue
			}
			introduced := ""
			for _, ev := range r.Events {
				if in, ok := ev["introduced"]; ok {
					introduced = in
				}
				fixed, ok := ev["fixed"]
				if !ok {
					continue
				}
				if (introduced == "" || introduced == "0" || compareVersions(version, introduced) >= 0) &&
					compareVersions(version, fixed) < 0 {
					return fixed
				}
				if compareVersions(fixed, version) > 0 && (smallest == "" || compareVersions(fixed, smallest) < 0) {
					smallest = fixed
				}
			}
		}
	}
	return smallest
}

// advisoryURL is the advisory reference, else the OSV page.
func (v *Vuln) advisoryURL() string {
	for _, r := range v.References {
		if r.Type == "ADVISORY" && strings.HasPrefix(r.URL, "https://") {
			return r.URL
		}
	}
	return "https://osv.dev/vulnerability/" + url.PathEscape(v.ID)
}

// cveAliases returns the CVE ids among the aliases.
func (v *Vuln) cveAliases() []string {
	var out []string
	for _, a := range v.Aliases {
		if strings.HasPrefix(a, "CVE-") {
			out = append(out, a)
		}
	}
	return out
}

// compareVersions compares dotted numeric versions with an optional
// pre-release ("1.0.0-beta.2" < "1.0.0"). Non-numeric segments compare as
// strings.
func compareVersions(a, b string) int {
	a, b = strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v")
	a, _, _ = strings.Cut(a, "+")
	b, _, _ = strings.Cut(b, "+")
	aMain, aPre, aHasPre := strings.Cut(a, "-")
	bMain, bPre, bHasPre := strings.Cut(b, "-")
	if c := compareSegments(strings.Split(aMain, "."), strings.Split(bMain, ".")); c != 0 {
		return c
	}
	switch {
	case aHasPre && !bHasPre:
		return -1
	case !aHasPre && bHasPre:
		return 1
	case !aHasPre && !bHasPre:
		return 0
	}
	return compareSegments(strings.Split(aPre, "."), strings.Split(bPre, "."))
}

func compareSegments(a, b []string) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		xn, xErr := strconv.Atoi(x)
		yn, yErr := strconv.Atoi(y)
		if x == "" {
			xn, xErr = 0, nil
		}
		if y == "" {
			yn, yErr = 0, nil
		}
		switch {
		case xErr == nil && yErr == nil:
			if xn != yn {
				if xn < yn {
					return -1
				}
				return 1
			}
		case x != y:
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
