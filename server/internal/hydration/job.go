package hydration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/Rahmannugar/macro-terminal/server/internal/hydration/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/ingestion"
	"github.com/Rahmannugar/macro-terminal/server/internal/normalization"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
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
	EnqueueMissingContentJobs(ctx context.Context, limit int32) (int64, error)
	ReclaimStaleContentJobs(ctx context.Context) (int64, error)
	ClaimBatch(ctx context.Context, limit int32) ([]models.ClaimedJob, error)
	Complete(ctx context.Context, id uuid.UUID) error
	Fail(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string) error
	FailPermanently(ctx context.Context, id uuid.UUID, cause string) error
	ContentTarget(ctx context.Context, id uuid.UUID) (models.ContentTarget, error)
	StoreContent(ctx context.Context, id uuid.UUID, content string) (int64, error)
}

type Fetcher interface {
	Fetch(ctx context.Context, configuration sourcemodels.SourceConfigurationWithSource) (ingestion.Result, error)
}

type Job struct {
	repository Repository
	fetcher    Fetcher
	logger     *slog.Logger
	wake       chan struct{}
}

func NewJob(repository Repository, fetcher Fetcher, logger *slog.Logger) *Job {
	return &Job{
		repository: repository,
		fetcher:    fetcher,
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
				"Content hydration cycle failed",
				"event", "hydration.cycle_failed",
				"error", err,
			)
		}
	}
}

type cycleStats struct {
	reclaimed       int64
	enqueued        int64
	claimed         int
	hydrated        int64
	failedBackoff   int
	failedPermanent int
}

func (job *Job) cycle(ctx context.Context) error {
	stats := cycleStats{}

	reclaimed, err := job.repository.ReclaimStaleContentJobs(ctx)
	if err != nil {
		return err
	}
	stats.reclaimed = reclaimed

	enqueued, err := job.repository.EnqueueMissingContentJobs(ctx, enqueueLimit)
	if err != nil {
		return err
	}
	stats.enqueued = enqueued
	if enqueued > 0 {
		job.logger.InfoContext(ctx,
			"Queued articles awaiting content hydration",
			"event", "hydration.backfilled",
			"articles", enqueued,
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
	for _, work := range claimed {
		if err := job.process(ctx, work, &stats); err != nil {
			job.logger.ErrorContext(ctx,
				"Content hydration job handling failed",
				"event", "hydration.job_error",
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
	stats *cycleStats,
) error {
	target, err := job.repository.ContentTarget(ctx, work.ArticleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			stats.failedPermanent++
			job.logger.WarnContext(ctx,
				"Content job references a missing article",
				"event", "hydration.failed",
				"outbox_id", work.ID,
				"article_id", work.ArticleID,
				"reason", "article_missing",
			)
			return job.repository.FailPermanently(ctx, work.ID,
				truncate(fmt.Sprintf("article %s not found", work.ArticleID), maxCauseLength))
		}
		return job.backoff(ctx, work, stats, err)
	}

	selector := normalization.ParseSelectors(target.Config).Content

	request, err := articleRequest(target)
	if err != nil {
		stats.failedPermanent++
		return job.repository.FailPermanently(ctx, work.ID,
			truncate(err.Error(), maxCauseLength))
	}

	result, err := job.fetcher.Fetch(ctx, request)
	if err != nil {
		var failure ingestion.Failure
		if errors.As(err, &failure) && !failure.Retryable() {
			stats.failedPermanent++
			job.logger.WarnContext(ctx,
				"Article page fetch is not retryable",
				"event", "hydration.failed",
				"outbox_id", work.ID,
				"article_id", work.ArticleID,
				"error", truncate(err.Error(), maxCauseLength),
			)
			return job.repository.FailPermanently(ctx, work.ID,
				truncate(err.Error(), maxCauseLength))
		}
		return job.backoff(ctx, work, stats, err)
	}

	content, err := extractContent(result.Body, selector)
	if err != nil {
		if selector == "" {
			// Generic extraction is a heuristic: a thin or badly marked-up
			// page may extract better on a later attempt.
			return job.backoff(ctx, work, stats, err)
		}
		stats.failedPermanent++
		job.logger.WarnContext(ctx,
			"Article page content could not be extracted",
			"event", "hydration.failed",
			"outbox_id", work.ID,
			"article_id", work.ArticleID,
			"selector", selector,
			"reason", "content_extraction_failed",
			"error", truncate(err.Error(), maxCauseLength),
		)
		return job.repository.FailPermanently(ctx, work.ID,
			truncate(err.Error(), maxCauseLength))
	}

	stored, err := job.repository.StoreContent(ctx, work.ArticleID, content)
	if err != nil {
		return job.backoff(ctx, work, stats, err)
	}
	if err := job.repository.Complete(ctx, work.ID); err != nil {
		return err
	}
	stats.hydrated += stored

	job.logger.InfoContext(ctx,
		"Article content hydrated",
		"event", "hydration.completed",
		"outbox_id", work.ID,
		"article_id", work.ArticleID,
		"stored", stored,
	)
	return nil
}

func (job *Job) backoff(
	ctx context.Context,
	work models.ClaimedJob,
	stats *cycleStats,
	cause error,
) error {
	stats.failedBackoff++
	job.logger.WarnContext(ctx,
		"Content hydration attempt failed",
		"event", "hydration.failed",
		"outbox_id", work.ID,
		"article_id", work.ArticleID,
		"attempts", work.Attempts,
		"error", truncate(cause.Error(), maxCauseLength),
	)
	return errors.Join(cause, job.repository.Fail(ctx, work.ID, maxAttempts, truncate(cause.Error(), maxCauseLength)))
}

func (job *Job) logCycle(ctx context.Context, stats cycleStats) {
	job.logger.InfoContext(ctx,
		"Content hydration cycle finished",
		"event", "hydration.cycle",
		"reclaimed", stats.reclaimed,
		"enqueued", stats.enqueued,
		"claimed", stats.claimed,
		"hydrated", stats.hydrated,
		"failed_backoff", stats.failedBackoff,
		"failed_permanent", stats.failedPermanent,
	)
}

// articleRequest points the shared ingestion fetcher at the article's own
// page: the source's configuration supplies spacing and headers, with the
// request URL swapped for the article's. The page is fetched as a web page
// whatever the configuration's listing type is.
func articleRequest(target models.ContentTarget) (sourcemodels.SourceConfigurationWithSource, error) {
	var document map[string]any
	if err := json.Unmarshal(target.Config, &document); err != nil {
		return sourcemodels.SourceConfigurationWithSource{}, fmt.Errorf("decode source configuration: %w", err)
	}
	document["url"] = target.ArticleURL
	merged, err := json.Marshal(document)
	if err != nil {
		return sourcemodels.SourceConfigurationWithSource{}, fmt.Errorf("encode article request configuration: %w", err)
	}
	return sourcemodels.SourceConfigurationWithSource{
		SourceConfiguration: sourcemodels.SourceConfiguration{
			ID:       target.ConfigurationID,
			SourceID: target.SourceID,
			Type:     "web",
			Config:   merged,
		},
		SourceName: target.SourceName,
		SourceType: target.SourceType,
	}, nil
}

// extractContent pulls the article body: the configured content selector
// when the source has one, the generic extraction otherwise. A page the
// selector does not fit fails permanently — the same markup will be served
// on every retry.
func extractContent(body []byte, selector string) (string, error) {
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("parse article page: %w", err)
	}
	if selector == "" {
		return extractGenericContent(document)
	}
	match := document.Find(selector).First()
	if match.Length() == 0 {
		return "", fmt.Errorf("no element matches content selector %q", selector)
	}
	html, err := match.Html()
	if err != nil {
		return "", fmt.Errorf("read content element: %w", err)
	}
	content := strings.TrimSpace(html)
	if content == "" {
		return "", fmt.Errorf("content selector %q matched an empty element", selector)
	}
	return content, nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
