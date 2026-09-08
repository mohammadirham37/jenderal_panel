package system

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// MetricsCollector periodically collects server metrics and stores them.
type MetricsCollector struct {
	db     *sql.DB
	cfg    config.MetricsConfig
	buffer *ringBuffer
}

// NewMetricsCollector creates a new MetricsCollector.
func NewMetricsCollector(db *sql.DB, cfg config.MetricsConfig) *MetricsCollector {
	// Default buffer size: enough to hold ~10 minutes of samples at 5s intervals.
	bufSize := 120
	return &MetricsCollector{
		db:     db,
		cfg:    cfg,
		buffer: newRingBuffer(bufSize),
	}
}

// Start begins background collection. It blocks until ctx is cancelled.
func (mc *MetricsCollector) Start(ctx context.Context) {
	go func() {
		collectTicker := time.NewTicker(mc.cfg.CollectInterval)
		storeTicker := time.NewTicker(mc.cfg.StoreInterval)
		cleanupTicker := time.NewTicker(24 * time.Hour)
		defer collectTicker.Stop()
		defer storeTicker.Stop()
		defer cleanupTicker.Stop()

		// Collect once immediately.
		m := mc.collect()
		mc.buffer.Add(m)

		var lastStored model.ServerMetrics
		for {
			select {
			case <-ctx.Done():
				return
			case <-collectTicker.C:
				m := mc.collect()
				mc.buffer.Add(m)
			case <-storeTicker.C:
				latest := mc.buffer.Latest()
				if latest.Timestamp.IsZero() {
					continue
				}
				if latest.Timestamp.Equal(lastStored.Timestamp) {
					continue
				}
				_ = mc.store(ctx, latest)
				lastStored = latest
		case <-cleanupTicker.C:
			mc.cleanup(ctx)
		}
	}
	}()
}

// Buffer returns the in-memory ring buffer.
func (mc *MetricsCollector) Buffer() *ringBuffer {
	return mc.buffer
}

// store inserts a metrics snapshot into the database.
func (mc *MetricsCollector) store(ctx context.Context, m model.ServerMetrics) error {
	const query = `INSERT INTO server_metrics
		(cpu, ram_used, ram_total, swap_used, swap_total, disk_used, disk_total,
		 load_1, load_5, load_15, net_rx, net_tx, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := mc.db.ExecContext(ctx, query,
		m.CPU, m.RAMUsed, m.RAMTotal, m.SwapUsed, m.SwapTotal,
		m.DiskUsed, m.DiskTotal,
		m.Load1, m.Load5, m.Load15,
		m.NetRx, m.NetTx,
		m.Timestamp.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store metrics: %w", err)
	}
	return nil
}

// GetRecent retrieves the most recent metrics from the database.
func (mc *MetricsCollector) GetRecent(ctx context.Context, limit int) ([]model.ServerMetrics, error) {
	const query = `SELECT cpu, ram_used, ram_total, swap_used, swap_total,
		disk_used, disk_total, load_1, load_5, load_15, net_rx, net_tx, created_at
		FROM server_metrics ORDER BY created_at DESC LIMIT ?`

	rows, err := mc.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent metrics: %w", err)
	}
	defer rows.Close()

	var results []model.ServerMetrics
	for rows.Next() {
		var m model.ServerMetrics
		var ts string
		err := rows.Scan(
			&m.CPU, &m.RAMUsed, &m.RAMTotal, &m.SwapUsed, &m.SwapTotal,
			&m.DiskUsed, &m.DiskTotal, &m.Load1, &m.Load5, &m.Load15,
			&m.NetRx, &m.NetTx, &ts,
		)
		if err != nil {
			return nil, fmt.Errorf("scan metrics row: %w", err)
		}
		m.Timestamp, _ = time.Parse(time.RFC3339, ts)
		results = append(results, m)
	}
	return results, rows.Err()
}

// cleanup removes metrics older than the configured retention period.
func (mc *MetricsCollector) cleanup(ctx context.Context) {
	cutoff := time.Now().UTC().AddDate(0, 0, -mc.cfg.RetentionDays).Format(time.RFC3339)
	_, _ = mc.db.ExecContext(ctx, "DELETE FROM server_metrics WHERE created_at < ?", cutoff)
}

// collect gathers current system metrics. On non-Linux systems the stub
// readers return zeros.
func (mc *MetricsCollector) collect() model.ServerMetrics {
	ramUsed, ramTotal := readMemory()
	swapUsed, swapTotal := readSwap()
	diskUsed, diskTotal := readDisk()
	load1, load5, load15 := readLoadAvg()
	netRx, netTx := readNetwork()

	return model.ServerMetrics{
		CPU:       readCPU(),
		RAMUsed:   ramUsed,
		RAMTotal:  ramTotal,
		SwapUsed:  swapUsed,
		SwapTotal: swapTotal,
		DiskUsed:  diskUsed,
		DiskTotal: diskTotal,
		Load1:     load1,
		Load5:     load5,
		Load15:    load15,
		NetRx:     netRx,
		NetTx:     netTx,
		Uptime:    readUptime(),
		Timestamp: time.Now(),
	}
}

// ---------------------------------------------------------------------------
// Ring buffer (unexported)
// ---------------------------------------------------------------------------

type ringBuffer struct {
	mu    sync.RWMutex
	items []model.ServerMetrics
	size  int
	pos   int
	count int
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		items: make([]model.ServerMetrics, size),
		size:  size,
	}
}

// Add writes a metric at the current position and advances the cursor.
func (rb *ringBuffer) Add(m model.ServerMetrics) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.items[rb.pos] = m
	rb.pos = (rb.pos + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

// Latest returns the most recently added metric.
func (rb *ringBuffer) Latest() model.ServerMetrics {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	if rb.count == 0 {
		return model.ServerMetrics{}
	}
	idx := (rb.pos - 1 + rb.size) % rb.size
	return rb.items[idx]
}

// All returns all stored metrics in chronological order (oldest first).
func (rb *ringBuffer) All() []model.ServerMetrics {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	if rb.count == 0 {
		return nil
	}
	result := make([]model.ServerMetrics, rb.count)
	if rb.count < rb.size {
		// Buffer hasn't wrapped yet.
		copy(result, rb.items[:rb.count])
	} else {
		// Buffer has wrapped: oldest is at rb.pos.
		n := copy(result, rb.items[rb.pos:])
		copy(result[n:], rb.items[:rb.pos])
	}
	return result
}

