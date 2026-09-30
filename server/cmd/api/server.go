package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 15 * time.Second
)

func serveHTTP(address string, handler http.Handler, logger *slog.Logger) error {
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("api listening", "address", server.Addr)
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErrors <- err
	}()

	shutdownSignal, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		if err != nil {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-shutdownSignal.Done():
		logger.Info("api shutdown started")
	}

	// Stop accepting work first, then allow in-flight requests a bounded window
	// to finish before infrastructure dependencies are closed by run's defers.
	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	if err := <-serverErrors; err != nil {
		return fmt.Errorf("stop HTTP server: %w", err)
	}

	logger.Info("api shutdown completed")
	return nil
}
