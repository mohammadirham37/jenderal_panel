package ssl

import (
	"context"
	"log/slog"
	"time"
)

// RenewalWorker periodically checks for certificates that are about to expire
// and renews them automatically.
type RenewalWorker struct {
	svc *Service
}

// NewRenewalWorker creates a new RenewalWorker.
func NewRenewalWorker(svc *Service) *RenewalWorker {
	return &RenewalWorker{svc: svc}
}

// Start begins the renewal loop. It checks every 24 hours for certificates
// that expire within 14 days and have auto_renew enabled. The loop runs until
// the context is cancelled.
func (w *RenewalWorker) Start(ctx context.Context) {
	go w.run(ctx)
}

func (w *RenewalWorker) run(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Run once immediately on start.
	w.renewExpiring(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("ssl renewal worker stopped")
			return
		case <-ticker.C:
			w.renewExpiring(ctx)
		}
	}
}

// renewExpiring fetches certificates expiring within 14 days and renews each one.
func (w *RenewalWorker) renewExpiring(ctx context.Context) {
	certs, err := w.svc.GetExpiringCerts(ctx, 14)
	if err != nil {
		slog.Error("ssl renewal: failed to get expiring certs", "error", err)
		return
	}

	if len(certs) == 0 {
		slog.Debug("ssl renewal: no certificates need renewal")
		return
	}

	slog.Info("ssl renewal: found certificates to renew", "count", len(certs))

	for _, cert := range certs {
		slog.Info("ssl renewal: renewing certificate", "id", cert.ID, "domain", cert.Domain, "expires_at", cert.ExpiresAt)

		if err := w.svc.Renew(ctx, cert.ID); err != nil {
			slog.Error("ssl renewal: failed to renew certificate",
				"id", cert.ID,
				"domain", cert.Domain,
				"error", err,
			)
			continue
		}

		slog.Info("ssl renewal: successfully renewed certificate", "id", cert.ID, "domain", cert.Domain)
	}
}
