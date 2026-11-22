package releaseupgrade

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUpgradeScriptIsValidBash(t *testing.T) {
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(UpgradeScript())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invalid bash: %v\n%s", err, out)
	}
}

func TestOverrideConfigNeverRemovesPackages(t *testing.T) {
	for _, want := range []string{"RemoveObsoletes=no", "PurgeObsoletes=no", "AllowThirdParty=yes", "Pockets=security,updates,proposed,backports,pgdg"} {
		if !strings.Contains(OverrideConfig, want) {
			t.Errorf("override config missing %q", want)
		}
	}
	script := UpgradeScript()
	override := strings.Index(script, `cat > "$OVERRIDE"`)
	upgrade := strings.Index(script, "do-release-upgrade -f DistUpgradeViewNonInteractive")
	if override < 0 || upgrade < 0 || override > upgrade {
		t.Fatal("the override must be written before do-release-upgrade runs")
	}
	if !strings.Contains(script, `rm -f "$OVERRIDE" "$APT_CONF"`) {
		t.Fatal("the override and apt conf must be removed when the script ends")
	}
}

// sandbox runs the real script with every system path redirected into a
// temp dir and the system commands stubbed. upgradeTo is the codename the
// stubbed do-release-upgrade leaves behind; upgradeExit its exit code.
type sandbox struct {
	root string
	t    *testing.T
}

func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"bin", "etc/apt/sources.list.d", "etc/mysql/mysql.conf.d", "etc/update-manager"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s := &sandbox{root: root, t: t}
	s.write("etc/os-release", "VERSION_CODENAME=noble\n")
	s.write("etc/update-manager/release-upgrades", "[DEFAULT]\nPrompt=never\n")
	return s
}

func (s *sandbox) path(rel string) string { return filepath.Join(s.root, rel) }

func (s *sandbox) write(rel, content string) {
	s.t.Helper()
	if err := os.MkdirAll(filepath.Dir(s.path(rel)), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(s.path(rel), []byte(content), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

func (s *sandbox) read(rel string) string {
	b, _ := os.ReadFile(s.path(rel))
	return string(b)
}

func (s *sandbox) stub(name, body string) {
	s.t.Helper()
	if err := os.WriteFile(s.path("bin/"+name), []byte("#!/bin/bash\n"+body), 0o755); err != nil {
		s.t.Fatal(err)
	}
}

func (s *sandbox) run(upgradeTo string, upgradeExit int) {
	s.t.Helper()
	calls := s.path("calls")
	log := func(name string) string { return "echo \"" + name + " $*\" >> " + calls + "\n" }
	s.stub("do-release-upgrade", log("do-release-upgrade")+
		"[ -f "+s.path("etc/update-manager/release-upgrades.d/jenderal-panel.cfg")+" ] && echo override-present >> "+calls+"\n"+
		"echo VERSION_CODENAME="+upgradeTo+" > "+s.path("etc/os-release")+"\n"+
		"exit "+strconv.Itoa(upgradeExit)+"\n")
	// Before the upgrade php8.3-fpm and postgresql-16 are installed; after
	// it postgresql-16 is gone (the case the repair must handle).
	s.stub("dpkg-query", log("dpkg-query")+
		"if [[ \"$*\" == *'php*'* ]]; then printf 'php8.3-fpm\\tinstalled\\npostgresql-16\\tinstalled\\n'; exit 0; fi\n"+
		"if [[ \"$*\" == *'postgresql-16-*'* ]]; then printf 'postgresql-16-pgvector\\tinstalled\\n'; exit 0; fi\n"+
		"case \"${@: -1}\" in php8.3-fpm) printf installed;; *) printf not-installed;; esac\n")
	s.stub("apt-cache", "echo '  Candidate: 16.9-1.pgdg26.04+1'\n")
	s.stub("apt-get", log("apt-get"))
	s.stub("systemctl", log("systemctl"))
	s.stub("nginx", log("nginx"))
	s.stub("bash", "exec /bin/bash \"$@\"\n")

	script := strings.NewReplacer(
		StateDir, s.path("state"),
		LogPath, s.path("log/release-upgrade.log"),
		"/etc/update-manager/", s.path("etc/update-manager")+"/",
		"/etc/apt/", s.path("etc/apt")+"/",
		"/etc/os-release", s.path("etc/os-release"),
		"/etc/mysql/", s.path("etc/mysql")+"/",
		"/etc/postgresql/", s.path("etc/postgresql")+"/",
		"/usr/lib/postgresql/", s.path("usr/lib/postgresql")+"/",
		"/var/backups/jenderal", s.path("backups"),
		"export PATH=/usr/sbin:/usr/bin:/sbin:/bin", "export PATH="+s.path("bin")+":/usr/bin:/bin",
	).Replace(UpgradeScript())
	// The repository scripts need the network; they are covered by their
	// own packages' tests.
	script = strings.ReplaceAll(script, `bash "$STATE_DIR/php-repo.sh"`, `echo php-repo >> `+calls)
	script = strings.ReplaceAll(script, `bash "$STATE_DIR/pgdg-repo.sh"`, `echo pgdg-repo >> `+calls)

	s.write("state/php-versions", "8.3\n../evil\n")
	cmd := exec.Command("/bin/bash", "-c", script)
	cmd.Env = []string{"PATH=" + s.path("bin") + ":/usr/bin:/bin"}
	if out, err := cmd.CombinedOutput(); err != nil {
		s.t.Logf("script exit: %v\n%s", err, out)
	}
}

func TestUpgradeScriptSuccessRepairsAndRequiresReboot(t *testing.T) {
	s := newSandbox(t)
	s.write("etc/apt/sources.list.d/pgdg.sources", "Types: deb\nURIs: https://apt.postgresql.org/pub/repos/apt\nSuites: noble-pgdg\n")
	s.write("state/mysql-native", "1\n")
	s.run("resolute", 0)

	if got := strings.TrimSpace(s.read("state/state")); got != StateRebootRequired {
		t.Fatalf("state = %q\nlog:\n%s", got, s.read("log/release-upgrade.log"))
	}
	if !strings.Contains(UpgradeScript(), `cat /proc/sys/kernel/random/boot_id > "$STATE_DIR/boot_id"`) {
		t.Error("the finished boot must be recorded")
	}
	calls := s.read("calls")
	for _, want := range []string{
		"do-release-upgrade -f DistUpgradeViewNonInteractive",
		"override-present",
		"php-repo",
		"pgdg-repo", // pgdg still on noble-pgdg after the upgrade
		"apt-get install -y",
		"postgresql-16",                     // reinstalled
		"apt-get install --only-upgrade -y", // noble builds -> resolute builds
		"systemctl enable --now php8.3-fpm",
		"systemctl restart mysql",
		"nginx -t",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("calls missing %q:\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "evil") {
		t.Error("an invalid PHP version reached systemctl")
	}
	if _, err := os.Stat(s.path("etc/update-manager/release-upgrades.d/jenderal-panel.cfg")); err == nil {
		t.Error("the override config was left behind")
	}
	if _, err := os.Stat(s.path("etc/apt/apt.conf.d/99jenderal-release-upgrade")); err == nil {
		t.Error("the apt conf was left behind")
	}
	if !strings.Contains(s.read("etc/update-manager/release-upgrades"), "Prompt=lts") {
		t.Error("Prompt=lts was not set")
	}
	if !strings.Contains(s.read("etc/mysql/mysql.conf.d/zz-jenderal-native-password.cnf"), "mysql_native_password=ON") {
		t.Error("mysql_native_password was not enabled")
	}
}

func TestUpgradeScriptFailureMarksFailedAndCleansUp(t *testing.T) {
	s := newSandbox(t)
	s.run("noble", 1)
	if got := strings.TrimSpace(s.read("state/state")); got != StateFailed {
		t.Fatalf("state = %q", got)
	}
	if strings.TrimSpace(s.read("state/exit_code")) != "1" {
		t.Errorf("exit_code = %q", s.read("state/exit_code"))
	}
	if strings.Contains(s.read("calls"), "php-repo") {
		t.Error("repair must not run when the upgrade failed")
	}
	if _, err := os.Stat(s.path("etc/update-manager/release-upgrades.d/jenderal-panel.cfg")); err == nil {
		t.Error("the override config was left behind after a failure")
	}
}

// do-release-upgrade can exit 0 without upgrading (e.g. no new release
// offered); that must not be reported as success.
func TestUpgradeScriptRequiresTheNewRelease(t *testing.T) {
	s := newSandbox(t)
	s.run("noble", 0)
	if got := strings.TrimSpace(s.read("state/state")); got != StateFailed {
		t.Fatalf("state = %q", got)
	}
}

// pgStubs fakes a server with one PostgreSQL 16 cluster and PostgreSQL 18
// binaries; pg_upgradecluster exits with upgradeExit.
func (s *sandbox) pgStubs(upgradeExit int) {
	calls := s.path("calls")
	log := func(name string) string { return "echo \"" + name + " $*\" >> " + calls + "\n" }
	s.write("usr/lib/postgresql/18/bin/postgres", "")
	os.Chmod(s.path("usr/lib/postgresql/18/bin/postgres"), 0o755)
	s.write("etc/postgresql/16/main/start.conf", "auto\n")
	s.stub("pg_lsclusters", "echo '16  main    5432 online postgres /var/lib/postgresql/16/main /var/log/postgresql/postgresql-16-main.log'\n")
	s.stub("runuser", log("runuser")+"echo 'CREATE ROLE app;'\n")
	s.stub("pg_upgradecluster", log("pg_upgradecluster")+"exit "+strconv.Itoa(upgradeExit)+"\n")
	s.stub("pg_ctlcluster", log("pg_ctlcluster"))
	s.stub("pg_dropcluster", log("pg_dropcluster"))
}

func TestUpgradeScriptMigratesPostgreSQLWhenAsked(t *testing.T) {
	s := newSandbox(t)
	s.pgStubs(0)
	s.write("state/pg-migrate", "1\n")
	s.run("resolute", 0)

	calls := s.read("calls")
	for _, want := range []string{
		"pgdg-repo", // PostgreSQL 16 is not in resolute's archive
		"postgresql-18-pgvector",
		"runuser -u postgres -- pg_dumpall -p 5432",
		"pg_upgradecluster -v 18 -m upgrade 16 main",
		"pg_ctlcluster 16 main stop",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("calls missing %q:\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "pg_dropcluster") {
		t.Error("a successful migration must not drop anything")
	}
	if got := s.read("etc/postgresql/16/main/start.conf"); got != "manual\n" {
		t.Errorf("old cluster start.conf = %q, want manual", got)
	}
	dumps, _ := filepath.Glob(s.path("backups/postgresql-16-main-*.sql.gz"))
	if len(dumps) != 1 {
		t.Errorf("backups = %v", dumps)
	}
	if got := strings.TrimSpace(s.read("state/state")); got != StateRebootRequired {
		t.Errorf("state = %q", got)
	}
}

func TestUpgradeScriptRestoresTheOldClusterWhenMigrationFails(t *testing.T) {
	s := newSandbox(t)
	s.pgStubs(1)
	s.write("state/pg-migrate", "1\n")
	s.run("resolute", 0)

	calls := s.read("calls")
	for _, want := range []string{"pg_dropcluster --stop 18 main", "pg_ctlcluster 16 main start"} {
		if !strings.Contains(calls, want) {
			t.Errorf("calls missing %q:\n%s", want, calls)
		}
	}
	if !strings.Contains(s.read("state/warnings"), "could not be migrated") {
		t.Errorf("warnings = %q", s.read("state/warnings"))
	}
	if got := s.read("etc/postgresql/16/main/start.conf"); got != "auto\n" {
		t.Errorf("old cluster start.conf = %q, want it untouched", got)
	}
	if got := strings.TrimSpace(s.read("state/state")); got != StateRebootRequired {
		t.Errorf("a failed migration must not fail the upgrade; state = %q", got)
	}
}

func TestUpgradeScriptSkipsPostgreSQLMigrationUnlessAsked(t *testing.T) {
	s := newSandbox(t)
	s.pgStubs(0)
	s.write("state/pg-migrate", "0\n")
	s.run("resolute", 0)
	calls := s.read("calls")
	if strings.Contains(calls, "pg_upgradecluster") || strings.Contains(calls, "pg_dumpall") {
		t.Errorf("migration ran without being asked:\n%s", calls)
	}
	if !strings.Contains(calls, "pgdg-repo") {
		t.Error("PostgreSQL 16 needs pgdg to keep receiving updates")
	}
}
