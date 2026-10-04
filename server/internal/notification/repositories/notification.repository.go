package repositories

import (
	"context"
	"errors"
	"fmt"

	"github.com/Rahmannugar/macro-terminal/server/internal/notification/models"
	notificationdb "github.com/Rahmannugar/macro-terminal/server/internal/notification/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool    *pgxpool.Pool
	queries *notificationdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, queries: notificationdb.New(pool)}
}

func (repository *Repository) EnqueueMissingArticleNotifications(ctx context.Context, limit int32) (int64, error) {
	queued, err := repository.queries.EnqueueMissingArticleNotifications(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("enqueue missing article notifications: %w", err)
	}
	return queued, nil
}

func (repository *Repository) EnqueueMissingCalendarEventNotifications(ctx context.Context, limit int32) (int64, error) {
	queued, err := repository.queries.EnqueueMissingCalendarEventNotifications(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("enqueue missing calendar event notifications: %w", err)
	}
	return queued, nil
}

func (repository *Repository) ReclaimStaleNotificationJobs(ctx context.Context) (int64, error) {
	reclaimed, err := repository.queries.ReclaimStaleNotificationJobs(ctx)
	if err != nil {
		return 0, fmt.Errorf("reclaim stale notification jobs: %w", err)
	}
	return reclaimed, nil
}

func (repository *Repository) ClaimBatch(ctx context.Context, limit int32) ([]models.ClaimedJob, error) {
	rows, err := repository.queries.ClaimNotificationBatch(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("claim notification batch: %w", err)
	}
	claimed := make([]models.ClaimedJob, 0, len(rows))
	for _, row := range rows {
		claimed = append(claimed, models.ClaimedJob{
			ID:          row.ID,
			SubjectType: row.SubjectType,
			SubjectID:   row.SubjectID,
			Attempts:    row.Attempts,
		})
	}
	return claimed, nil
}

func (repository *Repository) Complete(ctx context.Context, id uuid.UUID) error {
	if err := repository.queries.CompleteNotificationJob(ctx, id); err != nil {
		return fmt.Errorf("complete notification job: %w", err)
	}
	return nil
}

func (repository *Repository) Fail(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string) error {
	err := repository.queries.FailNotificationJob(ctx, notificationdb.FailNotificationJobParams{
		ID:        id,
		Attempts:  maxAttempts,
		LastError: &cause,
	})
	if err != nil {
		return fmt.Errorf("fail notification job: %w", err)
	}
	return nil
}

func (repository *Repository) FailPermanently(ctx context.Context, id uuid.UUID, cause string) error {
	err := repository.queries.FailNotificationJobPermanently(ctx, notificationdb.FailNotificationJobPermanentlyParams{
		ID:        id,
		LastError: &cause,
	})
	if err != nil {
		return fmt.Errorf("fail notification job permanently: %w", err)
	}
	return nil
}

func (repository *Repository) ArticleSubject(ctx context.Context, id uuid.UUID) (models.Subject, error) {
	row, err := repository.queries.GetNotificationSubjectForArticle(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Subject{}, fmt.Errorf("article %s: %w", id, pgx.ErrNoRows)
	}
	if err != nil {
		return models.Subject{}, fmt.Errorf("get article notification subject: %w", err)
	}
	return models.Subject{ID: row.ID, Title: row.Title}, nil
}

func (repository *Repository) CalendarEventSubject(ctx context.Context, id uuid.UUID) (models.Subject, error) {
	row, err := repository.queries.GetNotificationSubjectForCalendarEvent(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Subject{}, fmt.Errorf("calendar event %s: %w", id, pgx.ErrNoRows)
	}
	if err != nil {
		return models.Subject{}, fmt.Errorf("get calendar event notification subject: %w", err)
	}
	return models.Subject{ID: row.ID, Title: row.Title}, nil
}

func (repository *Repository) FanOutArticle(ctx context.Context, articleID uuid.UUID, title string) (int64, error) {
	created, err := repository.queries.FanOutArticleNotifications(ctx, notificationdb.FanOutArticleNotificationsParams{
		SubjectID: articleID,
		Title:     title,
	})
	if err != nil {
		return 0, fmt.Errorf("fan out article notifications: %w", err)
	}
	return created, nil
}

func (repository *Repository) FanOutCalendarEvent(ctx context.Context, eventID uuid.UUID, title string) (int64, error) {
	created, err := repository.queries.FanOutCalendarEventNotifications(ctx, notificationdb.FanOutCalendarEventNotificationsParams{
		SubjectID: eventID,
		Title:     title,
	})
	if err != nil {
		return 0, fmt.Errorf("fan out calendar event notifications: %w", err)
	}
	return created, nil
}
