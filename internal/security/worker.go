package security

import (
	"context"
	"fmt"
	"time"
)

type Worker struct {
	service     *Service
	events      *EventService
	busy        chan struct{}
	lastCleanup time.Time
	now         func() time.Time
}

func NewWorker(service *Service, events *EventService) *Worker {
	return &Worker{service: service, events: events, busy: make(chan struct{}, 1), now: func() time.Time { return time.Now().UTC() }}
}

func (w *Worker) Tick(ctx context.Context, now time.Time) {
	select {
	case w.busy <- struct{}{}:
	default:
		return
	}
	defer func() { <-w.busy }()
	report := w.service.RefreshPosture(ctx, now)
	active := map[string]bool{}
	for _, finding := range report.Findings {
		if finding.Severity == SeverityInfo || finding.Severity == SeverityLow {
			continue
		}
		fingerprint := "posture:" + finding.Code
		active[fingerprint] = true
		_, _, _ = w.events.Record(ctx, EventInput{Fingerprint: fingerprint, Category: "posture", Severity: finding.Severity, Component: "posture", Resource: finding.Component, Evidence: fmt.Sprintf(`{"state":%q}`, finding.State), RecommendedAction: finding.Remediation}, now)
	}
	previous, _, err := w.events.List(ctx, EventFilter{Component: "posture", Limit: 200})
	if err == nil {
		for _, event := range previous {
			if (event.Status == StatusOpen || event.Status == StatusAcknowledged) && !active[event.Fingerprint] {
				_ = w.events.ResolveFingerprint(ctx, event.Fingerprint, now)
			}
		}
	}
	if w.lastCleanup.IsZero() || now.Sub(w.lastCleanup) >= 24*time.Hour {
		if w.events.Cleanup(ctx, now) == nil {
			w.lastCleanup = now
		}
	}
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		w.Tick(ctx, w.now())
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.Tick(ctx, w.now())
			}
		}
	}()
}
