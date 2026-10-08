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

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
	articlerepositories "github.com/Rahmannugar/macro-terminal/server/internal/articles/repositories"
	calendarepositories "github.com/Rahmannugar/macro-terminal/server/internal/calendar/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/clustering"
	clusteringrepositories "github.com/Rahmannugar/macro-terminal/server/internal/clustering/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/config"
	"github.com/Rahmannugar/macro-terminal/server/internal/enrichment"
	enrichmentrepositories "github.com/Rahmannugar/macro-terminal/server/internal/enrichment/repositories"
	entityrepositories "github.com/Rahmannugar/macro-terminal/server/internal/entities/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/hydration"
	hydrationrepositories "github.com/Rahmannugar/macro-terminal/server/internal/hydration/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/indexing"
	indexingrepositories "github.com/Rahmannugar/macro-terminal/server/internal/indexing/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database"
	emaildelivery "github.com/Rahmannugar/macro-terminal/server/internal/infra/emaildelivery"
	emaildeliveryrepositories "github.com/Rahmannugar/macro-terminal/server/internal/infra/emaildelivery/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/telemetry"
	"github.com/Rahmannugar/macro-terminal/server/internal/ingestion"
	"github.com/Rahmannugar/macro-terminal/server/internal/mapping"
	marketrepositories "github.com/Rahmannugar/macro-terminal/server/internal/market/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/notification"
	notificationrepositories "github.com/Rahmannugar/macro-terminal/server/internal/notification/repositories"
	sourcesrepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/vector"
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
	// outboxChannel must match the channel the database trigger notifies on.
	outboxChannel = "macro_terminal_outbox"
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
	resourceStore := cache.NewJSONStore(redisClient)

	sourceRepository := sourcesrepositories.NewSourceRepository(databasePool)
	entityRepository := entityrepositories.NewEntityRepository(databasePool)
	sourceFetcher := ingestion.NewFetcher(
		telemetry.NewHTTPClient(providerFetchTimeout),
		ingestion.NewDefaultBreaker(),
		logger,
	)
	ingestionRunner := ingestion.NewRunner(
		sourceRepository,
		sourceFetcher,
		mapping.NewLoader(entityRepository),
		articlerepositories.NewArticleRepository(databasePool, resourceStore),
		calendarepositories.NewEventRepository(databasePool, resourceStore),
		marketrepositories.NewCandleRepository(databasePool),
		entityRepository,
		logger,
		ingestion.DefaultCadences(),
	)

	signalContext, stopSignals := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM,
	)
	defer stopSignals()
	workerContext, stopWorkers := context.WithCancel(signalContext)
	defer stopWorkers()

	enrichmentJob := enrichment.NewJob(
		enrichmentrepositories.NewOutboxRepository(databasePool, resourceStore),
		entityRepository,
		ai.NewClient(telemetry.NewHTTPClient(providerFetchTimeout), cfg.AI.APIKey, cfg.AI.Model),
		cfg.AI.Model,
		cfg.AI.APIKey != "",
		logger,
	)

	var articleVectors vector.Client
	if cfg.Ahnlich.AIAddr != "" {
		articleVectors, err = vector.NewClient(cfg.Ahnlich.AIAddr)
		if err != nil {
			return fmt.Errorf("connect Ahnlich: %w", err)
		}
	}
	indexingJob := indexing.NewJob(
		indexingrepositories.NewOutboxRepository(databasePool),
		articleVectors,
		cfg.Ahnlich.AIAddr != "",
		logger,
	)
	clusteringJob := clustering.NewJob(
		clusteringrepositories.NewRepository(databasePool, resourceStore),
		articleVectors,
		cfg.Ahnlich.AIAddr != "",
		logger,
	)
	notificationJob := notification.NewJob(
		notificationrepositories.NewRepository(databasePool),
		logger,
	)
	hydrationJob := hydration.NewJob(
		hydrationrepositories.NewRepository(databasePool, resourceStore),
		sourceFetcher,
		logger,
	)
	emailQueue, err := emaildelivery.NewQueue(databasePool, cfg.Auth.OTPHMACSecret)
	if err != nil {
		return fmt.Errorf("configure email delivery queue: %w", err)
	}
	emailSender, err := emaildelivery.NewResendSender(
		telemetry.NewHTTPClient(providerFetchTimeout),
		cfg.Resend.APIKey,
		cfg.Resend.NoReplyFrom,
	)
	if err != nil {
		return fmt.Errorf("configure email delivery sender: %w", err)
	}
	emailJob := emaildelivery.NewJob(
		emaildeliveryrepositories.NewRepository(databasePool),
		emailQueue,
		emailSender,
		logger,
	)

	results := make(chan workerResult, 9)
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
	go func() {
		results <- workerResult{name: "outbox-listener", err: database.Listen(
			workerContext,
			logger,
			cfg.Database.ConnectionString(),
			outboxChannel,
			func() {
				enrichmentJob.Wake()
				indexingJob.Wake()
				clusteringJob.Wake()
				notificationJob.Wake()
				hydrationJob.Wake()
				emailJob.Wake()
			},
		)}
	}()
	go func() {
		results <- workerResult{name: "enrichment-job", err: enrichmentJob.Run(workerContext)}
	}()
	go func() {
		results <- workerResult{name: "indexing-job", err: indexingJob.Run(workerContext)}
	}()
	go func() {
		results <- workerResult{name: "clustering-job", err: clusteringJob.Run(workerContext)}
	}()
	go func() {
		results <- workerResult{name: "notification-job", err: notificationJob.Run(workerContext)}
	}()
	go func() {
		results <- workerResult{name: "hydration-job", err: hydrationJob.Run(workerContext)}
	}()
	go func() {
		results <- workerResult{name: "email-delivery-job", err: emailJob.Run(workerContext)}
	}()
	logger.Info("worker started", "jobs", 9)

	<-workerContext.Done()
	stopWorkers()
	shutdownTimer := time.NewTimer(shutdownTimeout)
	defer shutdownTimer.Stop()
	for completed := 0; completed < 9; completed++ {
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
