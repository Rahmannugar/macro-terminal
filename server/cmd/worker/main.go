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
	entityrepositories "github.com/Rahmannugar/macro-terminal/server/internal/entities/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/telemetry"
	"github.com/Rahmannugar/macro-terminal/server/internal/ingestion"
	"github.com/Rahmannugar/macro-terminal/server/internal/mapping"
	sourcesrepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
)

const (
	databaseTimeout      = 10 * time.Second
	redisTimeout         = 5 * time.Second
	shutdownTimeout      = 15 * time.Second
	providerFetchTimeout = 30 * time.Second
	// databasePoolDefault stays modest because ingestion slots wait on
	// providers, not PostgreSQL.
	databasePoolDefault int32 = 10
)

type workerResult struct {
	name string
	err  error
}

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
	databasePool, err := database.Open(databaseContext, cfg.Database.ConnectionString(), cfg.Database.WorkerPoolOr(databasePoolDefault))
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

	sourceRepository := sourcesrepositories.NewSourceRepository(databasePool)
	entityRepository := entityrepositories.NewEntityRepository(databasePool)
	ingestionRunner := ingestion.NewRunner(
		sourceRepository,
		ingestion.NewFetcher(
			telemetry.NewHTTPClient(providerFetchTimeout),
			ingestion.NewDefaultBreaker(),
			logger,
		),
		mapping.NewLoader(entityRepository),
		logger,
		ingestion.DefaultCadences(),
	)

	signalContext, stopSignals := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM,
	)
	defer stopSignals()
	workerContext, stopWorkers := context.WithCancel(signalContext)
	defer stopWorkers()

	results := make(chan workerResult, 1)
	go func() {
		results <- workerResult{name: "ingestion-runner", err: ingestionRunner.Run(workerContext)}
	}()
	logger.Info("worker started", "jobs", 1)

	<-workerContext.Done()
	stopWorkers()
	shutdownTimer := time.NewTimer(shutdownTimeout)
	defer shutdownTimer.Stop()
	select {
	case result := <-results:
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			return fmt.Errorf("%s stopped: %w", result.name, result.err)
		}
		logger.Info("worker shutdown completed")
	case <-shutdownTimer.C:
		return fmt.Errorf("worker shutdown timed out after %s", shutdownTimeout)
	}
	return runError
}
