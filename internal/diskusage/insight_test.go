package diskusage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func insightDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel.db")
	db, err := database.Open(config.DatabaseConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db, path
}

// The panel's own database was invisible on the disk page, which is exactly
// where unexplained growth accumulated.
func TestPanelDataReportsDatabaseAndBiggestTables(t *testing.T) {
	db, path := insightDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := 0; i < 50; i++ {
		if _, err := db.Exec(`INSERT INTO background_tasks (id, name, module, status, output, error, started_at, ended_at, updated_at)
			VALUES (?, 'Install PHP', 'php', 'completed', ?, '', ?, ?, ?)`,
			strings.Repeat("x", 3)+string(rune('a'+i%26))+time.Now().Format("150405.000000000"),
			strings.Repeat("apt line\n", 500), now, now, now); err != nil {
			t.Fatal(err)
		}
	}

	svc := NewService(db, &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{Stdout: "4096\t" + filepath.Dir(path) + "\n"}, nil
		},
	})
	svc.SetDatabasePath(path)

	panelData := svc.PanelData(context.Background())
	if panelData.DatabaseBytes <= 0 {
		t.Fatalf("database size not reported: %+v", panelData)
	}
	var tasks *TableUsage
	for i := range panelData.Tables {
		if panelData.Tables[i].Name == "background_tasks" {
			tasks = &panelData.Tables[i]
		}
	}
	if tasks == nil {
		t.Fatalf("background_tasks missing from the report: %+v", panelData.Tables)
	}
	if tasks.Rows != 50 || tasks.Bytes <= 0 {
		t.Errorf("background_tasks = %+v, want 50 rows with a measured size", *tasks)
	}
	// Biggest first, so the page shows the culprit at the top.
	for i := 1; i < len(panelData.Tables); i++ {
		if panelData.Tables[i-1].Bytes < panelData.Tables[i].Bytes {
			t.Errorf("tables are not sorted by size: %+v", panelData.Tables)
			break
		}
	}
}

// Growth history answers "it grew, but from what?" across days.
func TestGrowthRecordsSnapshotsAndReportsDeltas(t *testing.T) {
	db, path := insightDB(t)
	svc := NewService(db, &executor.MockExecutor{})
	svc.SetDatabasePath(path)
	ctx := context.Background()

	day := func(d int) time.Time { return time.Date(2026, 10, 10+d, 6, 0, 0, 0, time.UTC) }
	for i, used := range []int64{10 << 30, 12 << 30, 15 << 30} {
		if err := svc.recordUsageSnapshot(ctx, day(i), Overview{
			Filesystems: []FilesystemStat{{MountedOn: "/", UsedBytes: used, SizeBytes: 40 << 30}},
			Dirs: []DirUsage{
				{Path: "/var/log", Label: "system_logs", Bytes: int64(i+1) << 30},
				{Path: "/var/lib/jenderal", Label: "panel_data", Bytes: 1 << 28},
			},
		}); err != nil {
			t.Fatalf("snapshot %d: %v", i, err)
		}
	}

	growth, err := svc.Growth(ctx, day(2), 7)
	if err != nil {
		t.Fatalf("Growth: %v", err)
	}
	if growth.UsedBytes != 15<<30 || growth.PreviousBytes != 10<<30 {
		t.Fatalf("growth = %+v, want 15 GiB now against 10 GiB earlier", growth)
	}
	if growth.DeltaBytes != 5<<30 {
		t.Errorf("delta = %d, want %d", growth.DeltaBytes, int64(5)<<30)
	}
	if len(growth.Points) != 3 {
		t.Errorf("points = %d, want one per snapshot", len(growth.Points))
	}
	// The report names which measured location grew the most.
	if len(growth.TopGrowers) == 0 || growth.TopGrowers[0].Path != "/var/log" {
		t.Errorf("top growers = %+v, want /var/log first", growth.TopGrowers)
	}
	if growth.TopGrowers[0].DeltaBytes != 2<<30 {
		t.Errorf("/var/log delta = %d, want %d", growth.TopGrowers[0].DeltaBytes, int64(2)<<30)
	}
}

// Largest files answers the question directly: which files are eating disk.
func TestLargestFilesParsesAndSorts(t *testing.T) {
	exec := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name != "find" {
				return &executor.Result{}, nil
			}
			return &executor.Result{Stdout: "" +
				"104857600\t/var/log/nginx/access.log\n" +
				"2147483648\t/home/web_shop/backup.tar.gz\n" +
				"536870912\t/var/lib/jenderal/jenderal.db\n" +
				"not a size line\n"}, nil
		},
	}
	svc := NewService(nil, exec)
	files, err := svc.LargestFiles(context.Background(), "/", 10)
	if err != nil {
		t.Fatalf("LargestFiles: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("files = %+v, want the three parsable rows", files)
	}
	if files[0].Path != "/home/web_shop/backup.tar.gz" || files[0].Bytes != 2147483648 {
		t.Errorf("largest first failed: %+v", files)
	}
	for _, bad := range []string{"/etc/nginx", "/home/web_shop", "../../etc"} {
		if _, err := svc.LargestFiles(context.Background(), bad, 10); err == nil {
			t.Errorf("scan root %q must be refused", bad)
		}
	}
}

func TestCompactPanelDataFreesSpace(t *testing.T) {
	db, path := insightDB(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339Nano)
	for i := 0; i < 200; i++ {
		if _, err := db.Exec(`INSERT INTO background_tasks (id, name, module, status, output, error, started_at, ended_at, updated_at)
			VALUES (?, 'Install', 'php', 'completed', ?, '', ?, ?, ?)`,
			fmt.Sprintf("old-%03d", i), strings.Repeat("apt output\n", 1000), old, old, old); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(db, &executor.MockExecutor{})
	svc.SetDatabasePath(path)

	result, err := svc.CompactPanelData(ctx)
	if err != nil {
		t.Fatalf("CompactPanelData: %v", err)
	}
	if result.TasksRemoved != 200 {
		t.Errorf("tasks removed = %d, want 200", result.TasksRemoved)
	}
	if result.BytesFreed <= 0 || result.BytesAfter >= result.BytesBefore {
		t.Errorf("no space reclaimed: %+v", result)
	}
}
