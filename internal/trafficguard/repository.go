package trafficguard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }
func enc(v any) string                     { b, _ := json.Marshal(v); return string(b) }
func ts(t time.Time) string                { return t.UTC().Format(time.RFC3339Nano) }

func (r *Repository) Cursor(ctx context.Context, id string) (LogCursor, error) {
	var c LogCursor
	var updated string
	err := r.db.QueryRowContext(ctx, `SELECT website_id,inode,offset,updated_at FROM traffic_log_cursors WHERE website_id=?`, id).Scan(&c.WebsiteID, &c.Inode, &c.Offset, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return LogCursor{WebsiteID: id}, nil
	}
	if err == nil {
		c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	}
	return c, err
}
func (r *Repository) CommitBucket(ctx context.Context, b MinuteBucket, c LogCursor) error {
	return r.CommitBuckets(ctx, []MinuteBucket{b}, c)
}

func (r *Repository) CommitBuckets(ctx context.Context, buckets []MinuteBucket, c LogCursor) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, b := range buckets {
		_, err = tx.ExecContext(ctx, `INSERT INTO traffic_minute_buckets(website_id,bucket_at,requests,status_4xx,status_5xx,status_429,bytes,peak_rps,top_ips,top_paths,top_agents) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(website_id,bucket_at) DO UPDATE SET requests=requests+excluded.requests,status_4xx=status_4xx+excluded.status_4xx,status_5xx=status_5xx+excluded.status_5xx,status_429=status_429+excluded.status_429,bytes=bytes+excluded.bytes,peak_rps=MAX(peak_rps,excluded.peak_rps),top_ips=excluded.top_ips,top_paths=excluded.top_paths,top_agents=excluded.top_agents`, b.WebsiteID, ts(b.BucketAt), b.Requests, b.Status4xx, b.Status5xx, b.Status429, b.Bytes, b.PeakRPS, enc(b.TopIPs), enc(b.TopPaths), enc(b.TopAgents))
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO traffic_log_cursors(website_id,inode,offset,updated_at) VALUES(?,?,?,?) ON CONFLICT(website_id) DO UPDATE SET inode=excluded.inode,offset=excluded.offset,updated_at=excluded.updated_at`, c.WebsiteID, c.Inode, c.Offset, ts(c.UpdatedAt))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) Baseline(ctx context.Context, id string) (Baseline, error) {
	var b Baseline
	var first sql.NullString
	var updated string
	err := r.db.QueryRowContext(ctx, `SELECT website_id,sample_count,mean_rpm,m2_rpm,first_sample_at,updated_at FROM traffic_baselines WHERE website_id=?`, id).Scan(&b.WebsiteID, &b.SampleCount, &b.MeanRPM, &b.M2RPM, &first, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Baseline{WebsiteID: id}, nil
	}
	if first.Valid {
		v, _ := time.Parse(time.RFC3339Nano, first.String)
		b.FirstSampleAt = &v
	}
	b.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return b, err
}
func (r *Repository) SaveBaseline(ctx context.Context, b Baseline) error {
	var first any
	if b.FirstSampleAt != nil {
		first = ts(*b.FirstSampleAt)
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO traffic_baselines(website_id,sample_count,mean_rpm,m2_rpm,first_sample_at,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(website_id) DO UPDATE SET sample_count=excluded.sample_count,mean_rpm=excluded.mean_rpm,m2_rpm=excluded.m2_rpm,first_sample_at=excluded.first_sample_at,updated_at=excluded.updated_at`, b.WebsiteID, b.SampleCount, b.MeanRPM, b.M2RPM, first, ts(b.UpdatedAt))
	return err
}
func (r *Repository) Bucket(ctx context.Context, id string, at time.Time) (MinuteBucket, error) {
	var b MinuteBucket
	var stamp, ips, paths, agents string
	err := r.db.QueryRowContext(ctx, `SELECT website_id,bucket_at,requests,status_4xx,status_5xx,status_429,bytes,peak_rps,top_ips,top_paths,top_agents FROM traffic_minute_buckets WHERE website_id=? AND bucket_at=?`, id, ts(at.Truncate(time.Minute))).Scan(&b.WebsiteID, &stamp, &b.Requests, &b.Status4xx, &b.Status5xx, &b.Status429, &b.Bytes, &b.PeakRPS, &ips, &paths, &agents)
	b.BucketAt, _ = time.Parse(time.RFC3339Nano, stamp)
	_ = json.Unmarshal([]byte(ips), &b.TopIPs)
	_ = json.Unmarshal([]byte(paths), &b.TopPaths)
	_ = json.Unmarshal([]byte(agents), &b.TopAgents)
	return b, err
}
func (r *Repository) ListBuckets(ctx context.Context, id string, since time.Time) ([]MinuteBucket, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT website_id,bucket_at,requests,status_4xx,status_5xx,status_429,bytes,peak_rps,top_ips,top_paths,top_agents FROM traffic_minute_buckets WHERE website_id=? AND bucket_at>=? ORDER BY bucket_at`, id, ts(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MinuteBucket
	for rows.Next() {
		var b MinuteBucket
		var stamp, ips, paths, agents string
		if err := rows.Scan(&b.WebsiteID, &stamp, &b.Requests, &b.Status4xx, &b.Status5xx, &b.Status429, &b.Bytes, &b.PeakRPS, &ips, &paths, &agents); err != nil {
			return nil, err
		}
		b.BucketAt, _ = time.Parse(time.RFC3339Nano, stamp)
		_ = json.Unmarshal([]byte(ips), &b.TopIPs)
		_ = json.Unmarshal([]byte(paths), &b.TopPaths)
		_ = json.Unmarshal([]byte(agents), &b.TopAgents)
		out = append(out, b)
	}
	return out, rows.Err()
}
func (r *Repository) Profile(ctx context.Context, id string, now time.Time) (WebsiteProfile, error) {
	var p WebsiteProfile
	var cidrs, observe, created, updated string
	err := r.db.QueryRowContext(ctx, `SELECT website_id,mode,proxy_mode,proxy_header,proxy_cidrs,requests_per_second,burst,connections,observe_started_at,created_at,updated_at FROM traffic_guard_profiles WHERE website_id=?`, id).Scan(&p.WebsiteID, &p.Mode, &p.ProxyMode, &p.ProxyHeader, &cidrs, &p.RequestsPerSecond, &p.Burst, &p.Connections, &observe, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultProfile(id, now), nil
	}
	if err != nil {
		return p, err
	}
	_ = json.Unmarshal([]byte(cidrs), &p.ProxyCIDRs)
	p.ObserveStartedAt, _ = time.Parse(time.RFC3339Nano, observe)
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return p, nil
}
func (r *Repository) Profiles(ctx context.Context, now time.Time) ([]WebsiteProfile, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM websites WHERE status IN ('active','suspended') ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []WebsiteProfile
	for _, id := range ids {
		if err := r.EnsureProfile(ctx, id, now); err != nil {
			return nil, err
		}
		p, err := r.Profile(ctx, id, now)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (r *Repository) EnsureProfile(ctx context.Context, id string, now time.Time) error {
	p := DefaultProfile(id, now)
	_, err := r.db.ExecContext(ctx, `INSERT OR IGNORE INTO traffic_guard_profiles(website_id,mode,proxy_mode,proxy_header,proxy_cidrs,requests_per_second,burst,connections,observe_started_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, p.WebsiteID, p.Mode, p.ProxyMode, p.ProxyHeader, enc(p.ProxyCIDRs), p.RequestsPerSecond, p.Burst, p.Connections, ts(p.ObserveStartedAt), ts(p.CreatedAt), ts(p.UpdatedAt))
	return err
}
func (r *Repository) SaveProfile(ctx context.Context, p WebsiteProfile) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO traffic_guard_profiles(website_id,mode,proxy_mode,proxy_header,proxy_cidrs,requests_per_second,burst,connections,observe_started_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(website_id) DO UPDATE SET mode=excluded.mode,proxy_mode=excluded.proxy_mode,proxy_header=excluded.proxy_header,proxy_cidrs=excluded.proxy_cidrs,requests_per_second=excluded.requests_per_second,burst=excluded.burst,connections=excluded.connections,observe_started_at=excluded.observe_started_at,updated_at=excluded.updated_at`, p.WebsiteID, p.Mode, p.ProxyMode, p.ProxyHeader, enc(p.ProxyCIDRs), p.RequestsPerSecond, p.Burst, p.Connections, ts(p.ObserveStartedAt), ts(p.CreatedAt), ts(p.UpdatedAt))
	return err
}
func (r *Repository) Cleanup(ctx context.Context, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM traffic_minute_buckets WHERE bucket_at<?`, ts(now.Add(-24*time.Hour)))
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `DELETE FROM traffic_hour_buckets WHERE bucket_at<?`, ts(now.Add(-30*24*time.Hour)))
	return err
}

func (r *Repository) RollupAndCleanup(ctx context.Context, now time.Time) error {
	cutoff := now.UTC().Truncate(time.Hour)
	rows, err := r.db.QueryContext(ctx, `SELECT website_id,bucket_at,requests,status_4xx,status_5xx,status_429,bytes,peak_rps FROM traffic_minute_buckets WHERE bucket_at<? ORDER BY website_id,bucket_at`, ts(cutoff))
	if err != nil {
		return err
	}
	type key struct {
		websiteID string
		hour      time.Time
	}
	rollups := map[key]HourlyBucket{}
	for rows.Next() {
		var websiteID, stamp string
		var requests, status4xx, status5xx, status429 int
		var bytes int64
		var peakRPS int
		if err := rows.Scan(&websiteID, &stamp, &requests, &status4xx, &status5xx, &status429, &bytes, &peakRPS); err != nil {
			rows.Close()
			return err
		}
		at, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			rows.Close()
			return fmt.Errorf("parse traffic bucket timestamp: %w", err)
		}
		k := key{websiteID: websiteID, hour: at.Truncate(time.Hour)}
		b := rollups[k]
		b.WebsiteID, b.BucketAt = websiteID, k.hour
		b.Requests += requests
		b.Status4xx += status4xx
		b.Status5xx += status5xx
		b.Status429 += status429
		b.Bytes += bytes
		if peakRPS > b.PeakRPS {
			b.PeakRPS = peakRPS
		}
		rollups[k] = b
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, b := range rollups {
		_, err = tx.ExecContext(ctx, `INSERT INTO traffic_hour_buckets(website_id,bucket_at,requests,status_4xx,status_5xx,status_429,bytes,peak_rps) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(website_id,bucket_at) DO UPDATE SET requests=excluded.requests,status_4xx=excluded.status_4xx,status_5xx=excluded.status_5xx,status_429=excluded.status_429,bytes=excluded.bytes,peak_rps=excluded.peak_rps`, b.WebsiteID, ts(b.BucketAt), b.Requests, b.Status4xx, b.Status5xx, b.Status429, b.Bytes, b.PeakRPS)
		if err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM traffic_minute_buckets WHERE bucket_at<?`, ts(now.Add(-24*time.Hour))); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM traffic_hour_buckets WHERE bucket_at<?`, ts(now.Add(-30*24*time.Hour))); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) SaveSnapshot(ctx context.Context, s ProxySnapshot) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO trusted_proxy_snapshots(id,ipv4_cidrs,ipv6_cidrs,fetched_at,consecutive_failures) VALUES(1,?,?,?,?) ON CONFLICT(id) DO UPDATE SET ipv4_cidrs=excluded.ipv4_cidrs,ipv6_cidrs=excluded.ipv6_cidrs,fetched_at=excluded.fetched_at,consecutive_failures=excluded.consecutive_failures`, enc(s.IPv4), enc(s.IPv6), ts(s.FetchedAt), s.ConsecutiveFailures)
	return err
}
func (r *Repository) Snapshot(ctx context.Context) (ProxySnapshot, error) {
	var s ProxySnapshot
	var v4, v6, at string
	err := r.db.QueryRowContext(ctx, `SELECT ipv4_cidrs,ipv6_cidrs,fetched_at,consecutive_failures FROM trusted_proxy_snapshots WHERE id=1`).Scan(&v4, &v6, &at, &s.ConsecutiveFailures)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("load proxy snapshot: %w", err)
	}
	_ = json.Unmarshal([]byte(v4), &s.IPv4)
	_ = json.Unmarshal([]byte(v6), &s.IPv6)
	s.FetchedAt, _ = time.Parse(time.RFC3339Nano, at)
	return s, nil
}
