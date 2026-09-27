package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
)

func Run(cfg config.ServerConfig, handler http.Handler, logger *slog.Logger) error {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	srv := &http.Server{
		Handler: handler,
		// Large database uploads (up to 2 GB) and long restores or exports
		// legitimately run for a long time: ReadTimeout covers the whole
		// request body and WriteTimeout the whole handler, so both need
		// generous bounds — a short write deadline aborts the connection
		// mid-import and surfaces as 502 behind the panel domain proxy.
		// ReadHeaderTimeout stays tight to keep slow-header clients out.
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       30 * time.Minute,
		WriteTimeout:      2 * time.Hour,
		IdleTimeout:       120 * time.Second,
	}

	if cfg.TLS.Enabled && cfg.TLS.Cert != "" && cfg.TLS.Key != "" {
		srv.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	// Create listener first so we know the port is bound
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	logger.Info("server listening", "addr", addr, "tls", cfg.TLS.Enabled)
	fmt.Fprintf(os.Stderr, "Jenderal Panel listening on %s\n", addr)

	// Start serving in goroutine
	errCh := make(chan error, 1)
	go func() {
		var serveErr error
		if cfg.TLS.Enabled && cfg.TLS.Cert != "" {
			// ServeTLS handles TLS on a plain listener
			serveErr = srv.ServeTLS(ln, cfg.TLS.Cert, cfg.TLS.Key)
		} else {
			serveErr = srv.Serve(ln)
		}
		errCh <- serveErr
	}()

	// Wait for signal or error
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("received signal, shutting down", "signal", sig)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}

	// Graceful shutdown
	fmt.Fprintln(os.Stderr, "Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
