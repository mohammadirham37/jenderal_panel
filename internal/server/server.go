package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
)

func New(cfg config.ServerConfig, handler http.Handler, logger *slog.Logger) *http.Server {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if cfg.TLS.Enabled && cfg.TLS.Cert != "" && cfg.TLS.Key != "" {
		srv.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	return srv
}

func ListenAndServe(ctx context.Context, srv *http.Server, cfg config.ServerConfig, logger *slog.Logger) error {
	errCh := make(chan error, 1)

	go func() {
		logger.Info("server starting", "addr", srv.Addr, "tls", cfg.TLS.Enabled)
		var err error
		if cfg.TLS.Enabled && cfg.TLS.Cert != "" {
			logger.Info("starting TLS server", "cert", cfg.TLS.Cert, "key", cfg.TLS.Key)
			err = srv.ListenAndServeTLS(cfg.TLS.Cert, cfg.TLS.Key)
		} else {
			logger.Info("starting HTTP server")
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server listen failed", "error", err)
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
