package notification

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/notification/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	cycleInterval  = 30 * time.Second
	enqueueLimit   = 16
	batchSize      = 16
	maxAttempts    = 8
	maxCauseLength = 500

	subjectArticle       = "article"
	subjectCalendarEvent = "calendar_event"
)

type Repository interface {
	EnqueueMissingArticleNotifications(ctx context.Context, limit int32) (int64, error)
	EnqueueMissingCalendarEventNotifications(ctx context.Context, limit int32) (int64, error)
	ReclaimStaleNotificationJobs(ctx context.Context) (int64, error)
	ClaimBatch(ctx context.Context, limit int32) ([]models.ClaimedJob, error)
	Complete(ctx context.Context, id uuid.UUID) error
	Fail(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string) error
	FailPermanently(ctx context.Context, id uuid.UUID, cause string) error
	ArticleSubject(ctx context.Context, id uuid.UUID) (models.Subject, error)
	CalendarEventSubject(ctx context.Context, id uuid.UUID) (models.Subject, error)
	FanOutArticle(ctx context.Context, articleID uuid.UUID, title string) (int64, error)
	FanOutCalendarEvent(ctx context.Context, eventID uuid.UUID, title string) (int64, error)
}

type Job struct {
	repository Repository
	logger     *slog.Logger
	wake       chan struct{}
}

func NewJob(repository Repository, logger *slog.Logger) *Job {
	return &Job{
		repository: repository,
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
				"Notification cycle failed",
				"event", "notification.cycle_failed",
				"error", err,
			)
		}
	}
}

type cycleStats struct {
	reclaimed       int64
	enqueuedArticle int64
	enqueuedEvent   int64
	claimed         int
	fannedOut       int
	created         int64
	failedBackoff   int
	failedPermanent int
}

func (job *Job) cycle(ctx context.Context) error {
	stats := cycleStats{}

	reclaimed, err := job.repository.ReclaimStaleNotificationJobs(ctx)
	if err != nil {
		return err
	}
	stats.reclaimed = reclaimed

	enqueued, err := job.repository.EnqueueMissingArticleNotifications(ctx, enqueueLimit)
	if err != nil {
		return err
	}
	stats.enqueuedArticle = enqueued

	enqueuedEvents, err := job.repository.EnqueueMissingCalendarEventNotifications(ctx, enqueueLimit)
	if err != nil {
		return err
	}
	stats.enqueuedEvent = enqueuedEvents
	if enqueued+enqueuedEvents > 0 {
		job.logger.InfoContext(ctx,
			"Queued mapped subjects awaiting notification fan-out",
			"event", "notification.backfilled",
			"articles", enqueued,
			"calendar_events", enqueuedEvents,
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
				"Notification job handling failed",
				"event", "notification.job_error",
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
	var (
		subject models.Subject
		created int64
		err     error
	)
	switch work.SubjectType {
	case subjectArticle:
		subject, err = job.repository.ArticleSubject(ctx, work.SubjectID)
		if err == nil {
			created, err = job.repository.FanOutArticle(ctx, subject.ID, subject.Title)
		}
	case subjectCalendarEvent:
		subject, err = job.repository.CalendarEventSubject(ctx, work.SubjectID)
		if err == nil {
			created, err = job.repository.FanOutCalendarEvent(ctx, subject.ID, subject.Title)
		}
	default:
		stats.failedPermanent++
		job.logger.WarnContext(ctx,
			"Notification payload references an unknown subject type",
			"event", "notification.failed",
			"outbox_id", work.ID,
			"subject_type", work.SubjectType,
			"reason", "unknown_subject_type",
		)
		return job.repository.FailPermanently(ctx, work.ID,
			truncate(fmt.Sprintf("unknown subject type %q", work.SubjectType), maxCauseLength))
	}

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			stats.failedPermanent++
			job.logger.WarnContext(ctx,
				"Notification payload references a missing subject",
				"event", "notification.failed",
				"outbox_id", work.ID,
				"subject_type", work.SubjectType,
				"subject_id", work.SubjectID,
				"reason", "subject_missing",
			)
			return job.repository.FailPermanently(ctx, work.ID,
				truncate(fmt.Sprintf("%s not found", work.SubjectType), maxCauseLength))
		}
		stats.failedBackoff++
		job.logger.WarnContext(ctx,
			"Notification attempt failed",
			"event", "notification.failed",
			"outbox_id", work.ID,
			"subject_type", work.SubjectType,
			"subject_id", work.SubjectID,
			"attempts", work.Attempts,
			"error", truncate(err.Error(), maxCauseLength),
		)
		return errors.Join(err, job.repository.Fail(ctx, work.ID, maxAttempts, truncate(err.Error(), maxCauseLength)))
	}

	if err := job.repository.Complete(ctx, work.ID); err != nil {
		return err
	}
	stats.fannedOut++
	stats.created += created

	job.logger.InfoContext(ctx,
		"Notifications fanned out",
		"event", "notification.fanned_out",
		"outbox_id", work.ID,
		"subject_type", work.SubjectType,
		"subject_id", work.SubjectID,
		"created", created,
	)
	return nil
}

func (job *Job) logCycle(ctx context.Context, stats cycleStats) {
	job.logger.InfoContext(ctx,
		"Notification cycle finished",
		"event", "notification.cycle",
		"reclaimed", stats.reclaimed,
		"enqueued_articles", stats.enqueuedArticle,
		"enqueued_calendar_events", stats.enqueuedEvent,
		"claimed", stats.claimed,
		"fanned_out", stats.fannedOut,
		"created", stats.created,
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
