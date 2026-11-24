package security

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/depaudit"
)

// DependencyComponent is the event component of vulnerable dependencies.
const DependencyComponent = "dependencies"

// DependencyEventSink turns the findings of a completed dependency audit
// into security events: high and critical findings in production
// dependencies are recorded, and open events that are no longer found are
// resolved. Dev dependencies and lower severities stay on the
// Dependencies tab only.
type DependencyEventSink struct{ events *EventService }

// NewDependencyEventSink creates the sink.
func NewDependencyEventSink(events *EventService) *DependencyEventSink {
	return &DependencyEventSink{events: events}
}

// SyncDependencyFindings implements depaudit.EventSink.
func (d *DependencyEventSink) SyncDependencyFindings(ctx context.Context, findings []depaudit.Finding, now time.Time) error {
	active := map[string]bool{}
	for _, f := range findings {
		severity := Severity(f.Severity)
		if f.Dev || (severity != SeverityHigh && severity != SeverityCritical) {
			continue
		}
		fingerprint := "deps:" + f.WebsiteID + ":" + f.Ecosystem + ":" + f.Package + ":" + f.VulnID
		active[fingerprint] = true
		evidence, _ := json.Marshal(map[string]string{
			"package": f.Package, "version": f.Version, "fixed_version": f.FixedVersion,
			"lockfile": f.Lockfile, "advisory": f.VulnID, "aliases": strings.Join(f.Aliases, ","),
		})
		if _, _, err := d.events.Record(ctx, EventInput{
			Fingerprint:       fingerprint,
			Category:          "dependency",
			Severity:          severity,
			Component:         DependencyComponent,
			Resource:          f.Domain,
			Evidence:          string(evidence),
			RecommendedAction: dependencyAction(f),
		}, now); err != nil {
			return err
		}
	}
	open, _, err := d.events.List(ctx, EventFilter{Component: DependencyComponent, Limit: 1000})
	if err != nil {
		return err
	}
	for _, e := range open {
		if (e.Status == StatusOpen || e.Status == StatusAcknowledged) && !active[e.Fingerprint] {
			if err := d.events.ResolveFingerprint(ctx, e.Fingerprint, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func dependencyAction(f depaudit.Finding) string {
	if f.FixedVersion == "" {
		return f.Package + " " + f.Version + " (" + f.VulnID + ") has no fixed version yet; review " + f.URL
	}
	tool := "npm update " + f.Package
	if f.Ecosystem == depaudit.EcosystemPackagist {
		tool = "composer update " + f.Package
	}
	return "Update " + f.Package + " " + f.Version + " to " + f.FixedVersion + " or later (" + tool + ") and redeploy."
}
