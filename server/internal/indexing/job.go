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
	cycleInterval = 30 * time.Second
	enqueueLimit  = 16
	// batchSize is embedded in one provider call per cycle; batching keeps a
	// full day of articles inside the free embedding tier's daily request
	// budget instead of spending one request per article.
	batchSize      = 50
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
			"Article indexing disabled until the AI API key and Ahnlich address are configured",
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

type claimedArticle struct {
	claim   models.ClaimedJob
	article vector.Article
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
	loaded := make([]claimedArticle, 0, len(claimed))
	for _, work := range claimed {
		article, err := job.repository.Article(ctx, work.ArticleID)
		if err != nil {
			if ctx.Err() != nil {
				processErr = errors.Join(processErr, ctx.Err())
				break
			}
			if errors.Is(err, pgx.ErrNoRows) {
				stats.failedPermanent++
				job.logger.WarnContext(ctx,
					"Indexing payload references a missing article",
					"event", "indexing.failed",
					"outbox_id", work.ID,
					"article_id", work.ArticleID,
					"reason", "article_missing",
				)
				processErr = errors.Join(processErr, job.repository.FailPermanently(ctx, work.ID, "article not found"))
				continue
			}
			stats.failedBackoff++
			processErr = errors.Join(processErr, err, job.repository.Fail(ctx, work.ID, maxAttempts, truncate(err.Error(), maxCauseLength)))
			continue
		}
		loaded = append(loaded, claimedArticle{
			claim: work,
			article: vector.Article{
				ID:          article.ID,
				PublishedAt: publishedAt(article),
				Title:       article.Title,
				Content:     article.Content,
			},
		})
	}
	if len(loaded) == 0 {
		job.logCycle(ctx, stats)
		return processErr
	}

	started := time.Now()
	articles := make([]vector.Article, len(loaded))
	for index, item := range loaded {
		articles[index] = item.article
	}
	storeErrs, batchErr := job.indexer.StoreArticles(ctx, articles)
	if batchErr != nil {
		job.logger.WarnContext(ctx,
			"Article batch indexing failed",
			"event", "indexing.batch_failed",
			"articles", len(loaded),
			"error", truncate(batchErr.Error(), maxCauseLength),
		)
		if ctx.Err() != nil {
			job.logCycle(ctx, stats)
			return errors.Join(ctx.Err(), processErr)
		}
		for _, item := range loaded {
			if err := job.repository.Fail(ctx, item.claim.ID, maxAttempts, truncate(batchErr.Error(), maxCauseLength)); err != nil {
				processErr = errors.Join(processErr, err)
			}
			if item.claim.Attempts >= maxAttempts {
				stats.failedPermanent++
			} else {
				stats.failedBackoff++
			}
		}
		job.logCycle(ctx, stats)
		return processErr
	}

	for index, item := range loaded {
		if ctx.Err() != nil {
			processErr = errors.Join(processErr, ctx.Err())
			break
		}
		if index < len(storeErrs) && storeErrs[index] != nil {
			if err := job.fail(ctx, item.claim, &stats, storeErrs[index]); err != nil {
				processErr = errors.Join(processErr, err)
			}
			continue
		}
		if err := job.repository.Complete(ctx, item.claim.ID); err != nil {
			processErr = errors.Join(processErr, err)
			continue
		}
		stats.indexed++
	}
	if stats.indexed > 0 {
		job.logger.InfoContext(ctx,
			"Articles indexed",
			"event", "indexing.articles_indexed",
			"count", stats.indexed,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	}
	job.logCycle(ctx, stats)
	return processErr
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
