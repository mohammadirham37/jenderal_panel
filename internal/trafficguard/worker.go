package trafficguard

import (
	"context"
	"time"
)

type Worker struct {
	collector *Collector
	updater   *CloudflareUpdater
	busy      chan struct{}
	now       func() time.Time
	lastProxy time.Time
}

func NewWorker(c *Collector, u *CloudflareUpdater) *Worker {
	return &Worker{collector: c, updater: u, busy: make(chan struct{}, 1), now: func() time.Time { return time.Now().UTC() }}
}
func (w *Worker) Tick(ctx context.Context) {
	select {
	case w.busy <- struct{}{}:
	default:
		return
	}
	defer func() { <-w.busy }()
	now := w.now()
	if w.collector != nil {
		_ = w.collector.Collect(ctx, now)
	}
	if w.updater != nil && (w.lastProxy.IsZero() || now.Sub(w.lastProxy) >= 24*time.Hour) {
		if w.updater.Refresh(ctx, now) == nil {
			w.lastProxy = now
		}
	}
}
func (w *Worker) Start(ctx context.Context) {
	go func() {
		w.Tick(ctx)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.Tick(ctx)
			}
		}
	}()
}
