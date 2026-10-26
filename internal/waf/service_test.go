package waf

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// fakeExec simulates the root-side filesystem and package state the service
// touches: files map drives test/cat/rm/tee, dpkgOK drives dpkg-query.
type fakeExec struct {
	files   map[string]string
	dpkgOK  map[string]bool
	calls   [][]string
	nginxOK bool
	// included, when set, is a file the vhost includes: nginx -t fails
	// while it is missing.
	included string
}

func newFakeExec() *fakeExec {
	return &fakeExec{
		files:   map[string]string{},
		dpkgOK:  map[string]bool{},
		nginxOK: true,
	}
}

func (f *fakeExec) Run(ctx context.Context, name string, args ...string) (*executor.Result, error) {
	return f.RunSudo(ctx, name, args...)
}

func (f *fakeExec) RunSudo(ctx context.Context, name string, args ...string) (*executor.Result, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	switch name {
	case "dpkg-query":
		if f.dpkgOK[args[len(args)-1]] {
			return &executor.Result{ExitCode: 0, Stdout: "install ok installed"}, nil
		}
		return &executor.Result{ExitCode: 1}, nil
	case "test":
		if _, ok := f.files[args[len(args)-1]]; ok {
			return &executor.Result{ExitCode: 0}, nil
		}
		return &executor.Result{ExitCode: 1}, nil
	case "cat":
		if content, ok := f.files[args[len(args)-1]]; ok {
			return &executor.Result{ExitCode: 0, Stdout: content}, nil
		}
		return &executor.Result{ExitCode: 1}, nil
	case "cp":
		if content, ok := f.files[args[0]]; ok {
			f.files[args[1]] = content
		}
		return &executor.Result{ExitCode: 0}, nil
	case "rm":
		delete(f.files, args[len(args)-1])
		return &executor.Result{ExitCode: 0}, nil
	case "/usr/sbin/nginx":
		if _, ok := f.files[f.included]; f.included != "" && !ok {
			return &executor.Result{ExitCode: 1, Stderr: "open() \"" + f.included + "\" failed (2: No such file or directory)"}, nil
		}
		if f.nginxOK {
			return &executor.Result{ExitCode: 0}, nil
		}
		return &executor.Result{ExitCode: 1, Stderr: "nginx: configuration file test failed"}, nil
	case "/usr/bin/systemctl":
		return &executor.Result{ExitCode: 0}, nil
	}
	return &executor.Result{ExitCode: 0}, nil
}

func (f *fakeExec) RunSudoWithInput(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if name == "tee" {
		f.files[args[len(args)-1]] = input
	}
	return &executor.Result{ExitCode: 0}, nil
}

func (f *fakeExec) RunSudoStream(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (f *fakeExec) RunSudoStreamSplit(context.Context, io.Writer, io.Writer, string, ...string) (int, error) {
	return 0, nil
}

func (f *fakeExec) RunSudoWithInputStream(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (f *fakeExec) called(command string) bool {
	for _, call := range f.calls {
		if call[0] == command {
			return true
		}
	}
	return false
}

func TestStatusDetectsInstalledEnabledAndMode(t *testing.T) {
	fake := newFakeExec()
	fake.dpkgOK[connectorPackage] = true
	fake.dpkgOK[crsPackage] = true
	fake.files[crsRulesDir] = "dir"
	fake.files[coreConfig] = "core"
	fake.files[crsSetup] = "setup"
	fake.files[rulesFile] = "Include /etc/modsecurity/modsecurity.conf\nSecRuleEngine On\n"
	fake.files[dosFile] = "limit_req_zone $binary_remote_addr zone=jenderal_dos:10m rate=60r/m;\n"

	status, err := newFakeService(fake).Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.ModSecurity.Installed {
		t.Errorf("modsecurity installed = %v, want true", status.ModSecurity.Installed)
	}
	if status.ModSecurity.Mode != ModeBlocking {
		t.Errorf("mode = %q, want blocking", status.ModSecurity.Mode)
	}
	if !status.DoS.Defaults || status.DoS.RequestsPerMinute != 60 {
		t.Errorf("dos status = %+v, want defaults with 60r/m", status.DoS)
	}
}

func TestEnableWritesNginxIncludeAndRulesFile(t *testing.T) {
	fake := newFakeExec()
	fake.dpkgOK[connectorPackage] = true
	fake.dpkgOK[crsPackage] = true
	fake.files[crsRulesDir] = "dir"
	fake.files[coreConfig] = "core"
	fake.files[crsSetup] = "setup"

	svc := &Service{exec: fake}
	siteID := "01M247E0A6H332PZNCS9AQGXFQ"
	if err := svc.SetSiteProtection(context.Background(), siteID, SiteProtection{WAF: true, DoS: true}, 120, 20); err != nil {
		t.Fatalf("SetSiteProtection: %v", err)
	}

	rules := fake.files[rulesFile]
	for _, want := range []string{"Include /etc/modsecurity/modsecurity.conf", "Include " + crsSetupManaged, "Include /usr/share/modsecurity-crs/rules/*.conf", "SecRuleEngine On", "SecAuditLog /var/log/nginx/modsec_audit.log", "SecTmpDir /tmp"} {
		if !strings.Contains(rules, want) {
			t.Errorf("rules file missing %q:\n%s", want, rules)
		}
	}
	if !strings.Contains(fake.files[crsSetupManaged], "setvar:tx.crs_setup_version=335") {
		t.Fatalf("managed CRS setup must set tx.crs_setup_version:\n%s", fake.files[crsSetupManaged])
	}
	hook := fake.files[fmt.Sprintf(siteHookFormat, siteID)]
	for _, want := range []string{"modsecurity on;", "modsecurity_rules_file "+rulesFile, "limit_req zone=jenderal_dos burst=20 nodelay;"} {
		if !strings.Contains(hook, want) {
			t.Errorf("site hook missing %q:\n%s", want, hook)
		}
	}
	if !fake.called("/usr/sbin/nginx") || !fake.called("/usr/bin/systemctl") {
		t.Error("enable must validate with nginx -t and reload nginx")
	}

	// Both off leaves the (always included) file without directives.
	if err := svc.SetSiteProtection(context.Background(), siteID, SiteProtection{}, 120, 20); err != nil {
		t.Fatalf("SetSiteProtection(off): %v", err)
	}
	if state := siteProtectionFromHook(fake.files[fmt.Sprintf(siteHookFormat, siteID)]); state.WAF || state.DoS {
		t.Error("protection must be off after disabling both")
	}
}

func TestEnableRollsBackWhenNginxConfigBroken(t *testing.T) {
	fake := newFakeExec()
	fake.dpkgOK[connectorPackage] = true
	fake.dpkgOK[crsPackage] = true
	fake.files[crsRulesDir] = "dir"
	fake.nginxOK = false

	svc := &Service{exec: fake}
	siteID := "01M247E0A6H332PZNCS9AQGXFQ"
	if err := svc.SetSiteProtection(context.Background(), siteID, SiteProtection{WAF: true, DoS: true}, 120, 20); err == nil {
		t.Fatal("expected error when nginx -t fails")
	}
	if state := siteProtectionFromHook(fake.files[fmt.Sprintf(siteHookFormat, siteID)]); state.WAF || state.DoS {
		t.Error("broken config must be rolled back")
	}
}

func TestConfigureDoSValidatesAndWritesRateLimit(t *testing.T) {
	svc := &Service{exec: newFakeExec()}
	if err := svc.SetDoSDefaults(context.Background(), 5, 10); err == nil {
		t.Error("expected rejection of requests_per_minute below 10")
	}
	if err := svc.SetDoSDefaults(context.Background(), 120, 0); err == nil {
		t.Error("expected rejection of burst below 1")
	}

	fake := newFakeExec()
	svc.exec = fake
	if err := svc.SetDoSDefaults(context.Background(), 120, 20); err != nil {
		t.Fatalf("ConfigureDoS: %v", err)
	}
	content := fake.files[dosFile]
	for _, want := range []string{"rate=120r/m", "zone=jenderal_dos:10m"} {
		if !strings.Contains(content, want) {
			t.Errorf("dos conf missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "limit_req zone=") {
		t.Errorf("dos defaults conf must not enforce globally:\n%s", content)
	}
}

// A per-site toggle is available to non-admin site owners, so it must never
// change the shared zone rate: an existing zone file is left untouched.
func TestSiteProtectionNeverReratesSharedZone(t *testing.T) {
	fake := newFakeExec()
	fake.files[dosFile] = "# Managed by Jenderal Panel - DoS protection rate limit zone\nlimit_req_zone $binary_remote_addr zone=jenderal_dos:10m rate=60r/m;\n"

	svc := &Service{exec: fake}
	if err := svc.SetSiteProtection(context.Background(), "01M247E0A6H332PZNCS9AQGXFQ", SiteProtection{DoS: true}, 55555, 20); err != nil {
		t.Fatalf("SetSiteProtection: %v", err)
	}
	if !strings.Contains(fake.files[dosFile], "rate=60r/m") {
		t.Errorf("per-site toggle rewrote the shared zone rate:\n%s", fake.files[dosFile])
	}
}

// First ever enable bootstraps the shared zone with the fallback rate.
func TestSiteProtectionCreatesMissingZone(t *testing.T) {
	fake := newFakeExec()
	svc := &Service{exec: fake}
	if err := svc.SetSiteProtection(context.Background(), "01M247E0A6H332PZNCS9AQGXFQ", SiteProtection{DoS: true}, 120, 20); err != nil {
		t.Fatalf("SetSiteProtection: %v", err)
	}
	if !strings.Contains(fake.files[dosFile], "rate=120r/m") {
		t.Errorf("missing zone was not bootstrapped:\n%s", fake.files[dosFile])
	}
	if strings.Contains(fake.files[dosFile], "limit_req zone=") {
		t.Errorf("zone conf must not enforce globally:\n%s", fake.files[dosFile])
	}
}

func newFakeService(fake *fakeExec) *Service {
	return &Service{exec: fake}
}

// Environment-carrying variants behave like their plain counterparts here;
// the panel only uses them to keep secrets out of argv.
func (f *fakeExec) RunSudoWithEnv(ctx context.Context, env map[string]string, name string, args ...string) (*executor.Result, error) {
	return f.RunSudo(ctx, name, args...)
}

func (f *fakeExec) RunSudoWithInputStreamEnv(ctx context.Context, env map[string]string, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
	return f.RunSudoWithInputStream(ctx, stdin, stderrW, name, args...)
}

// The vhost always includes the per-site security file. Turning the last
// protection off used to delete it, nginx -t then failed, and the change
// was rolled back with a generic error.
func TestSiteProtectionCanBeTurnedOffAgain(t *testing.T) {
	const site = "01M247E0A6H332PZNCS9AQGXFQ"
	hook := fmt.Sprintf(siteHookFormat, site)
	fake := newFakeExec()
	fake.included = hook
	fake.files[hook] = "# Jenderal Traffic Guard: observe configuration not enabled yet\n"
	svc := &Service{exec: fake}
	ctx := context.Background()

	if err := svc.SetSiteProtection(ctx, site, SiteProtection{DoS: true}, 120, 20); err != nil {
		t.Fatalf("enable DoS: %v", err)
	}
	if err := svc.SetSiteProtection(ctx, site, SiteProtection{}, 120, 20); err != nil {
		t.Fatalf("disable DoS: %v", err)
	}
	content, ok := fake.files[hook]
	if !ok {
		t.Fatal("the included file must stay in place")
	}
	if state := siteProtectionFromHook(content); state.DoS || state.WAF {
		t.Errorf("protection still on after disabling: %q", content)
	}
}

// Traffic Guard and the WAF page share the website's include file; turning
// DoS on or off must keep the Traffic Guard limits, and both together must
// stay valid nginx (one limit_req_status).
func TestSiteProtectionKeepsTrafficGuardLimits(t *testing.T) {
	const site = "01M247E0A6H332PZNCS9AQGXFQ"
	hook := fmt.Sprintf(siteHookFormat, site)
	fake := newFakeExec()
	fake.included = hook
	fake.files[hook] = "# Managed by Jenderal Panel - HTTP-layer protection only\nlimit_req zone=jenderal_balanced burst=20 nodelay;\nlimit_conn jenderal_connections 20;\nlimit_req_status 429;\nlimit_conn_status 429;\n"
	svc := &Service{exec: fake}
	ctx := context.Background()

	if err := svc.SetSiteProtection(ctx, site, SiteProtection{DoS: true}, 120, 20); err != nil {
		t.Fatal(err)
	}
	combined := fake.files[hook]
	for _, want := range []string{"zone=jenderal_balanced", "zone=jenderal_dos", "limit_conn jenderal_connections"} {
		if !strings.Contains(combined, want) {
			t.Errorf("combined include lost %q:\n%s", want, combined)
		}
	}
	if n := strings.Count(combined, "limit_req_status"); n != 1 {
		t.Errorf("limit_req_status appears %d times:\n%s", n, combined)
	}

	if err := svc.SetSiteProtection(ctx, site, SiteProtection{}, 120, 20); err != nil {
		t.Fatal(err)
	}
	combined = fake.files[hook]
	if strings.Contains(combined, "jenderal_dos") || !strings.Contains(combined, "zone=jenderal_balanced") {
		t.Errorf("turning DoS off must keep Traffic Guard and drop DoS:\n%s", combined)
	}
}
