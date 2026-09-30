package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/config"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/telemetry"
)

const databaseTimeout = 10 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run() (runError error) {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	telemetryRuntime, err := telemetry.New(context.Background(), "macro-terminal-api", string(cfg.Environment))
	if err != nil {
		return fmt.Errorf("initialize telemetry: %w", err)
	}
	logger := telemetryRuntime.Logger()
	slog.SetDefault(logger)
	// Telemetry is initialized first and shut down last so dependency close and
	// HTTP shutdown failures can still be correlated and exported.
	defer func() {
		telemetryContext, cancelTelemetry := context.WithTimeout(
			context.Background(),
			shutdownTimeout,
		)
		defer cancelTelemetry()
		if err := telemetryRuntime.Shutdown(telemetryContext); err != nil {
			runError = errors.Join(runError, fmt.Errorf("shutdown telemetry: %w", err))
		}
	}()

	databaseContext, cancelDatabase := context.WithTimeout(context.Background(), databaseTimeout)
	databasePool, err := database.Open(databaseContext, cfg.Database.ConnectionString())
	cancelDatabase()
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer databasePool.Close()

	redisClient, err := cache.Open(cfg.Redis.URL)
	if err != nil {
		return fmt.Errorf("connect Redis: %w", err)
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			logger.Error("close Redis client", "error", err)
		}
	}()

	router, err := newRouter(cfg, telemetryRuntime, databasePool)
	if err != nil {
		return err
	}
	return serveHTTP(cfg.HTTP.Address(), router, logger)
}
