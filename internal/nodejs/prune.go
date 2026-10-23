package nodejs

import (
	"context"
	"fmt"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/noderuntime"
)

// pruneUnlocked removes the website's unused Node versions and the nvm
// download cache. The caller holds the website's mutation lock. Failures
// are logged, not returned: the runtime in use is unaffected either way.
func (s *Service) pruneUnlocked(ctx context.Context, webUser, domain, version string, log func(string)) noderuntime.PruneResult {
	result, err := s.runtime.Prune(ctx, webUser, version, log)
	if err != nil {
		if log != nil {
			log(fmt.Sprintf("WARNING: could not remove unused Node.js versions for %s: %v", domain, err))
		}
		return result
	}
	if log != nil && result.BytesFreed > 0 {
		log(fmt.Sprintf("Freed %d MB of unused Node.js runtimes for %s", result.BytesFreed>>20, domain))
	}
	return result
}

// PruneRuntime removes the unused Node versions of one website.
func (s *Service) PruneRuntime(ctx context.Context, websiteID string, log func(string)) (noderuntime.PruneResult, error) {
	s.runtimeGate.RLock()
	defer s.runtimeGate.RUnlock()
	unlock := s.mutations.Lock(websiteID)
	defer unlock()

	website, err := s.loadWebsiteRuntime(ctx, websiteID)
	if err != nil {
		return noderuntime.PruneResult{}, err
	}
	if website.NodeVersion == "" {
		return noderuntime.PruneResult{}, model.NewValidationError(website.Domain + " has no Node.js runtime selected")
	}
	result, err := s.runtime.Prune(ctx, website.WebUser, website.NodeVersion, log)
	if err != nil {
		return result, err
	}
	if log != nil {
		log(fmt.Sprintf("%s keeps Node.js %s; freed %d MB", website.Domain, result.Kept, result.BytesFreed>>20))
	}
	return result, nil
}

// PruneAllRuntimes removes unused Node versions from every website with a
// Node.js runtime selected, continuing past failures.
func (s *Service) PruneAllRuntimes(ctx context.Context, log func(string)) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, domain FROM websites WHERE COALESCE(node_version, '') != '' ORDER BY domain`)
	if err != nil {
		return fmt.Errorf("list Node.js websites: %w", err)
	}
	type site struct{ id, domain string }
	var sites []site
	for rows.Next() {
		var st site
		if err := rows.Scan(&st.id, &st.domain); err != nil {
			rows.Close()
			return fmt.Errorf("scan Node.js website: %w", err)
		}
		sites = append(sites, st)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("read Node.js websites: %w", err)
	}

	var failed []string
	var freed int64
	for _, st := range sites {
		if log != nil {
			log("==> " + st.domain)
		}
		result, err := s.PruneRuntime(ctx, st.id, log)
		if err != nil {
			failed = append(failed, st.domain)
			if log != nil {
				log("ERROR: " + err.Error())
			}
			continue
		}
		freed += result.BytesFreed
	}
	if log != nil {
		log(fmt.Sprintf("Freed %d MB in total", freed>>20))
	}
	if len(failed) > 0 {
		return fmt.Errorf("removing unused Node.js versions failed for %s", strings.Join(failed, ", "))
	}
	return nil
}
