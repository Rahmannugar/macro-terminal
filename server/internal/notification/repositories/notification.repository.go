package repositories

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/notification/models"
	notificationdb "github.com/Rahmannugar/macro-terminal/server/internal/notification/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

func (repository *Repository) ListUserNotificationsPage(
	ctx context.Context,
	userID uuid.UUID,
	cursor *paging.Cursor,
	limit int32,
) ([]models.UserNotification, *paging.Cursor, error) {
	params := notificationdb.ListUserNotificationsPageParams{
		UserID:   userID,
		PageSize: limit,
	}
	if cursor != nil {
		params.CursorAt = pgtype.Timestamptz{Time: cursor.At, Valid: true}
		params.CursorID = pgtype.UUID{Bytes: cursor.ID, Valid: true}
	}
	rows, err := repository.queries.ListUserNotificationsPage(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list user notifications: %w", err)
	}
	notifications := make([]models.UserNotification, 0, len(rows))
	for _, row := range rows {
		var readAt *time.Time
		if row.ReadAt.Valid {
			value := row.ReadAt.Time
			readAt = &value
		}
		notifications = append(notifications, models.UserNotification{
			ID:          row.ID,
			Type:        row.Type,
			SubjectType: row.SubjectType,
			SubjectID:   row.SubjectID,
			Title:       row.Title,
			ReadAt:      readAt,
			CreatedAt:   row.CreatedAt.Time,
			PairSymbol:  row.PairSymbol,
		})
	}
	var next *paging.Cursor
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		next = &paging.Cursor{At: last.CreatedAt.Time, ID: last.ID}
	}
	return notifications, next, nil
}

func (repository *Repository) CountUserUnreadNotifications(ctx context.Context, userID uuid.UUID) (int64, error) {
	count, err := repository.queries.CountUserUnreadNotifications(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return count, nil
}

func (repository *Repository) MarkUserNotificationRead(ctx context.Context, id, userID uuid.UUID) (int64, error) {
	updated, err := repository.queries.MarkUserNotificationRead(ctx, notificationdb.MarkUserNotificationReadParams{
		ID:     id,
		UserID: userID,
	})
	if err != nil {
		return 0, fmt.Errorf("mark notification read: %w", err)
	}
	return updated, nil
}

func (repository *Repository) MarkAllUserNotificationsRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	updated, err := repository.queries.MarkAllUserNotificationsRead(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("mark all notifications read: %w", err)
	}
	return updated, nil
}

func (repository *Repository) ListUserAlertPairIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	pairIDs, err := repository.queries.ListUserAlertPairIDs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list alert pair ids: %w", err)
	}
	return pairIDs, nil
}

func (repository *Repository) ListUserNotificationChannels(ctx context.Context, userID uuid.UUID) ([]models.UserChannel, error) {
	rows, err := repository.queries.ListUserNotificationChannels(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list notification channels: %w", err)
	}
	channels := make([]models.UserChannel, 0, len(rows))
	for _, row := range rows {
		channels = append(channels, models.UserChannel{
			Channel:     row.Channel,
			WebhookHint: row.WebhookHint,
		})
	}
	return channels, nil
}

func (repository *Repository) EnsureInAppChannel(ctx context.Context, userID uuid.UUID) error {
	if err := repository.queries.EnsureInAppChannel(ctx, userID); err != nil {
		return fmt.Errorf("ensure in-app channel: %w", err)
	}
	return nil
}

func (repository *Repository) UpsertWebhookChannel(ctx context.Context, userID uuid.UUID, channel string, nonce, ciphertext []byte, hint string) error {
	err := repository.queries.UpsertWebhookChannel(ctx, notificationdb.UpsertWebhookChannelParams{
		UserID:               userID,
		Channel:              channel,
		WebhookUrlEncrypted:  ciphertext,
		WebhookUrlNonce:      nonce,
		WebhookUrlKeyVersion: 1,
		WebhookHint:          &hint,
	})
	if err != nil {
		return fmt.Errorf("upsert webhook channel: %w", err)
	}
	return nil
}

func (repository *Repository) DeleteUserNotificationChannel(ctx context.Context, userID uuid.UUID, channel string) (int64, error) {
	deleted, err := repository.queries.DeleteUserNotificationChannel(ctx, notificationdb.DeleteUserNotificationChannelParams{
		UserID:  userID,
		Channel: channel,
	})
	if err != nil {
		return 0, fmt.Errorf("delete notification channel: %w", err)
	}
	return deleted, nil
}

func (repository *Repository) UpsertPairAlertsForConfiguredChannels(ctx context.Context, userID, entityPairID uuid.UUID) (int64, error) {
	created, err := repository.queries.UpsertPairAlertsForConfiguredChannels(ctx, notificationdb.UpsertPairAlertsForConfiguredChannelsParams{
		UserID:       userID,
		EntityPairID: entityPairID,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert pair alerts: %w", err)
	}
	return created, nil
}

func (repository *Repository) DeletePairAlerts(ctx context.Context, userID, entityPairID uuid.UUID) (int64, error) {
	deleted, err := repository.queries.DeletePairAlerts(ctx, notificationdb.DeletePairAlertsParams{
		UserID:       userID,
		EntityPairID: entityPairID,
	})
	if err != nil {
		return 0, fmt.Errorf("delete pair alerts: %w", err)
	}
	return deleted, nil
}

func (repository *Repository) DeleteChannelAlerts(ctx context.Context, userID uuid.UUID, channel string) (int64, error) {
	deleted, err := repository.queries.DeleteChannelAlerts(ctx, notificationdb.DeleteChannelAlertsParams{
		UserID:  userID,
		Channel: channel,
	})
	if err != nil {
		return 0, fmt.Errorf("delete channel alerts: %w", err)
	}
	return deleted, nil
}

func (repository *Repository) SyncChannelAlertsFromInApp(ctx context.Context, userID uuid.UUID, channel string) (int64, error) {
	created, err := repository.queries.SyncChannelAlertsFromInApp(ctx, notificationdb.SyncChannelAlertsFromInAppParams{
		UserID:  userID,
		Channel: channel,
	})
	if err != nil {
		return 0, fmt.Errorf("sync channel alerts: %w", err)
	}
	return created, nil
}

func (repository *Repository) EnqueueWebhookDeliveries(ctx context.Context, limit int32) (int64, error) {
	queued, err := repository.queries.EnqueueWebhookDeliveries(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("enqueue webhook deliveries: %w", err)
	}
	return queued, nil
}

func (repository *Repository) ReclaimStaleWebhookJobs(ctx context.Context) (int64, error) {
	reclaimed, err := repository.queries.ReclaimStaleWebhookJobs(ctx)
	if err != nil {
		return 0, fmt.Errorf("reclaim stale webhook jobs: %w", err)
	}
	return reclaimed, nil
}

func (repository *Repository) ClaimWebhookBatch(ctx context.Context, limit int32) ([]models.WebhookClaim, error) {
	rows, err := repository.queries.ClaimWebhookBatch(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("claim webhook batch: %w", err)
	}
	claimed := make([]models.WebhookClaim, 0, len(rows))
	for _, row := range rows {
		claimed = append(claimed, models.WebhookClaim{
			ID:             row.ID,
			NotificationID: row.NotificationID,
			Attempts:       row.Attempts,
		})
	}
	return claimed, nil
}

func (repository *Repository) CompleteWebhookJob(ctx context.Context, id uuid.UUID) error {
	if err := repository.queries.CompleteWebhookJob(ctx, id); err != nil {
		return fmt.Errorf("complete webhook job: %w", err)
	}
	return nil
}

func (repository *Repository) FailWebhookJob(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string) error {
	err := repository.queries.FailWebhookJob(ctx, notificationdb.FailWebhookJobParams{
		ID:        id,
		Attempts:  maxAttempts,
		LastError: &cause,
	})
	if err != nil {
		return fmt.Errorf("fail webhook job: %w", err)
	}
	return nil
}

func (repository *Repository) FailWebhookJobPermanently(ctx context.Context, id uuid.UUID, cause string) error {
	err := repository.queries.FailWebhookJobPermanently(ctx, notificationdb.FailWebhookJobPermanentlyParams{
		ID:        id,
		LastError: &cause,
	})
	if err != nil {
		return fmt.Errorf("fail webhook job permanently: %w", err)
	}
	return nil
}

func (repository *Repository) GetWebhookDelivery(ctx context.Context, notificationID uuid.UUID) (models.WebhookDelivery, error) {
	row, err := repository.queries.GetWebhookDelivery(ctx, notificationID)
	if err != nil {
		return models.WebhookDelivery{}, fmt.Errorf("get webhook delivery: %w", err)
	}
	return models.WebhookDelivery{
		Title:       row.Title,
		AlertType:   row.Type,
		PairSymbol:  row.PairSymbol,
		SubjectType: row.SubjectType,
		SubjectID:   row.SubjectID,
		ImageURL:    row.ImageUrl,
		Channel:     row.Channel,
		Nonce:       row.WebhookUrlNonce,
		Ciphertext:  row.WebhookUrlEncrypted,
	}, nil
}
