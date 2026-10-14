// Retention flushes articles older than the configured window along with every
// row and vector that references them, and purges aged outbox and notification
// history on the same daily pass.
package retention

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

const (
	cycleInterval = 24 * time.Hour
	// batchLimit keeps each deletion transaction short so one pass never
	// blocks concurrent reads for long.
	batchLimit = 500
	// doneOutboxRetention is the replay-free audit window for completed rows;
	// failed rows keep a longer window because an operator may still replay them.
	doneOutboxRetention    = 14 * 24 * time.Hour
	failedOutboxRetention  = 90 * 24 * time.Hour
	readNotificationRetain = 90 * 24 * time.Hour
)

type Repository interface {
	ListExpiredArticleIDs(ctx context.Context, cutoff time.Time, limit int32) ([]uuid.UUID, error)
	DeleteArticles(ctx context.Context, ids []uuid.UUID) error
	PurgeDoneOutboxRows(ctx context.Context, before time.Time) (int64, error)
	PurgeFailedOutboxRows(ctx context.Context, before time.Time) (int64, error)
	PurgeReadNotifications(ctx context.Context, before time.Time) (int64, error)
}

// VectorStore is the vector-index half the retention pass needs; a nil store
// means vector work is not configured, so only the relational rows are purged.
type VectorStore interface {
	DeleteArticles(ctx context.Context, ids []uuid.UUID) error
}

type Job struct {
	repository  Repository
	vectors     VectorStore
	articleDays int
	logger      *slog.Logger
	now         func() time.Time
}

func NewJob(
	repository Repository,
	vectors VectorStore,
	articleDays int,
	logger *slog.Logger,
) *Job {
	return &Job{
		repository:  repository,
		vectors:     vectors,
		articleDays: articleDays,
		logger:      logger,
		now:         time.Now,
	}
}

func (job *Job) Run(ctx context.Context) error {
	if job.articleDays <= 0 {
		job.logger.InfoContext(ctx,
			"Retention disabled because the article window is not positive",
			"event", "retention.skipped",
			"reason", "disabled",
		)
		<-ctx.Done()
		return ctx.Err()
	}
	ticker := time.NewTicker(cycleInterval)
	defer ticker.Stop()
	for {
		job.cycle(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (job *Job) cycle(ctx context.Context) {
	cutoff := job.now().AddDate(0, 0, -job.articleDays)
	deleted := job.expireArticles(ctx, cutoff)

	donePurged, err := job.repository.PurgeDoneOutboxRows(ctx, job.now().Add(-doneOutboxRetention))
	if err != nil {
		job.logFailure(ctx, "purge done outbox rows", err)
	}
	failedPurged, err := job.repository.PurgeFailedOutboxRows(ctx, job.now().Add(-failedOutboxRetention))
	if err != nil {
		job.logFailure(ctx, "purge failed outbox rows", err)
	}
	notificationsPurged, err := job.repository.PurgeReadNotifications(ctx, job.now().Add(-readNotificationRetain))
	if err != nil {
		job.logFailure(ctx, "purge read notifications", err)
	}

	job.logger.InfoContext(ctx,
		"Retention cycle finished",
		"event", "retention.cycle",
		"articles_deleted", deleted,
		"outbox_done_purged", donePurged,
		"outbox_failed_purged", failedPurged,
		"notifications_purged", notificationsPurged,
	)
}

// expireArticles drains every expired article in bounded batches and reports
// how many were deleted.
func (job *Job) expireArticles(ctx context.Context, cutoff time.Time) int64 {
	var deleted int64
	for {
		if err := ctx.Err(); err != nil {
			return deleted
		}
		ids, err := job.repository.ListExpiredArticleIDs(ctx, cutoff, batchLimit)
		if err != nil {
			job.logFailure(ctx, "list expired articles", err)
			return deleted
		}
		if len(ids) == 0 {
			return deleted
		}
		// Vectors go first: if the index delete fails the relational rows stay
		// put and the next pass retries, so a vector can never outlive its row.
		if job.vectors != nil {
			if err := job.vectors.DeleteArticles(ctx, ids); err != nil {
				job.logFailure(ctx, "delete article vectors", err)
				return deleted
			}
		}
		if err := job.repository.DeleteArticles(ctx, ids); err != nil {
			job.logFailure(ctx, "delete expired articles", err)
			return deleted
		}
		deleted += int64(len(ids))
	}
}

func (job *Job) logFailure(ctx context.Context, step string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	job.logger.ErrorContext(ctx,
		"Retention step failed",
		"event", "retention.step_failed",
		"step", step,
		"error", err,
	)
}
