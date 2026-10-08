package ingestion

import (
	"context"
	"errors"
	"log/slog"
	"time"

	articlemodels "github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	calendarmodels "github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/mapping"
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
	// mappingLogTitleLimit caps how many unmapped titles one mapping log
	// line carries, so a batch of noise cannot flood the log.
	mappingLogTitleLimit = 10
)

type ConfigurationSource interface {
	ListSourceConfigurationsWithSource(context.Context) ([]models.SourceConfigurationWithSource, error)
	MarkSourceConfigurationsRun(context.Context, []uuid.UUID, time.Time) error
}

// SourceFetcher fetches one configuration; the fetcher implements it.
type SourceFetcher interface {
	Fetch(context.Context, models.SourceConfigurationWithSource) (Result, error)
}

// DictionaryLoader loads the mapping vocabulary; the mapping loader
// implements it.
type DictionaryLoader interface {
	Load(context.Context) (mapping.Dictionary, error)
}

// ArticleStore persists normalized candidates together with their mapping
// outcomes; the articles repository implements it.
type ArticleStore interface {
	PersistArticles(
		context.Context,
		[]articlemodels.PersistEntry,
	) (articlemodels.PersistStats, error)
}

// EventStore persists classified calendar events with their entity links;
// the calendar repository implements it.
type EventStore interface {
	PersistEvents(
		context.Context,
		[]calendarmodels.PersistEntry,
	) (calendarmodels.PersistStats, error)
}

// Runner is the schedule loop. Dispatch times live in PostgreSQL, so a
// restart resumes the cadence instead of refetching everything. A source
// that fails never stops the others.
type Runner struct {
	configurations ConfigurationSource
	fetcher        SourceFetcher
	mapper         DictionaryLoader
	articles       ArticleStore
	events         EventStore
	logger         *slog.Logger
	cadences       Cadences
	tick           time.Duration
	now            func() time.Time
	wake           chan struct{}
	concurrency    int
}

// NewRunner builds the schedule loop.
func NewRunner(
	configurations ConfigurationSource,
	fetcher SourceFetcher,
	mapper DictionaryLoader,
	articles ArticleStore,
	events EventStore,
	logger *slog.Logger,
	cadences Cadences,
) *Runner {
	return &Runner{
		configurations: configurations,
		fetcher:        fetcher,
		mapper:         mapper,
		articles:       articles,
		events:         events,
		logger:         logger,
		cadences:       cadences,
		tick:           runnerTick,
		now:            time.Now,
		wake:           make(chan struct{}, 1),
		concurrency:    runnerConcurrency,
	}
}

// Run blocks until ctx is canceled. The tick is the recovery path: a
// missed notification costs at most one tick of delay.
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
		case <-runner.wake:
			runner.logger.InfoContext(ctx, "Schedule woken by change notification",
				"event", "ingestion.schedule.woken",
				"operation", "ingestion.schedule",
			)
			runner.runDue(ctx)
		}
	}
}

// Wake buffers a single re-check: a burst of calls still costs one extra
// pass.
func (runner *Runner) Wake() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

// runDue fetches configurations past their cadence. Dispatch times are
// recorded before fetching, so a slow provider cannot become due again
// early.
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
		if configuration.LastRunAt != nil &&
			now.Sub(*configuration.LastRunAt) < runner.cadences.For(configuration.SourceName, configuration.SourceType) {
			continue
		}
		due = append(due, configuration)
	}
	if len(due) == 0 {
		return
	}

	ids := make([]uuid.UUID, 0, len(due))
	for _, configuration := range due {
		ids = append(ids, configuration.ID)
	}
	if err := runner.configurations.MarkSourceConfigurationsRun(ctx, ids, now); err != nil {
		if ctx.Err() != nil {
			return
		}
		runner.logger.ErrorContext(ctx, "Failed to record schedule state",
			"event", "ingestion.schedule.mark.failed",
			"operation", "ingestion.run",
			"error", err,
		)
		return
	}

	// The vocabulary is loaded once per pass and shared read-only by the
	// concurrent fetches. A load failure only skips mapping for this pass.
	var dictionary *mapping.Dictionary
	loaded, err := runner.mapper.Load(ctx)
	switch {
	case err == nil:
		dictionary = &loaded
	case ctx.Err() == nil:
		runner.logger.ErrorContext(ctx, "Mapping vocabulary unavailable",
			"event", "ingestion.mapping.load.failed",
			"operation", "ingestion.mapping",
			"error", err,
		)
	}

	var group errgroup.Group
	group.SetLimit(runner.concurrency)
	for _, configuration := range due {
		configuration := configuration
		group.Go(func() error {
			runner.fetchOne(ctx, configuration, dictionary)
			return nil
		})
	}
	_ = group.Wait()
}

func (runner *Runner) fetchOne(
	ctx context.Context,
	configuration models.SourceConfigurationWithSource,
	dictionary *mapping.Dictionary,
) {
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
	if configuration.SourceType == "calendar" {
		runner.fetchCalendar(ctx, configuration, dictionary, result, started)
		return
	}
	feedItems := make([]normalization.FeedItem, 0, len(result.Items))
	for _, item := range result.Items {
		feedItems = append(feedItems, normalization.FeedItem{
			Title:     item.Title,
			URL:       item.URL,
			Content:   item.Content,
			Summary:   item.Summary,
			ImageURL:  item.ImageURL,
			Published: item.Published,
		})
	}
	candidates, stats := normalization.Articles(normalization.Input{
		SourceID:   configuration.SourceID,
		SourceType: configuration.SourceType,
		ConfigType: configuration.Type,
		Items:      feedItems,
		Body:       result.Body,
		BaseURL:    result.BaseURL,
		Selectors:  normalization.ParseSelectors(configuration.Config),
	})
	runner.logger.InfoContext(ctx, "Fetch succeeded",
		"event", "ingestion.fetch.succeeded",
		"operation", "ingestion.fetch",
		"source", configuration.SourceName,
		"configuration_type", configuration.Type,
		"items", stats.Candidates+stats.Duplicates+stats.Invalid,
		"bytes", len(result.Body),
		"attempts", result.Attempts,
		"status_code", result.StatusCode,
		"duration_ms", runner.now().Sub(started).Milliseconds(),
		"candidates", stats.Candidates,
		"duplicates", stats.Duplicates,
		"invalid", stats.Invalid,
	)
	runner.storeCandidates(ctx, configuration, dictionary, candidates)
}

// fetchCalendar parses a calendar payload, classifies each indicator row,
// and stores it. Rows the vocabulary cannot name are skipped and counted;
// non-calendar rows only reach the news pipeline when they carry their own
// URL. A missing vocabulary therefore stores nothing rather than guessing.
func (runner *Runner) fetchCalendar(
	ctx context.Context,
	configuration models.SourceConfigurationWithSource,
	dictionary *mapping.Dictionary,
	result Result,
	started time.Time,
) {
	events, news, calendarStats := normalization.CalendarEvents(result.Body)

	entries := make([]calendarmodels.PersistEntry, 0, len(events))
	unclassified := 0
	unmappedNames := make([]string, 0, mappingLogTitleLimit)
	for _, event := range events {
		if dictionary == nil {
			unclassified++
			continue
		}
		match, ok := dictionary.ClassifyIndicator(event.Name)
		if !ok {
			unclassified++
			if len(unmappedNames) < mappingLogTitleLimit {
				unmappedNames = append(unmappedNames, event.Name)
			}
			continue
		}
		entry := calendarmodels.PersistEntry{
			SourceID:    configuration.SourceID,
			IndicatorID: match.IndicatorID,
			EntityID:    match.EntityID,
			ScheduledAt: event.ScheduledAt,
			Previous:    event.Previous,
			Consensus:   event.Consensus,
			Actual:      event.Actual,
		}
		if event.Actual != nil {
			released := runner.now()
			entry.ReleasedAt = &released
		}
		entries = append(entries, entry)
	}

	newsCandidates, articleStats := normalization.Articles(normalization.Input{
		SourceID:   configuration.SourceID,
		SourceType: configuration.SourceType,
		ConfigType: configuration.Type,
		Items:      news,
		BaseURL:    result.BaseURL,
	})
	runner.logger.InfoContext(ctx, "Fetch succeeded",
		"event", "ingestion.fetch.succeeded",
		"operation", "ingestion.fetch",
		"source", configuration.SourceName,
		"configuration_type", configuration.Type,
		"items", calendarStats.Rows,
		"bytes", len(result.Body),
		"attempts", result.Attempts,
		"status_code", result.StatusCode,
		"duration_ms", runner.now().Sub(started).Milliseconds(),
		"candidates", len(entries)+articleStats.Candidates,
		"duplicates", articleStats.Duplicates,
		"invalid", calendarStats.Malformed+articleStats.Invalid,
		"skipped", unclassified+calendarStats.NewsSkipped,
	)

	runner.storeEvents(ctx, configuration, entries, unclassified, unmappedNames)
	runner.storeCandidates(ctx, configuration, dictionary, newsCandidates)
}

// storeEvents persists one calendar pass. Skipped rows are logged with
// the pass so an unknown indicator name stays visible instead of silently
// disappearing.
func (runner *Runner) storeEvents(
	ctx context.Context,
	configuration models.SourceConfigurationWithSource,
	entries []calendarmodels.PersistEntry,
	unclassified int,
	unmappedNames []string,
) {
	stats, err := runner.events.PersistEvents(ctx, entries)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		runner.logger.ErrorContext(ctx, "Failed to store calendar events",
			"event", "ingestion.calendar.persist.failed",
			"operation", "ingestion.calendar.persist",
			"source", configuration.SourceName,
			"configuration_type", configuration.Type,
			"error", err,
		)
		return
	}
	runner.logger.InfoContext(ctx, "Calendar events stored",
		"event", "ingestion.calendar.persisted",
		"operation", "ingestion.calendar.persist",
		"source", configuration.SourceName,
		"configuration_type", configuration.Type,
		"stored", stats.Stored,
		"links_added", stats.LinksAdded,
		"unclassified", unclassified,
		"unmapped_names", unmappedNames,
	)
}

// storeCandidates maps one fetch's candidates when a dictionary is
// available, then always persists them: an article is a canonical source
// record, so a vocabulary outage must not drop what was fetched.
func (runner *Runner) storeCandidates(
	ctx context.Context,
	configuration models.SourceConfigurationWithSource,
	dictionary *mapping.Dictionary,
	candidates []normalization.Candidate,
) {
	if len(candidates) == 0 {
		return
	}

	entries := make([]articlemodels.PersistEntry, 0, len(candidates))
	var outcomes []mapping.Outcome
	var unmappedTitles []string
	if dictionary != nil {
		outcomes = make([]mapping.Outcome, 0, len(candidates))
		unmappedTitles = make([]string, 0, mappingLogTitleLimit)
	}
	for _, candidate := range candidates {
		entry := articlemodels.PersistEntry{
			SourceID: candidate.SourceID,
			Title:    candidate.Title,
			Content:  candidate.Content,
			URL:      candidate.URL,
			ImageURL: candidate.ImageURL,
		}
		if !candidate.PublishedAt.IsZero() {
			published := candidate.PublishedAt
			entry.PublishedAt = &published
		}
		if dictionary != nil {
			outcome := dictionary.Map(candidate.Title, candidate.Content)
			outcomes = append(outcomes, outcome)
			if outcome.Mapped() {
				entry.EntityIDs = outcome.EntityIDs
			} else {
				entry.QueueUnmapped = true
				if len(unmappedTitles) < mappingLogTitleLimit {
					unmappedTitles = append(unmappedTitles, candidate.Title)
				}
			}
		}
		entries = append(entries, entry)
	}

	if dictionary != nil {
		summary := mapping.Summarize(outcomes)
		runner.logger.InfoContext(ctx, "Mapping completed",
			"event", "ingestion.mapping.completed",
			"operation", "ingestion.mapping",
			"source", configuration.SourceName,
			"configuration_type", configuration.Type,
			"candidates", len(candidates),
			"mapped", summary.Mapped,
			"unmapped", summary.Unmapped,
			"entity_codes", summary.EntityCodes,
			"affected_pairs", summary.PairSymbols,
			"unmapped_titles", unmappedTitles,
		)
	}

	stats, err := runner.articles.PersistArticles(ctx, entries)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		runner.logger.ErrorContext(ctx, "Failed to store articles",
			"event", "ingestion.articles.persist.failed",
			"operation", "ingestion.articles.persist",
			"source", configuration.SourceName,
			"configuration_type", configuration.Type,
			"error", err,
		)
		return
	}
	runner.logger.InfoContext(ctx, "Articles stored",
		"event", "ingestion.articles.persisted",
		"operation", "ingestion.articles.persist",
		"source", configuration.SourceName,
		"configuration_type", configuration.Type,
		"stored", len(entries),
		"unmapped_queued", stats.UnmappedQueued,
		"resolved", stats.Resolved,
	)
}
