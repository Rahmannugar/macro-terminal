package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/config"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/telemetry"
)

const (
	databaseTimeout = 10 * time.Second
	redisTimeout    = 5 * time.Second
	shutdownTimeout = 15 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(); err != nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run() (runError error) {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	telemetryRuntime, err := telemetry.New(context.Background(), "macro-terminal-worker", string(cfg.Environment))
	if err != nil {
		return fmt.Errorf("initialize telemetry: %w", err)
	}
	logger := telemetryRuntime.Logger()
	slog.SetDefault(logger)
	// Telemetry starts before infrastructure and stops after it so shutdown
	// failures retain their trace and log correlation.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := telemetryRuntime.Shutdown(ctx); err != nil {
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
			runError = errors.Join(runError, fmt.Errorf("close Redis client: %w", err))
		}
	}()
	redisContext, cancelRedis := context.WithTimeout(context.Background(), redisTimeout)
	err = redisClient.Ping(redisContext).Err()
	cancelRedis()
	if err != nil {
		return fmt.Errorf("ping Redis: %w", err)
	}

	logger.Info("worker started", "consumers", 0)

	signalContext, stopSignals := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM,
	)
	defer stopSignals()
	<-signalContext.Done()
	logger.Info("worker shutdown completed")
	return runError
}
