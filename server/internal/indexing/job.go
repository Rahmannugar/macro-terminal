package indexing

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/indexing/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/vector"
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
	EnqueueMissingIndexJobs(ctx context.Context, limit int32) (int64, error)
	ReclaimStaleIndexJobs(ctx context.Context) (int64, error)
	ClaimBatch(ctx context.Context, limit int32) ([]models.ClaimedJob, error)
	Complete(ctx context.Context, id uuid.UUID) error
	Fail(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string) error
	FailPermanently(ctx context.Context, id uuid.UUID, cause string) error
	Article(ctx context.Context, id uuid.UUID) (models.Article, error)
}

type Job struct {
	repository Repository
	indexer    vector.Indexer
	enabled    bool
	logger     *slog.Logger
	wake       chan struct{}
}

func NewJob(
	repository Repository,
	indexer vector.Indexer,
	enabled bool,
	logger *slog.Logger,
) *Job {
	return &Job{
		repository: repository,
		indexer:    indexer,
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
			"Article indexing disabled until the Ahnlich address is configured",
			"event", "indexing.skipped",
			"reason", "missing_config",
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
				"Article indexing cycle failed",
				"event", "indexing.cycle_failed",
				"error", err,
			)
		}
	}
}

type cycleStats struct {
	reclaimed       int64
	enqueued        int64
	claimed         int
	indexed         int
	failedBackoff   int
	failedPermanent int
}

func (job *Job) cycle(ctx context.Context) error {
	stats := cycleStats{}

	reclaimed, err := job.repository.ReclaimStaleIndexJobs(ctx)
	if err != nil {
		return err
	}
	stats.reclaimed = reclaimed

	enqueued, err := job.repository.EnqueueMissingIndexJobs(ctx, enqueueLimit)
	if err != nil {
		return err
	}
	stats.enqueued = enqueued
	if enqueued > 0 {
		job.logger.InfoContext(ctx,
			"Queued articles awaiting indexing",
			"event", "indexing.backfilled",
			"enqueued", enqueued,
		)
	}

	var processErr error
	for {
		claimed, err := job.repository.ClaimBatch(ctx, batchSize)
		if err != nil {
			return errors.Join(processErr, err)
		}
		stats.claimed = len(claimed)
		if len(claimed) == 0 {
			if stats.reclaimed > 0 {
				job.logCycle(ctx, stats)
			}
			return processErr
		}

		for _, work := range claimed {
			if err := job.process(ctx, work, &stats); err != nil {
				job.logger.ErrorContext(ctx,
					"Article indexing job handling failed",
					"event", "indexing.job_error",
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
		if len(claimed) < batchSize {
			return processErr
		}
		stats = cycleStats{}
	}
}

func (job *Job) process(ctx context.Context, work models.ClaimedJob, stats *cycleStats) error {
	article, err := job.repository.Article(ctx, work.ArticleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			stats.failedPermanent++
			job.logger.WarnContext(ctx,
				"Indexing payload references a missing article",
				"event", "indexing.failed",
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
	if err := job.indexer.StoreArticle(ctx, vector.Article{
		ID:          article.ID,
		PublishedAt: publishedAt(article),
		Title:       article.Title,
		Content:     article.Content,
	}); err != nil {
		return job.fail(ctx, work, stats, err)
	}
	if err := job.repository.Complete(ctx, work.ID); err != nil {
		return err
	}
	stats.indexed++

	job.logger.InfoContext(ctx,
		"Article indexed",
		"event", "indexing.article_indexed",
		"outbox_id", work.ID,
		"article_id", article.ID,
		"duration_ms", time.Since(started).Milliseconds(),
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
		"Indexing attempt failed",
		"event", "indexing.failed",
		"outbox_id", work.ID,
		"article_id", work.ArticleID,
		"attempts", work.Attempts,
		"error", truncate(cause.Error(), maxCauseLength),
	)
	return job.repository.Fail(ctx, work.ID, maxAttempts, truncate(cause.Error(), maxCauseLength))
}

func (job *Job) logCycle(ctx context.Context, stats cycleStats) {
	job.logger.InfoContext(ctx,
		"Article indexing cycle finished",
		"event", "indexing.cycle",
		"reclaimed", stats.reclaimed,
		"enqueued", stats.enqueued,
		"claimed", stats.claimed,
		"indexed", stats.indexed,
		"failed_backoff", stats.failedBackoff,
		"failed_permanent", stats.failedPermanent,
	)
}

func publishedAt(article models.Article) time.Time {
	if article.PublishedAt == nil {
		return time.Time{}
	}
	return *article.PublishedAt
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
