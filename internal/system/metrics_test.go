package system

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// setupTestDB creates an in-memory SQLite database with the server_metrics table.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS server_metrics (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		cpu        REAL NOT NULL,
		ram_used   INTEGER NOT NULL,
		ram_total  INTEGER NOT NULL,
		swap_used  INTEGER NOT NULL,
		swap_total INTEGER NOT NULL,
		disk_used  INTEGER NOT NULL,
		disk_total INTEGER NOT NULL,
		load_1     REAL NOT NULL,
		load_5     REAL NOT NULL,
		load_15    REAL NOT NULL,
		net_rx     INTEGER NOT NULL,
		net_tx     INTEGER NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_server_metrics_created_at ON server_metrics(created_at);`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// -----------------------------------------------------------------------
// Ring buffer tests
// -----------------------------------------------------------------------

func TestRingBufferAddAndLatest(t *testing.T) {
	rb := newRingBuffer(3)

	// Empty buffer returns zero-value.
	latest := rb.Latest()
	if !latest.Timestamp.IsZero() {
		t.Error("Latest() on empty buffer should return zero-value Timestamp")
	}

	now := time.Now()
	rb.Add(model.ServerMetrics{CPU: 10, Timestamp: now})
	latest = rb.Latest()
	if latest.CPU != 10 {
		t.Errorf("Latest().CPU = %v, want 10", latest.CPU)
	}

	rb.Add(model.ServerMetrics{CPU: 20, Timestamp: now.Add(time.Second)})
	latest = rb.Latest()
	if latest.CPU != 20 {
		t.Errorf("Latest().CPU = %v, want 20", latest.CPU)
	}
}

func TestRingBufferAll(t *testing.T) {
	rb := newRingBuffer(3)

	// Empty buffer.
	if items := rb.All(); items != nil {
		t.Errorf("All() on empty buffer = %v, want nil", items)
	}

	// Fill partially.
	rb.Add(model.ServerMetrics{CPU: 1})
	rb.Add(model.ServerMetrics{CPU: 2})
	items := rb.All()
	if len(items) != 2 {
		t.Fatalf("All() len = %d, want 2", len(items))
	}
	if items[0].CPU != 1 || items[1].CPU != 2 {
		t.Errorf("All() = [%v, %v], want [1, 2]", items[0].CPU, items[1].CPU)
	}
}

func TestRingBufferWrapAround(t *testing.T) {
	rb := newRingBuffer(3)

	rb.Add(model.ServerMetrics{CPU: 1})
	rb.Add(model.ServerMetrics{CPU: 2})
	rb.Add(model.ServerMetrics{CPU: 3})
	rb.Add(model.ServerMetrics{CPU: 4}) // overwrites CPU=1
	rb.Add(model.ServerMetrics{CPU: 5}) // overwrites CPU=2

	latest := rb.Latest()
	if latest.CPU != 5 {
		t.Errorf("Latest().CPU = %v, want 5", latest.CPU)
	}

	items := rb.All()
	if len(items) != 3 {
		t.Fatalf("All() len = %d, want 3", len(items))
	}
	// Should be in chronological order: 3, 4, 5
	expected := []float64{3, 4, 5}
	for i, want := range expected {
		if items[i].CPU != want {
			t.Errorf("All()[%d].CPU = %v, want %v", i, items[i].CPU, want)
		}
	}
}

// -----------------------------------------------------------------------
// Store and GetRecent tests
// -----------------------------------------------------------------------

func TestStoreAndGetRecent(t *testing.T) {
	db := setupTestDB(t)
	cfg := config.MetricsConfig{
		CollectInterval: 5 * time.Second,
		StoreInterval:   60 * time.Second,
		RetentionDays:   7,
	}
	mc := NewMetricsCollector(db, cfg)

	ctx := context.Background()
	now := time.Now().UTC()

	// Store 3 metrics.
	for i := 0; i < 3; i++ {
		m := model.ServerMetrics{
			CPU:       float64(i + 1) * 10,
			RAMUsed:   uint64(i+1) * 1024,
			RAMTotal:  8192,
			SwapUsed:  0,
			SwapTotal: 2048,
			DiskUsed:  uint64(i+1) * 10000,
			DiskTotal: 100000,
			Load1:     float64(i+1) * 0.5,
			Load5:     float64(i+1) * 0.3,
			Load15:    float64(i+1) * 0.1,
			NetRx:     uint64(i+1) * 100,
			NetTx:     uint64(i+1) * 50,
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		}
		if err := mc.store(ctx, m); err != nil {
			t.Fatalf("store(%d) error: %v", i, err)
		}
	}

	// GetRecent with limit 2.
	results, err := mc.GetRecent(ctx, 2)
	if err != nil {
		t.Fatalf("GetRecent() error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("GetRecent(2) returned %d rows, want 2", len(results))
	}

	// Most recent first (DESC).
	if results[0].CPU != 30 {
		t.Errorf("results[0].CPU = %v, want 30", results[0].CPU)
	}
	if results[1].CPU != 20 {
		t.Errorf("results[1].CPU = %v, want 20", results[1].CPU)
	}

	// Verify other fields round-trip correctly.
	if results[0].RAMTotal != 8192 {
		t.Errorf("results[0].RAMTotal = %v, want 8192", results[0].RAMTotal)
	}
	if results[0].Load1 != 1.5 {
		t.Errorf("results[0].Load1 = %v, want 1.5", results[0].Load1)
	}
}

func TestCleanup(t *testing.T) {
	db := setupTestDB(t)
	cfg := config.MetricsConfig{
		CollectInterval: 5 * time.Second,
		StoreInterval:   60 * time.Second,
		RetentionDays:   7,
	}
	mc := NewMetricsCollector(db, cfg)
	ctx := context.Background()

	// Insert a metric that's 10 days old.
	old := model.ServerMetrics{
		CPU:       50,
		Timestamp: time.Now().UTC().AddDate(0, 0, -10),
	}
	if err := mc.store(ctx, old); err != nil {
		t.Fatalf("store old metric: %v", err)
	}

	// Insert a recent metric.
	recent := model.ServerMetrics{
		CPU:       80,
		Timestamp: time.Now().UTC(),
	}
	if err := mc.store(ctx, recent); err != nil {
		t.Fatalf("store recent metric: %v", err)
	}

	// Run cleanup.
	mc.cleanup(ctx)

	// Only the recent metric should remain.
	results, err := mc.GetRecent(ctx, 10)
	if err != nil {
		t.Fatalf("GetRecent() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("after cleanup: got %d rows, want 1", len(results))
	}
	if results[0].CPU != 80 {
		t.Errorf("remaining metric CPU = %v, want 80", results[0].CPU)
	}
}

func TestCollect(t *testing.T) {
	db := setupTestDB(t)
	cfg := config.MetricsConfig{
		CollectInterval: 5 * time.Second,
		StoreInterval:   60 * time.Second,
		RetentionDays:   7,
	}
	mc := NewMetricsCollector(db, cfg)

	m := mc.collect()
	// On non-Linux (macOS dev) all stub readers return zeros.
	if m.CPU != 0 {
		t.Errorf("collect().CPU = %v, want 0 (stub)", m.CPU)
	}
	if m.Timestamp.IsZero() {
		t.Error("collect().Timestamp should not be zero")
	}
}

func TestNewMetricsCollectorBuffer(t *testing.T) {
	db := setupTestDB(t)
	cfg := config.MetricsConfig{
		CollectInterval: 5 * time.Second,
		StoreInterval:   60 * time.Second,
		RetentionDays:   7,
	}
	mc := NewMetricsCollector(db, cfg)

	if mc.Buffer() == nil {
		t.Error("Buffer() should not be nil")
	}
}
