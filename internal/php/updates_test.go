package php

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

const noblePolicy = `php8.3-common:
  Installed: 8.3.30-1+ubuntu24.04.1+deb.sury.org+1
  Candidate: 8.3.36-1+ubuntu24.04.1+deb.sury.org+1
  Version table:
 *** 8.3.30-1+ubuntu24.04.1+deb.sury.org+1 100
php8.3-fpm:
  Installed: 8.3.30-1+ubuntu24.04.1+deb.sury.org+1
  Candidate: 8.3.36-1+ubuntu24.04.1+deb.sury.org+1
php8.3-opcache:
  Installed: 8.3.30-1+ubuntu24.04.1+deb.sury.org+1
  Candidate: (none)
`

// updatesExecutor fakes a noble server with PHP 8.3 (outdated) and 8.4
// (already patched) installed.
func updatesExecutor(codename string, calls *[]string) *executor.MockExecutor {
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			*calls = append(*calls, name+" "+strings.Join(args, " "))
			switch {
			case name == "/bin/cat":
				return mockResult("VERSION_CODENAME="+codename+"\n", "", 0), nil
			case name == "test":
				if args[1] == "/etc/php/8.3" || args[1] == "/etc/php/8.4" || args[1] == "/etc/php/8.5" {
					return mockResult("", "", 0), nil
				}
				return mockResult("", "", 1), nil
			case name == "/usr/bin/dpkg-query" && strings.HasSuffix(args[2], "php8.3-*"):
				return mockResult("php8.3-common\t8.3.30-1+ubuntu24.04.1+deb.sury.org+1\tii \n"+
					"php8.3-fpm:amd64\t8.3.30-1+ubuntu24.04.1+deb.sury.org+1\tii \n"+
					"php8.3-opcache\t8.3.30-1+ubuntu24.04.1+deb.sury.org+1\tii \n"+
					"php8.3-xdebug\t\tun \n", "", 1), nil
			case name == "/usr/bin/dpkg-query" && strings.HasSuffix(args[2], "php8.4-*"):
				return mockResult("php8.4-common\t8.4.27-1+ubuntu24.04.1+deb.sury.org+1\tii \n", "", 0), nil
			case name == "/usr/bin/dpkg-query" && strings.HasSuffix(args[2], "php8.5-*"):
				return mockResult("php8.5-common\t8.5.4-1ubuntu1\tii \nphp8.5-fpm\t8.5.4-1ubuntu1\tii \n", "", 0), nil
			case name == "/usr/bin/env" && strings.Contains(strings.Join(args, " "), "php8.3-common"):
				return mockResult(noblePolicy, "", 0), nil
			case name == "/usr/bin/env" && strings.Contains(strings.Join(args, " "), "php8.4-common"):
				return mockResult("php8.4-common:\n  Installed: 8.4.27-1+ubuntu24.04.1+deb.sury.org+1\n  Candidate: 8.4.27-1+ubuntu24.04.1+deb.sury.org+1\n", "", 0), nil
			case name == "/usr/bin/env":
				return mockResult("php8.5-common:\n  Installed: 8.5.4-1ubuntu1\n  Candidate: 8.5.4-1ubuntu1.1\n", "", 0), nil
			case name == "/usr/bin/stat":
				return mockResult("1792540800\n", "", 0), nil
			}
			return mockResult("", "", 0), nil
		},
	}
}

func TestPatchOverviewReportsUpdatesAndVulnerability(t *testing.T) {
	var calls []string
	svc := NewService(updatesExecutor("noble", &calls), nil)
	overview, err := svc.PatchOverview(context.Background())
	if err != nil {
		t.Fatalf("PatchOverview: %v", err)
	}
	if len(overview.Versions) != 2 {
		t.Fatalf("versions = %+v, want 8.3 and 8.4", overview.Versions)
	}

	php83 := overview.Versions[0]
	if php83.Version != "8.3" || php83.Current != "8.3.30" || php83.Latest != "8.3.36" {
		t.Errorf("8.3 = %+v", php83)
	}
	if !php83.UpdateAvailable || len(php83.Packages) != 2 || php83.Packages[0].Name != "php8.3-common" || php83.Packages[1].Name != "php8.3-fpm" {
		t.Errorf("8.3 packages = %+v, want common + fpm (opcache has no candidate, xdebug not installed)", php83.Packages)
	}
	if !php83.Vulnerable || php83.SecurityBaseline != "8.3.36" || php83.AdvisoryURL == "" {
		t.Errorf("8.3 should be flagged vulnerable: %+v", php83)
	}

	php84 := overview.Versions[1]
	if php84.UpdateAvailable || php84.Vulnerable || php84.Current != "8.4.27" {
		t.Errorf("8.4 should be patched and up to date: %+v", php84)
	}
	if overview.ListsUpdatedAt == nil || overview.ListsUpdatedAt.Unix() != 1792540800 {
		t.Errorf("lists_updated_at = %v", overview.ListsUpdatedAt)
	}
	for _, call := range calls {
		if strings.Contains(call, "apt-cache policy") && !strings.HasPrefix(call, "/usr/bin/env LC_ALL=C") {
			t.Errorf("apt-cache must run with LC_ALL=C: %q", call)
		}
	}
}

func TestPatchOverviewSkipsBaselineOnUbuntuArchive(t *testing.T) {
	var calls []string
	svc := NewService(updatesExecutor("resolute", &calls), nil)
	overview, err := svc.PatchOverview(context.Background())
	if err != nil {
		t.Fatalf("PatchOverview: %v", err)
	}
	if len(overview.Versions) != 1 {
		t.Fatalf("versions = %+v", overview.Versions)
	}
	php85 := overview.Versions[0]
	// 8.5.4-1ubuntu1.1 is a security backport: an update, but the upstream
	// number never reaches 8.5.12, so the baseline must not apply.
	if !php85.UpdateAvailable || php85.Vulnerable || php85.SecurityBaseline != "" {
		t.Errorf("8.5 on resolute = %+v", php85)
	}
}

func TestUpgradeCommandsOnlyUpgradeInstalledPackages(t *testing.T) {
	var calls []string
	svc := NewService(updatesExecutor("noble", &calls), nil)
	commands, err := svc.upgradeCommands(context.Background(), []string{"8.3", "8.4"})
	if err != nil {
		t.Fatalf("upgradeCommands: %v", err)
	}
	joined := make([]string, len(commands))
	for i, c := range commands {
		joined[i] = strings.Join(c, " ")
	}
	all := strings.Join(joined, "\n")

	if !strings.HasPrefix(joined[0], "bash -c") || !strings.HasPrefix(joined[1], "apt-get update") {
		t.Errorf("first steps must refresh the repository and lists:\n%s", all)
	}
	upgrade := joined[2]
	for _, want := range []string{"install --only-upgrade -y", "--force-confold", "php8.3-common", "php8.3-fpm", "php8.3-opcache", "php8.4-common"} {
		if !strings.Contains(upgrade, want) {
			t.Errorf("upgrade step missing %q: %s", want, upgrade)
		}
	}
	if strings.Contains(upgrade, "xdebug") || strings.Contains(upgrade, "*") {
		t.Errorf("upgrade must list installed packages explicitly: %s", upgrade)
	}
	if joined[len(joined)-1] != "systemctl try-restart php8.3-fpm" || strings.Contains(all, "try-restart php8.4-fpm") {
		t.Errorf("only installed FPM units are restarted:\n%s", all)
	}

	_, err = svc.upgradeCommands(context.Background(), []string{"8.1"})
	var domainErr *model.DomainError
	if !errors.As(err, &domainErr) {
		t.Errorf("upgrading a version without packages should be a validation error, got %v", err)
	}
}

func TestUpstreamVersionAndCompare(t *testing.T) {
	cases := map[string]string{
		"1:8.3.12-1+ubuntu24.04.1+deb.sury.org+1": "8.3.12",
		"8.5.4-1ubuntu1":                          "8.5.4",
		"garbage":                                 "",
	}
	for in, want := range cases {
		if got := upstreamVersion(in); got != want {
			t.Errorf("upstreamVersion(%q) = %q, want %q", in, got, want)
		}
	}
	if compareUpstream("8.3.9", "8.3.36") != -1 || compareUpstream("8.4.27", "8.4.27") != 0 || compareUpstream("8.10.0", "8.9.9") != 1 {
		t.Error("compareUpstream must compare numerically")
	}
}
