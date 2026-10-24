package nodejs

import (
	"context"
	"fmt"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// RemoveRuntime deletes a website's NVM and Node versions and clears its
// selected Node.js version, for sites that do not need Node at all (a PHP
// site that never builds front-end assets, for example). Node websites and
// websites with Node apps are refused: their services run on that runtime.
func (s *Service) RemoveRuntime(ctx context.Context, websiteID string, log func(string)) error {
	s.runtimeGate.RLock()
	defer s.runtimeGate.RUnlock()
	unlock := s.mutations.Lock(websiteID)
	defer unlock()

	website, err := s.loadWebsiteRuntime(ctx, websiteID)
	if err != nil {
		return err
	}
	var appType string
	if err := s.db.QueryRowContext(ctx, `SELECT app_type FROM websites WHERE id = ?`, websiteID).Scan(&appType); err != nil {
		return fmt.Errorf("load website type: %w", err)
	}
	if appType == "node" || appType == "nodejs" {
		return model.NewValidationError(website.Domain + " is a Node.js website; its app needs the Node.js runtime")
	}
	apps, err := s.ListByWebsite(ctx, websiteID)
	if err != nil {
		return err
	}
	if len(apps) > 0 {
		return model.NewValidationError(fmt.Sprintf("%s has %d Node.js app(s); delete them on the Node.js page first", website.Domain, len(apps)))
	}

	if log != nil {
		log("Removing NVM, Node.js, and the npm cache for " + website.Domain)
	}
	freed, err := s.runtime.Remove(ctx, website.WebUser)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `UPDATE websites SET node_version = '', updated_at = ? WHERE id = ?`, now, websiteID); err != nil {
		return fmt.Errorf("clear website runtime: %w", err)
	}
	if log != nil {
		log(fmt.Sprintf("Removed the Node.js runtime from %s; freed %d MB", website.Domain, freed>>20))
	}
	return nil
}
