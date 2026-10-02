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

	articlerepositories "github.com/Rahmannugar/macro-terminal/server/internal/articles/repositories"
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
	// scheduleChangeChannel must match the channel the database trigger
	// notifies on configuration changes.
	scheduleChangeChannel = "macro_terminal_source_configurations"
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
		articlerepositories.NewArticleRepository(databasePool),
		logger,
		ingestion.DefaultCadences(),
	)

	signalContext, stopSignals := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM,
	)
	defer stopSignals()
	workerContext, stopWorkers := context.WithCancel(signalContext)
	defer stopWorkers()

	results := make(chan workerResult, 2)
	go func() {
		results <- workerResult{name: "ingestion-runner", err: ingestionRunner.Run(workerContext)}
	}()
	go func() {
		results <- workerResult{name: "schedule-listener", err: database.Listen(
			workerContext,
			logger,
			cfg.Database.ConnectionString(),
			scheduleChangeChannel,
			ingestionRunner.Wake,
		)}
	}()
	logger.Info("worker started", "jobs", 2)

	<-workerContext.Done()
	stopWorkers()
	shutdownTimer := time.NewTimer(shutdownTimeout)
	defer shutdownTimer.Stop()
	for completed := 0; completed < 2; completed++ {
		select {
		case result := <-results:
			if result.err != nil && !errors.Is(result.err, context.Canceled) {
				return fmt.Errorf("%s stopped: %w", result.name, result.err)
			}
		case <-shutdownTimer.C:
			return fmt.Errorf("worker shutdown timed out after %s", shutdownTimeout)
		}
	}
	logger.Info("worker shutdown completed")
	return runError
}
