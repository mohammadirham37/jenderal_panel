package diskusage

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// The websites list shows each site's disk footprint. Measuring every home
// directory walks all of its files, so results are cached and re-measured
// only when they are older than siteSizeTTL or the caller asks to refresh.

const siteSizeTTL = 15 * time.Minute

// siteSizeTimeout bounds one measuring pass. Like the largest-files scan it
// must outlive the executor's 30-second default on sites with big trees.
var siteSizeTimeout = 3 * time.Minute

type siteSizeEntry struct {
	bytes      int64
	measuredAt time.Time
}

// siteSizeCache holds measured home directory sizes keyed by path. measure
// serializes passes so concurrent page loads do not run du twice.
type siteSizeCache struct {
	mu      sync.Mutex
	measure sync.Mutex
	entries map[string]siteSizeEntry
}

func (c *siteSizeCache) get(home string) (siteSizeEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[home]
	return entry, ok
}

func (c *siteSizeCache) put(sizes map[string]int64, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]siteSizeEntry)
	}
	for home, bytes := range sizes {
		c.entries[home] = siteSizeEntry{bytes: bytes, measuredAt: at}
	}
}

// SiteSize is one website's measured disk footprint.
type SiteSize struct {
	ID         string    `json:"id"`
	Bytes      int64     `json:"bytes"`
	MeasuredAt time.Time `json:"measured_at"`
}

// WebsiteSizes returns the disk footprint of the websites visible to the
// caller: every site for admins, only their own for other users. Sizes come
// from the cache unless they are stale or refresh is set.
func (s *Service) WebsiteSizes(ctx context.Context, ownerID string, admin, refresh bool) ([]SiteSize, error) {
	sizes := []SiteSize{}
	if s.db == nil {
		return sizes, nil
	}
	query := `SELECT id, web_user FROM websites WHERE web_user <> ''`
	args := []any{}
	if !admin {
		query += ` AND created_by = ?`
		args = append(args, ownerID)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return sizes, fmt.Errorf("list websites: %w", err)
	}
	type site struct{ id, home string }
	var sites []site
	for rows.Next() {
		var id, webUser string
		if err := rows.Scan(&id, &webUser); err != nil {
			rows.Close()
			return sizes, fmt.Errorf("list websites: %w", err)
		}
		sites = append(sites, site{id: id, home: "/home/" + webUser})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sizes, fmt.Errorf("list websites: %w", err)
	}

	homes := make([]string, 0, len(sites))
	for _, st := range sites {
		homes = append(homes, st.home)
	}
	s.measureSites(ctx, homes, refresh)

	for _, st := range sites {
		if entry, ok := s.sizes.get(st.home); ok {
			sizes = append(sizes, SiteSize{ID: st.id, Bytes: entry.bytes, MeasuredAt: entry.measuredAt})
		}
	}
	return sizes, nil
}

// measureSites re-measures the homes whose cached size is missing or stale.
// A failed pass leaves the previous values in place.
func (s *Service) measureSites(ctx context.Context, homes []string, refresh bool) {
	s.sizes.measure.Lock()
	defer s.sizes.measure.Unlock()

	now := time.Now()
	var stale []string
	for _, home := range homes {
		entry, ok := s.sizes.get(home)
		if refresh || !ok || now.Sub(entry.measuredAt) > siteSizeTTL {
			stale = append(stale, home)
		}
	}
	if len(stale) == 0 {
		return
	}
	sort.Strings(stale)

	measureCtx, cancel := context.WithTimeout(ctx, siteSizeTimeout)
	defer cancel()
	args := append([]string{"-c", "3", "nice", "-n", "19", "du", "-sb"}, stale...)
	res, err := s.exec.RunSudo(measureCtx, "ionice", args...)
	if err != nil || res == nil {
		return
	}
	// du still prints the homes it could read when another one is missing
	// (a site still provisioning), so the partial output is kept.
	s.sizes.put(parseDuSizes(res.Stdout), time.Now())
}
