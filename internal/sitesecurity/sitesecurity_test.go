package sitesecurity

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type memFiles struct {
	values map[string]string
	failOn string
}

func (m *memFiles) Read(_ context.Context, path string) (string, bool, error) {
	v, ok := m.values[path]
	return v, ok, nil
}

func (m *memFiles) Write(_ context.Context, path, content string) error {
	if path == m.failOn {
		return errors.New("write failed")
	}
	m.values[path] = content
	return nil
}

func (m *memFiles) Remove(_ context.Context, path string) error {
	delete(m.values, path)
	return nil
}

const site = "01SITE"

const trafficEnforce = "# Managed by Jenderal Panel - HTTP-layer protection only\nreal_ip_header CF-Connecting-IP;\nlimit_req zone=jenderal_balanced burst=20 nodelay;\nlimit_conn jenderal_connections 20;\nlimit_req_status 429;\nlimit_conn_status 429;\n"
const trafficObserve = trafficEnforce + "limit_req_dry_run on;\nlimit_conn_dry_run on;\n"
const wafDoS = "# Managed by Jenderal Panel - per-site protection\nmodsecurity on;\nmodsecurity_rules_file /etc/nginx/modsec/main.conf;\nlimit_req_status 403;\nlimit_req zone=jenderal_dos burst=20 nodelay;\n"

func TestComposeKeepsBothAndEmitsStatusOnce(t *testing.T) {
	out := Compose(trafficEnforce, wafDoS)
	for _, want := range []string{"real_ip_header", "zone=jenderal_balanced", "zone=jenderal_dos", "modsecurity on;", "limit_conn jenderal_connections"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "limit_req_status") != 1 || strings.Count(out, "limit_conn_status") != 1 {
		t.Errorf("status directives must appear once:\n%s", out)
	}
	if strings.Contains(out, "limit_req_status 403") {
		t.Errorf("one status for every limit:\n%s", out)
	}
}

// limit_req_dry_run would turn DoS enforcement into a dry run too.
func TestComposePausesObserveLimitsWhileDoSEnforces(t *testing.T) {
	out := Compose(trafficObserve, wafDoS)
	if strings.Contains(out, "dry_run") || strings.Contains(out, "jenderal_balanced") {
		t.Errorf("observe-mode limits must be paused while DoS enforces:\n%s", out)
	}
	if !strings.Contains(out, "zone=jenderal_dos") || !strings.Contains(out, "real_ip_header") {
		t.Errorf("DoS and real IP must stay:\n%s", out)
	}
	// Without DoS the observe mode works as before.
	if out := Compose(trafficObserve, ""); !strings.Contains(out, "limit_req_dry_run on;") {
		t.Errorf("observe mode lost without DoS:\n%s", out)
	}
}

func TestUpdateAdoptsALegacyIncludeAndCanUndo(t *testing.T) {
	files := &memFiles{values: map[string]string{IncludePath(site): trafficEnforce}}
	undo, err := Update(context.Background(), files, site, PartWAF, wafDoS)
	if err != nil {
		t.Fatal(err)
	}
	if files.values[fragmentPath(site, PartTraffic)] != trafficEnforce {
		t.Error("legacy Traffic Guard include not adopted as its fragment")
	}
	if !strings.Contains(files.values[IncludePath(site)], "jenderal_balanced") {
		t.Error("Traffic Guard limits lost")
	}
	if err := undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if files.values[IncludePath(site)] != trafficEnforce {
		t.Errorf("undo did not restore the include:\n%s", files.values[IncludePath(site)])
	}
	if _, ok := files.values[fragmentPath(site, PartWAF)]; ok {
		t.Error("undo left the new WAF fragment")
	}
}

func TestUpdateClearingTheLastPartKeepsAValidInclude(t *testing.T) {
	files := &memFiles{values: map[string]string{}}
	if _, err := Update(context.Background(), files, site, PartWAF, wafDoS); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(context.Background(), files, site, PartWAF, ""); err != nil {
		t.Fatal(err)
	}
	include, ok := files.values[IncludePath(site)]
	if !ok {
		t.Fatal("the always-included file must exist")
	}
	if len(directives(include)) != 0 {
		t.Errorf("include should carry no directives:\n%s", include)
	}
}

func TestUpdateRejectsBadInput(t *testing.T) {
	files := &memFiles{values: map[string]string{}}
	if _, err := Update(context.Background(), files, "../etc", PartWAF, ""); err == nil {
		t.Error("path-like id accepted")
	}
	if _, err := Update(context.Background(), files, site, "other", ""); err == nil {
		t.Error("unknown part accepted")
	}
}
