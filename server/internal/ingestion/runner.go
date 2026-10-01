package ingestion

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/normalization"
	"github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

const (
	// runnerTick is how often the scheduler looks for due sources: every
	// minute it fetches everything that is overdue.
	runnerTick = time.Minute
	// runnerConcurrency is how many fetches run at the same time.
	runnerConcurrency = 4
)

// ConfigurationSource loads the source configurations to schedule, one row
// per configuration.
type ConfigurationSource interface {
	ListSourceConfigurationsWithSource(context.Context) ([]models.SourceConfigurationWithSource, error)
}

// SourceFetcher fetches one configuration; the fetcher implements it.
type SourceFetcher interface {
	Fetch(context.Context, models.SourceConfigurationWithSource) (Result, error)
}

// Runner is the schedule loop. It runs one pass immediately at boot so
// restarts catch up, then wakes every tick. A source that fails never
// stops the others.
type Runner struct {
	configurations ConfigurationSource
	fetcher        SourceFetcher
	logger         *slog.Logger
	cadences       Cadences
	tick           time.Duration
	now            func() time.Time
	lastDispatched map[uuid.UUID]time.Time
	concurrency    int
}

// NewRunner builds the schedule loop.
func NewRunner(
	configurations ConfigurationSource,
	fetcher SourceFetcher,
	logger *slog.Logger,
	cadences Cadences,
) *Runner {
	return &Runner{
		configurations: configurations,
		fetcher:        fetcher,
		logger:         logger,
		cadences:       cadences,
		tick:           runnerTick,
		now:            time.Now,
		lastDispatched: map[uuid.UUID]time.Time{},
		concurrency:    runnerConcurrency,
	}
}

// Run blocks until ctx is canceled and fetches due sources every tick.
func (runner *Runner) Run(ctx context.Context) error {
	runner.runDue(ctx)
	ticker := time.NewTicker(runner.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runner.runDue(ctx)
		}
	}
}

// runDue fetches every configuration whose cadence has passed. Last-run
// times are recorded before fetching, so a slow provider cannot become due
// again early.
func (runner *Runner) runDue(ctx context.Context) {
	configurations, err := runner.configurations.ListSourceConfigurationsWithSource(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		runner.logger.ErrorContext(ctx, "Failed to load source configurations",
			"event", "ingestion.configurations.load.failed",
			"operation", "ingestion.run",
			"error", err,
		)
		return
	}

	now := runner.now()
	var due []models.SourceConfigurationWithSource
	for _, configuration := range configurations {
		last := runner.lastDispatched[configuration.ID]
		if now.Sub(last) < runner.cadences.For(configuration.SourceName, configuration.SourceType) {
			continue
		}
		runner.lastDispatched[configuration.ID] = now
		due = append(due, configuration)
	}
	if len(due) == 0 {
		return
	}

	var group errgroup.Group
	group.SetLimit(runner.concurrency)
	for _, configuration := range due {
		configuration := configuration
		group.Go(func() error {
			runner.fetchOne(ctx, configuration)
			return nil
		})
	}
	_ = group.Wait()
}

func (runner *Runner) fetchOne(ctx context.Context, configuration models.SourceConfigurationWithSource) {
	started := runner.now()
	result, err := runner.fetcher.Fetch(ctx, configuration)
	if err != nil {
		switch {
		case errors.Is(err, ErrCircuitOpen):
			runner.logger.InfoContext(ctx, "Fetch skipped: circuit breaker open",
				"event", "ingestion.fetch.skipped",
				"operation", "ingestion.fetch",
				"reason", "circuit_open",
				"source", configuration.SourceName,
				"configuration_type", configuration.Type,
			)
		case errors.Is(err, ErrSecretMissing):
			runner.logger.WarnContext(ctx, "Fetch skipped: secret not configured",
				"event", "ingestion.fetch.skipped",
				"operation", "ingestion.fetch",
				"reason", "missing_secret",
				"source", configuration.SourceName,
				"configuration_type", configuration.Type,
				"error", err,
			)
		case ctx.Err() != nil:
			return
		default:
			runner.logger.ErrorContext(ctx, "Fetch failed",
				"event", "ingestion.fetch.failed",
				"operation", "ingestion.fetch",
				"source", configuration.SourceName,
				"configuration_type", configuration.Type,
				"malformed", errors.Is(err, ErrMalformed),
				"duration_ms", runner.now().Sub(started).Milliseconds(),
				"error", err,
			)
		}
		return
	}
	feedItems := make([]normalization.FeedItem, 0, len(result.Items))
	for _, item := range result.Items {
		feedItems = append(feedItems, normalization.FeedItem{
			Title:     item.Title,
			URL:       item.URL,
			Summary:   item.Summary,
			Published: item.Published,
		})
	}
	_, stats := normalization.Articles(normalization.Input{
		SourceID:   configuration.SourceID,
		SourceType: configuration.SourceType,
		ConfigType: configuration.Type,
		Items:      feedItems,
		Body:       result.Body,
		BaseURL:    result.BaseURL,
	})
	runner.logger.InfoContext(ctx, "Fetch succeeded",
		"event", "ingestion.fetch.succeeded",
		"operation", "ingestion.fetch",
		"source", configuration.SourceName,
		"configuration_type", configuration.Type,
		"items", len(result.Items),
		"bytes", len(result.Body),
		"attempts", result.Attempts,
		"status_code", result.StatusCode,
		"duration_ms", runner.now().Sub(started).Milliseconds(),
		"candidates", stats.Candidates,
		"duplicates", stats.Duplicates,
		"invalid", stats.Invalid,
	)
}
