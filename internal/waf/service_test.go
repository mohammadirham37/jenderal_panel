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

	// Both off removes the include entirely.
	if err := svc.SetSiteProtection(context.Background(), siteID, SiteProtection{}, 120, 20); err != nil {
		t.Fatalf("SetSiteProtection(off): %v", err)
	}
	if _, still := fake.files[fmt.Sprintf(siteHookFormat, siteID)]; still {
		t.Error("hook file must be removed when protection is disabled")
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
	if _, still := fake.files[fmt.Sprintf(siteHookFormat, siteID)]; still {
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
