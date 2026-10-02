package enrichment

import (
	"context"
	"encoding/json"
	"errors"
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
	batchSize      = 8
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
			job.logCycle(ctx, stats)
		}
		return nil
	}

	var processErr error
	for _, work := range claimed {
		if err := job.process(ctx, work, known, codes, &stats); err != nil {
			job.logger.ErrorContext(ctx,
				"AI enrichment job handling failed",
				"event", "ai.enrichment.job_error",
				"outbox_id", work.ID,
				"error", err,
			)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			processErr = err
		}
	}
	job.logCycle(ctx, stats)
	return processErr
}

func (job *Job) process(
	ctx context.Context,
	work models.ClaimedJob,
	known map[string]struct{},
	codes []string,
	stats *cycleStats,
) error {
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
			return job.repository.FailPermanently(ctx, work.ID, "article not found")
		}
		stats.failedBackoff++
		return errors.Join(err, job.repository.Fail(ctx, work.ID, maxAttempts, truncate(err.Error(), maxCauseLength)))
	}

	started := time.Now()
	enrichment, err := job.enricher.Enrich(ctx, ai.EnrichInput{
		Title:       article.Title,
		Content:     article.Content,
		EntityCodes: codes,
	})
	if err != nil {
		return job.fail(ctx, work, stats, err)
	}

	clean, report := sanitize(enrichment, known)
	result, err := json.Marshal(clean)
	if err != nil {
		return job.fail(ctx, work, stats, err)
	}
	stored, err := job.repository.StoreEnrichment(ctx, article.ID, job.model, result)
	if err != nil {
		return job.fail(ctx, work, stats, err)
	}
	if err := job.repository.Complete(ctx, work.ID); err != nil {
		return err
	}
	if stored {
		stats.enriched++
	} else {
		stats.skippedExisting++
	}

	job.logger.InfoContext(ctx,
		"Article enriched",
		"event", "ai.enrichment.completed",
		"outbox_id", work.ID,
		"article_id", article.ID,
		"model", job.model,
		"duration_ms", time.Since(started).Milliseconds(),
		"entities", len(clean.Entities),
		"topics", len(clean.Topics),
		"concepts", len(clean.Concepts),
		"unknown_entities", report.UnknownEntities,
		"dropped_values", report.Dropped,
		"stored", stored,
	)
	return nil
}

func (job *Job) fail(ctx context.Context, work models.ClaimedJob, stats *cycleStats, cause error) error {
	if work.Attempts >= maxAttempts {
		stats.failedPermanent++
	} else {
		stats.failedBackoff++
	}
	job.logger.WarnContext(ctx,
		"Enrichment attempt failed",
		"event", "ai.enrichment.failed",
		"outbox_id", work.ID,
		"article_id", work.ArticleID,
		"attempts", work.Attempts,
		"error", truncate(cause.Error(), maxCauseLength),
	)
	return job.repository.Fail(ctx, work.ID, maxAttempts, truncate(cause.Error(), maxCauseLength))
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

func (job *Job) logCycle(ctx context.Context, stats cycleStats) {
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
