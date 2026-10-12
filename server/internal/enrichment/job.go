package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
	"github.com/Rahmannugar/macro-terminal/server/internal/enrichment/models"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	cycleInterval  = 30 * time.Second
	enqueueLimit   = 16
	batchSize      = 10
	maxAttempts    = 8
	maxCauseLength = 500
)

type Repository interface {
	EnqueueMissingArticles(ctx context.Context, limit int32) (int64, error)
	ReclaimStaleEnrichmentJobs(ctx context.Context) (int64, error)
	ClaimBatch(ctx context.Context, limit int32) ([]models.ClaimedJob, error)
	Complete(ctx context.Context, id uuid.UUID) error
	Fail(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string) error
	FailPermanently(ctx context.Context, id uuid.UUID, cause string) error
	Article(ctx context.Context, id uuid.UUID) (models.Article, error)
	StoreEnrichment(ctx context.Context, articleID uuid.UUID, model string, result json.RawMessage) (bool, error)
}

type EntityLister interface {
	ListEntities(ctx context.Context) ([]entitymodels.Entity, error)
}

type Job struct {
	repository Repository
	entities   EntityLister
	enricher   ai.Enricher
	model      string
	enabled    bool
	logger     *slog.Logger
	wake       chan struct{}
}

func NewJob(
	repository Repository,
	entities EntityLister,
	enricher ai.Enricher,
	model string,
	enabled bool,
	logger *slog.Logger,
) *Job {
	return &Job{
		repository: repository,
		entities:   entities,
		enricher:   enricher,
		model:      model,
		enabled:    enabled,
		logger:     logger,
		wake:       make(chan struct{}, 1),
	}
}

func (job *Job) Wake() {
	select {
	case job.wake <- struct{}{}:
	default:
	}
}

func (job *Job) Run(ctx context.Context) error {
	if !job.enabled {
		job.logger.InfoContext(ctx,
			"AI enrichment disabled until an API key is configured",
			"event", "ai.enrichment.skipped",
			"reason", "missing_secret",
		)
		<-ctx.Done()
		return ctx.Err()
	}
	ticker := time.NewTicker(cycleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case <-job.wake:
		}
		if err := job.cycle(ctx); err != nil && !errors.Is(err, context.Canceled) {
			job.logger.ErrorContext(ctx,
				"AI enrichment cycle failed",
				"event", "ai.enrichment.cycle_failed",
				"error", err,
			)
		}
	}
}

type cycleStats struct {
	reclaimed       int64
	enqueued        int64
	claimed         int
	enriched        int
	skippedExisting int
	failedBackoff   int
	failedPermanent int
}

func (job *Job) cycle(ctx context.Context) error {
	stats := cycleStats{}

	reclaimed, err := job.repository.ReclaimStaleEnrichmentJobs(ctx)
	if err != nil {
		return err
	}
	stats.reclaimed = reclaimed

	enqueued, err := job.repository.EnqueueMissingArticles(ctx, enqueueLimit)
	if err != nil {
		return err
	}
	stats.enqueued = enqueued
	if enqueued > 0 {
		job.logger.InfoContext(ctx,
			"Queued articles awaiting enrichment",
			"event", "ai.enrichment.backfilled",
			"enqueued", enqueued,
		)
	}

	known, codes, err := job.entityUniverse(ctx)
	if err != nil {
		return err
	}

	claimed, err := job.repository.ClaimBatch(ctx, batchSize)
	if err != nil {
		return err
	}
	stats.claimed = len(claimed)
	if len(claimed) == 0 {
		if stats.reclaimed > 0 {
			job.logCycle(ctx, &stats)
		}
		return nil
	}

	return job.processBatch(ctx, claimed, known, codes, &stats)
}

type pendingWork struct {
	work    models.ClaimedJob
	article models.Article
}

// processBatch fetches every claimed article, enriches them in a single
// provider call, then stores each result and completes its outbox row.
func (job *Job) processBatch(
	ctx context.Context,
	claimed []models.ClaimedJob,
	known map[string]struct{},
	codes []string,
	stats *cycleStats,
) error {
	articles := make([]pendingWork, 0, len(claimed))
	for _, work := range claimed {
		article, err := job.repository.Article(ctx, work.ArticleID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				stats.failedPermanent++
				job.logger.WarnContext(ctx,
					"Enrichment payload references a missing article",
					"event", "ai.enrichment.failed",
					"outbox_id", work.ID,
					"article_id", work.ArticleID,
					"reason", "article_missing",
				)
				if err := job.repository.FailPermanently(ctx, work.ID, "article not found"); err != nil {
					return err
				}
				continue
			}
			stats.failedBackoff++
			loadErr := err
			if err := job.repository.Fail(ctx, work.ID, maxAttempts, truncate(loadErr.Error(), maxCauseLength)); err != nil {
				return err
			}
			return loadErr
		}
		articles = append(articles, pendingWork{work: work, article: article})
	}

	if len(articles) == 0 {
		job.logCycle(ctx, stats)
		return nil
	}

	inputs := make([]ai.EnrichInput, len(articles))
	for i, item := range articles {
		inputs[i] = ai.EnrichInput{Title: item.article.Title, Content: item.article.Content}
	}

	started := time.Now()
	results, err := job.enricher.BatchEnrich(ctx, ai.BatchEnrichInput{
		Articles:    inputs,
		EntityCodes: codes,
	})
	if err != nil {
		return job.failBatch(ctx, articles, stats, err)
	}
	if len(results) != len(articles) {
		err := fmt.Errorf("provider returned %d results for %d articles", len(results), len(articles))
		return job.failBatch(ctx, articles, stats, err)
	}

	var processErr error
	for i, item := range articles {
		clean, report := sanitize(results[i], known)
		result, err := json.Marshal(clean)
		if err != nil {
			stats.failedBackoff++
			if err := job.repository.Fail(ctx, item.work.ID, maxAttempts, truncate(err.Error(), maxCauseLength)); err != nil && processErr == nil {
				processErr = err
			}
			continue
		}
		stored, err := job.repository.StoreEnrichment(ctx, item.article.ID, job.model, result)
		if err != nil {
			stats.failedBackoff++
			if err := job.repository.Fail(ctx, item.work.ID, maxAttempts, truncate(err.Error(), maxCauseLength)); err != nil && processErr == nil {
				processErr = err
			}
			continue
		}
		if err := job.repository.Complete(ctx, item.work.ID); err != nil {
			if processErr == nil {
				processErr = err
			}
			continue
		}
		if stored {
			stats.enriched++
		} else {
			stats.skippedExisting++
		}
		job.logger.InfoContext(ctx,
			"Article enriched",
			"event", "ai.enrichment.completed",
			"outbox_id", item.work.ID,
			"article_id", item.article.ID,
			"model", job.model,
			"entities", len(clean.Entities),
			"topics", len(clean.Topics),
			"concepts", len(clean.Concepts),
			"unknown_entities", report.UnknownEntities,
			"dropped_values", report.Dropped,
			"stored", stored,
		)
	}

	job.logger.InfoContext(ctx,
		"Batch enrichment completed",
		"event", "ai.enrichment.batch_completed",
		"batch_size", len(articles),
		"duration_ms", time.Since(started).Milliseconds(),
	)
	job.logCycle(ctx, stats)
	return processErr
}

func (job *Job) failBatch(
	ctx context.Context,
	articles []pendingWork,
	stats *cycleStats,
	cause error,
) error {
	for _, item := range articles {
		if item.work.Attempts >= maxAttempts {
			stats.failedPermanent++
		} else {
			stats.failedBackoff++
		}
		job.logger.WarnContext(ctx,
			"Enrichment attempt failed",
			"event", "ai.enrichment.failed",
			"outbox_id", item.work.ID,
			"article_id", item.article.ID,
			"attempts", item.work.Attempts,
			"error", truncate(cause.Error(), maxCauseLength),
		)
		if err := job.repository.Fail(ctx, item.work.ID, maxAttempts, truncate(cause.Error(), maxCauseLength)); err != nil {
			return err
		}
	}
	return nil
}

func (job *Job) entityUniverse(ctx context.Context) (map[string]struct{}, []string, error) {
	entities, err := job.entities.ListEntities(ctx)
	if err != nil {
		return nil, nil, err
	}
	known := make(map[string]struct{}, len(entities))
	codes := make([]string, 0, len(entities))
	for _, entity := range entities {
		if _, exists := known[entity.Code]; exists {
			continue
		}
		known[entity.Code] = struct{}{}
		codes = append(codes, entity.Code)
	}
	return known, codes, nil
}

func (job *Job) logCycle(ctx context.Context, stats *cycleStats) {
	job.logger.InfoContext(ctx,
		"AI enrichment cycle finished",
		"event", "ai.enrichment.cycle",
		"reclaimed", stats.reclaimed,
		"enqueued", stats.enqueued,
		"claimed", stats.claimed,
		"enriched", stats.enriched,
		"skipped_existing", stats.skippedExisting,
		"failed_backoff", stats.failedBackoff,
		"failed_permanent", stats.failedPermanent,
	)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
