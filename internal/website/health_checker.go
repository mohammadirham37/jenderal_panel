package website

import (
	"context"
	"log"
	"time"
)

// HealthChecker probes enabled website health checks every minute and
// dispatches state-change notifications (down / recovered) through the
// configured notifier.
type HealthChecker struct {
	svc    *Service
	notify func(string)
}

// NewHealthChecker creates a new HealthChecker. notify receives human
// readable state-change messages (nil notifications are ignored).
func NewHealthChecker(svc *Service, notify func(string)) *HealthChecker {
	return &HealthChecker{svc: svc, notify: notify}
}

// Start launches the probe loop. It blocks until ctx is cancelled.
func (h *HealthChecker) Start(ctx context.Context) {
	go h.loop(ctx)
}

func (h *HealthChecker) loop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := h.svc.ProbeAllWebsiteHealth(ctx, h.safeNotify()); err != nil {
				log.Printf("health checker: %v", err)
			}
		}
	}
}

func (h *HealthChecker) safeNotify() func(string) {
	if h.notify == nil {
		return func(string) {}
	}
	return h.notify
}
