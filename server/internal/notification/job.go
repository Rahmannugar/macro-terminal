package notification

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/notification/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/notification/webhook"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	cycleInterval  = 30 * time.Second
	enqueueLimit   = 16
	batchSize      = 16
	maxAttempts    = 8
	maxCauseLength = 500

	webhookEnqueueLimit  = 16
	webhookBatchSize     = 16
	webhookMaxAttempts   = 8
	webhookResponseLimit = 512

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
	EnqueueWebhookDeliveries(ctx context.Context, limit int32) (int64, error)
	ReclaimStaleWebhookJobs(ctx context.Context) (int64, error)
	ClaimWebhookBatch(ctx context.Context, limit int32) ([]models.WebhookClaim, error)
	CompleteWebhookJob(ctx context.Context, id uuid.UUID) error
	FailWebhookJob(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string) error
	FailWebhookJobPermanently(ctx context.Context, id uuid.UUID, cause string) error
	GetWebhookDelivery(ctx context.Context, notificationID uuid.UUID) (models.WebhookDelivery, error)
}

type Job struct {
	repository Repository
	logger     *slog.Logger
	httpClient *http.Client
	cipher     *webhook.Cipher
	wake       chan struct{}
}

// NewJob builds the notification job. A nil cipher (or nil httpClient)
// disables the webhook delivery phase; in-app fan-out keeps working.
func NewJob(repository Repository, logger *slog.Logger, httpClient *http.Client, cipher *webhook.Cipher) *Job {
	return &Job{
		repository: repository,
		logger:     logger,
		httpClient: httpClient,
		cipher:     cipher,
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

	webhookReclaimed       int64
	webhookEnqueued        int64
	webhookClaimed         int
	webhookSent            int
	webhookDisconnected    int
	webhookFailedBackoff   int
	webhookFailedPermanent int
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

	if err := job.webhookCycle(ctx, &stats); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		processErr = errors.Join(processErr, err)
	}

	job.logCycle(ctx, stats)
	return processErr
}

// webhookCycle claims Slack/Discord deliveries from the shared outbox and
// POSTs them. Without a cipher or HTTP client the phase is skipped, leaving
// in-app delivery untouched.
func (job *Job) webhookCycle(ctx context.Context, stats *cycleStats) error {
	if job.cipher == nil || job.httpClient == nil {
		return nil
	}

	reclaimed, err := job.repository.ReclaimStaleWebhookJobs(ctx)
	if err != nil {
		return err
	}
	stats.webhookReclaimed = reclaimed

	enqueued, err := job.repository.EnqueueWebhookDeliveries(ctx, webhookEnqueueLimit)
	if err != nil {
		return err
	}
	stats.webhookEnqueued = enqueued

	claimed, err := job.repository.ClaimWebhookBatch(ctx, webhookBatchSize)
	if err != nil {
		return err
	}
	stats.webhookClaimed = len(claimed)

	var processErr error
	for _, work := range claimed {
		if err := job.deliverWebhook(ctx, work, stats); err != nil {
			job.logger.ErrorContext(ctx,
				"Webhook delivery handling failed",
				"event", "notification.webhook_job_error",
				"outbox_id", work.ID,
				"error", err,
			)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			processErr = err
		}
	}
	return processErr
}

func (job *Job) deliverWebhook(
	ctx context.Context,
	work models.WebhookClaim,
	stats *cycleStats,
) error {
	delivery, err := job.repository.GetWebhookDelivery(ctx, work.NotificationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			stats.webhookFailedPermanent++
			return job.repository.FailWebhookJobPermanently(ctx, work.ID,
				truncate("notification not found", maxCauseLength))
		}
		stats.webhookFailedBackoff++
		return errors.Join(err, job.repository.FailWebhookJob(ctx, work.ID, webhookMaxAttempts,
			truncate(err.Error(), maxCauseLength)))
	}
	if delivery.Channel == nil {
		if err := job.repository.CompleteWebhookJob(ctx, work.ID); err != nil {
			return err
		}
		stats.webhookDisconnected++
		job.logger.InfoContext(ctx,
			"Webhook delivery skipped, channel disconnected",
			"event", "notification.webhook_disconnected",
			"outbox_id", work.ID,
			"notification_id", work.NotificationID,
		)
		return nil
	}

	endpoint, err := job.cipher.Decrypt(delivery.Nonce, delivery.Ciphertext)
	if err != nil {
		stats.webhookFailedPermanent++
		return job.repository.FailWebhookJobPermanently(ctx, work.ID,
			truncate(err.Error(), maxCauseLength))
	}
	body, err := webhook.Message(*delivery.Channel, delivery.AlertType, delivery.Title, delivery.PairSymbol)
	if err != nil {
		stats.webhookFailedPermanent++
		return job.repository.FailWebhookJobPermanently(ctx, work.ID,
			truncate(err.Error(), maxCauseLength))
	}

	if err := job.post(ctx, endpoint, body); err != nil {
		var rejected *webhookRejectedError
		if errors.As(err, &rejected) {
			stats.webhookFailedPermanent++
			job.logger.WarnContext(ctx,
				"Webhook rejected the delivery",
				"event", "notification.webhook_rejected",
				"outbox_id", work.ID,
				"notification_id", work.NotificationID,
				"channel", *delivery.Channel,
				"error", truncate(err.Error(), maxCauseLength),
			)
			return job.repository.FailWebhookJobPermanently(ctx, work.ID,
				truncate(err.Error(), maxCauseLength))
		}
		stats.webhookFailedBackoff++
		return errors.Join(err, job.repository.FailWebhookJob(ctx, work.ID, webhookMaxAttempts,
			truncate(err.Error(), maxCauseLength)))
	}

	if err := job.repository.CompleteWebhookJob(ctx, work.ID); err != nil {
		return err
	}
	stats.webhookSent++
	job.logger.InfoContext(ctx,
		"Webhook delivered",
		"event", "notification.webhook_sent",
		"outbox_id", work.ID,
		"notification_id", work.NotificationID,
		"channel", *delivery.Channel,
	)
	return nil
}

// webhookRejectedError marks a response that no retry can fix (4xx other
// than 429): the endpoint rejected the payload or no longer exists.
type webhookRejectedError struct {
	status int
	cause  string
}

func (err *webhookRejectedError) Error() string {
	return fmt.Sprintf("webhook rejected (%d): %s", err.status, err.cause)
}

func (job *Job) post(ctx context.Context, endpoint string, body []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return &webhookRejectedError{status: 0, cause: err.Error()}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "MacroTerminal/1.0")

	response, err := job.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, webhookResponseLimit))
		_ = response.Body.Close()
	}()

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	payload, _ := io.ReadAll(io.LimitReader(response.Body, webhookResponseLimit))
	cause := strings.TrimSpace(string(payload))
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return fmt.Errorf("webhook responded %d: %s", response.StatusCode, cause)
	}
	return &webhookRejectedError{status: response.StatusCode, cause: cause}
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
		"webhook_reclaimed", stats.webhookReclaimed,
		"webhook_enqueued", stats.webhookEnqueued,
		"webhook_claimed", stats.webhookClaimed,
		"webhook_sent", stats.webhookSent,
		"webhook_disconnected", stats.webhookDisconnected,
		"webhook_failed_backoff", stats.webhookFailedBackoff,
		"webhook_failed_permanent", stats.webhookFailedPermanent,
	)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
