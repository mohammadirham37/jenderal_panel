package diskusage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestParseDF(t *testing.T) {
	out := "Filesystem 1024-blocks Used Available Capacity Mounted on\n" +
		"/dev/vda1 41234567000 21000000000 19000000000 53% /\n" +
		"tmpfs 1000 0 1000 0% /dev/shm\n" +
		"/dev/vda1 41234567000 21000000000 19000000000 53% /boot\n"

	stats := parseDF(out)
	// parseDF skips only the header; tmpfs rows are filtered by df's -x flags
	// at runtime, so the raw parser must report all three data rows.
	if len(stats) != 3 {
		t.Fatalf("expected 3 filesystems, got %d", len(stats))
	}
	root := stats[0]
	if root.Source != "/dev/vda1" || root.MountedOn != "/" {
		t.Errorf("unexpected source/mount: %+v", root)
	}
	if root.SizeBytes != 41234567000 || root.UsedBytes != 21000000000 || root.AvailBytes != 19000000000 {
		t.Errorf("unexpected byte fields: %+v", root)
	}
	if root.UsePercent != 53 {
		t.Errorf("expected 53%%, got %d", root.UsePercent)
	}
}

func TestParseHumanSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"512.0M", 512 * 1024 * 1024},
		{"1.5G", int64(1.5 * 1024 * 1024 * 1024)},
		{"42.0K", 42 * 1024},
		{"100", 100},
		{"2.0T", 2 * 1024 * 1024 * 1024 * 1024},
		{"bogus", 0},
		{"", 0},
	}
	for _, c := range cases {
		if got := parseHumanSize(c.in); got != c.want {
			t.Errorf("parseHumanSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseJournalUsage(t *testing.T) {
	out := "Archived and active journals take up 512.0M in the file system.\n"
	if got := parseJournalUsage(out); got != 512*1024*1024 {
		t.Errorf("expected 512MiB, got %d", got)
	}
	if got := parseJournalUsage(""); got != 0 {
		t.Errorf("expected 0 for empty output, got %d", got)
	}
}

func TestParseDuSizes(t *testing.T) {
	out := "104857600\t/var/log\n20971520\t/var/cache/apt\n0\t/tmp\nnot-a-number\tx\njustonepath\n"
	sizes := parseDuSizes(out)
	if len(sizes) != 3 {
		t.Fatalf("expected 3 parsed paths, got %d: %v", len(sizes), sizes)
	}
	if sizes["/var/log"] != 104857600 || sizes["/tmp"] != 0 {
		t.Errorf("unexpected sizes: %v", sizes)
	}
}

func TestCleanupCommandsBounds(t *testing.T) {
	svc := NewService(nil, &executor.MockExecutor{})

	// Nothing selected -> no commands.
	if cmds := svc.CleanupCommands(CleanupOptions{}); len(cmds) != 0 {
		t.Errorf("expected no commands, got %d", len(cmds))
	}

	opts := CleanupOptions{
		JournalVacuumMB: 100,
		AptClean:        true,
		AptAutoremove:   true,
		TmpCleanDays:    7,
		DockerPrune:     true,
	}
	plan := svc.CleanupPlan(opts)
	if len(plan) != 5 {
		t.Fatalf("expected 5 planned actions, got %d", len(plan))
	}

	cmds := svc.CleanupCommands(opts)
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(cmds))
	}
	if cmds[0][0] != "journalctl" || !strings.HasPrefix(cmds[0][1], "--vacuum-size=100M") {
		t.Errorf("unexpected journal command: %v", cmds[0])
	}
	if cmds[3][0] != "find" || cmds[3][6] != "+7" {
		t.Errorf("unexpected tmp clean command: %v", cmds[3])
	}

	// The handler clamps at the extremes; the command builder must keep the
	// user value as given so the preview matches what runs.
	extreme := CleanupOptions{JournalVacuumMB: 99999, TmpCleanDays: 99999}
	extremeCmds := svc.CleanupCommands(extreme)
	if extremeCmds[0][1] != "--vacuum-size=99999M" {
		t.Errorf("expected raw value passthrough, got %v", extremeCmds[0])
	}
}

func TestOverviewCollectsReport(t *testing.T) {
	exec := &executor.MockExecutor{}
	exec.RunFunc = func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "uname" {
			return &executor.Result{Stdout: "6.8.0-45-generic\n"}, nil
		}
		return &executor.Result{}, errors.New("unexpected command " + name)
	}
	exec.RunSudoFunc = func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		switch name {
		case "df":
			return &executor.Result{Stdout: "Filesystem 1-blocks Used Available Capacity Mounted on\n/dev/vda1 100 60 40 60% /\n"}, nil
		case "du":
			// The labeled-paths scan passes "-sb" plus the path list; the
			// root filesystem scan passes "-B1 --max-depth=1 -x /".
			if len(args) > 0 && args[0] == "-B1" {
				return &executor.Result{
					Stdout: "32212254720\t/\n21474836480\t/var\n5368709120\t/home\n1073741824\t/usr\n",
				}, nil
			}
			return &executor.Result{
				Stdout: "104857600\t/var/log\n20971520\t/var/cache/apt\n0\t/tmp\n",
			}, nil
		case "journalctl":
			return &executor.Result{Stdout: "Archived and active journals take up 100.0M in the file system.\n"}, nil
		case "dpkg-query":
			return &executor.Result{Stdout: "linux-image-6.8.0-45-generic\nlinux-image-6.5.0-14-generic\nlinux-headers-6.5.0-14-generic\n"}, nil
		case "docker":
			return &executor.Result{Stdout: "Images\t2.5GB\nContainers\t100MB\n"}, nil
		}
		return &executor.Result{}, errors.New("unexpected sudo command " + name)
	}

	svc := NewService(nil, exec)
	overview, err := svc.Overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}

	if len(overview.Filesystems) != 1 || overview.Filesystems[0].UsePercent != 60 {
		t.Errorf("unexpected filesystems: %+v", overview.Filesystems)
	}
	if overview.JournalBytes != 100*1024*1024 {
		t.Errorf("unexpected journal bytes: %d", overview.JournalBytes)
	}
	if overview.Kernel != "6.8.0-45-generic" {
		t.Errorf("unexpected kernel: %s", overview.Kernel)
	}
	if len(overview.OldKernels) != 2 {
		t.Fatalf("expected 2 old kernel packages, got %d: %v", len(overview.OldKernels), overview.OldKernels)
	}
	if !overview.HasDocker || len(overview.Docker) != 2 {
		t.Errorf("unexpected docker usage: %+v", overview.Docker)
	}

	// Root folders come back largest first, without the "/" total line.
	if len(overview.RootDirs) != 3 {
		t.Fatalf("expected 3 root dirs, got %d: %+v", len(overview.RootDirs), overview.RootDirs)
	}
	if overview.RootDirs[0].Path != "/var" || overview.RootDirs[0].Bytes != 21474836480 {
		t.Errorf("expected /var first, got %+v", overview.RootDirs[0])
	}
	if overview.RootDirs[2].Path != "/usr" {
		t.Errorf("expected /usr last, got %+v", overview.RootDirs[2])
	}

	// No database handle means no per-website section.
	if len(overview.Websites) != 0 {
		t.Errorf("expected no website rows without a db, got %+v", overview.Websites)
	}

	byLabel := map[string]DirUsage{}
	for _, d := range overview.Dirs {
		byLabel[d.Label] = d
	}
	logs := byLabel["system_logs"]
	if logs.Bytes != 104857600 || logs.Missing {
		t.Errorf("unexpected /var/log usage: %+v", logs)
	}
	if !byLabel["websites"].Missing {
		t.Errorf("expected /var/www to be reported missing when du has no line for it")
	}
	if !byLabel["panel_backups"].Missing {
		t.Errorf("expected /var/lib/jenderal/backups to be reported missing")
	}
}
