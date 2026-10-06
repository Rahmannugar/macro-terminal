package emaildelivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/emaildelivery/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	cycleInterval  = 5 * time.Second
	batchSize      = 8
	maxAttempts    = 5
	maxCauseLength = 500
)

type Repository interface {
	ReclaimStaleJobs(ctx context.Context) (int64, error)
	ClaimBatch(ctx context.Context, limit int32) ([]models.ClaimedJob, error)
	Complete(ctx context.Context, id uuid.UUID) error
	Fail(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string, retryAfterSeconds float64) error
	FailPermanently(ctx context.Context, id uuid.UUID, cause string) error
	Delivery(ctx context.Context, id uuid.UUID) (models.Delivery, error)
	MarkDelivered(ctx context.Context, id uuid.UUID, providerID string) (int64, error)
	MarkTerminal(ctx context.Context, id uuid.UUID, status string, cause string) (int64, error)
	NoteError(ctx context.Context, id uuid.UUID, cause string) error
}

type Job struct {
	repository Repository
	queue      *Queue
	sender     *ResendSender
	logger     *slog.Logger
	wake       chan struct{}
}

func NewJob(repository Repository, queue *Queue, sender *ResendSender, logger *slog.Logger) *Job {
	return &Job{
		repository: repository,
		queue:      queue,
		sender:     sender,
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
				"Email delivery cycle failed",
				"event", "emaildelivery.cycle_failed",
				"error", err,
			)
		}
	}
}

type cycleStats struct {
	reclaimed   int64
	claimed     int
	delivered   int64
	failedRetry int
	failedFinal int
}

func (job *Job) cycle(ctx context.Context) error {
	stats := cycleStats{}

	reclaimed, err := job.repository.ReclaimStaleJobs(ctx)
	if err != nil {
		return err
	}
	stats.reclaimed = reclaimed

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
				"Email delivery job handling failed",
				"event", "emaildelivery.job_error",
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
	delivery, err := job.repository.Delivery(ctx, work.DeliveryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			stats.failedFinal++
			return job.repository.FailPermanently(ctx, work.ID, "email delivery row missing")
		}
		return job.backoff(ctx, work, stats, err, 0)
	}

	// A delivery that already reached a terminal state owns no more sends; the
	// outbox row is simply closed behind it.
	if delivery.Status != "pending" {
		return job.repository.Complete(ctx, work.ID)
	}

	if !time.Now().UTC().Before(delivery.ExpiresAt) {
		stats.failedFinal++
		job.logger.WarnContext(ctx,
			"Email delivery expired before it was sent",
			"event", "emaildelivery.failed",
			"outbox_id", work.ID,
			"delivery_id", work.DeliveryID,
			"reason", "expired",
		)
		_, markErr := job.repository.MarkTerminal(ctx, delivery.ID, "expired", "verification expired before delivery")
		if markErr != nil {
			return markErr
		}
		return job.repository.FailPermanently(ctx, work.ID, "verification expired before delivery")
	}

	payload, err := job.queue.Decrypt(delivery.ID, delivery.Nonce, delivery.Ciphertext)
	if err != nil {
		stats.failedFinal++
		_, markErr := job.repository.MarkTerminal(ctx, delivery.ID, "failed", "payload could not be decrypted")
		if markErr != nil {
			return markErr
		}
		return job.repository.FailPermanently(ctx, work.ID, "email payload could not be decrypted")
	}

	subject, text, ok := render(delivery.Template, payload, time.Now().UTC())
	if !ok {
		stats.failedFinal++
		_, markErr := job.repository.MarkTerminal(ctx, delivery.ID, "failed", "unknown email template")
		if markErr != nil {
			return markErr
		}
		return job.repository.FailPermanently(ctx, work.ID,
			truncate(fmt.Sprintf("unknown email template %q", delivery.Template), maxCauseLength))
	}

	providerID, err := job.sender.Send(
		ctx,
		payload.Recipient,
		subject,
		text,
		"email-delivery/"+work.DeliveryID.String(),
	)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cause := truncate(err.Error(), maxCauseLength)
		var failure SendFailure
		if errors.As(err, &failure) && !failure.Retryable() {
			stats.failedFinal++
			job.logger.WarnContext(ctx,
				"Email delivery rejected permanently",
				"event", "emaildelivery.failed",
				"outbox_id", work.ID,
				"delivery_id", work.DeliveryID,
				"reason", cause,
			)
			_, markErr := job.repository.MarkTerminal(ctx, delivery.ID, "failed", cause)
			if markErr != nil {
				return markErr
			}
			return job.repository.FailPermanently(ctx, work.ID, cause)
		}
		retryAfter := 0.0
		if errors.As(err, &failure) {
			retryAfter = float64(failure.RetryAfter().Seconds())
		}
		return job.backoff(ctx, work, stats, err, retryAfter)
	}

	rows, err := job.repository.MarkDelivered(ctx, delivery.ID, providerID)
	if err != nil {
		return job.backoff(ctx, work, stats, err, 0)
	}
	if rows == 0 {
		job.logger.WarnContext(ctx,
			"Email delivery was already finalized by another writer",
			"event", "emaildelivery.raced",
			"outbox_id", work.ID,
			"delivery_id", work.DeliveryID,
		)
	}
	if err := job.repository.Complete(ctx, work.ID); err != nil {
		return err
	}
	stats.delivered++

	job.logger.InfoContext(ctx,
		"Email delivered",
		"event", "emaildelivery.delivered",
		"outbox_id", work.ID,
		"delivery_id", work.DeliveryID,
		"attempt", work.Attempts,
	)
	return nil
}

func (job *Job) backoff(
	ctx context.Context,
	work models.ClaimedJob,
	stats *cycleStats,
	cause error,
	retryAfterSeconds float64,
) error {
	stats.failedRetry++
	job.logger.WarnContext(ctx,
		"Email delivery attempt failed",
		"event", "emaildelivery.failed",
		"outbox_id", work.ID,
		"delivery_id", work.DeliveryID,
		"attempts", work.Attempts,
		"error", truncate(cause.Error(), maxCauseLength),
	)
	noteErr := job.repository.NoteError(ctx, work.DeliveryID, truncate(cause.Error(), maxCauseLength))
	failErr := job.repository.Fail(ctx, work.ID, maxAttempts, truncate(cause.Error(), maxCauseLength), retryAfterSeconds)
	return errors.Join(cause, noteErr, failErr)
}

func (job *Job) logCycle(ctx context.Context, stats cycleStats) {
	job.logger.InfoContext(ctx,
		"Email delivery cycle finished",
		"event", "emaildelivery.cycle",
		"reclaimed", stats.reclaimed,
		"claimed", stats.claimed,
		"delivered", stats.delivered,
		"failed_retry", stats.failedRetry,
		"failed_final", stats.failedFinal,
	)
}

func render(templateName string, payload Payload, now time.Time) (string, string, bool) {
	switch templateName {
	case TemplateVerification:
		return "Your Macro Terminal sign-up code", verificationText(payload, now), true
	default:
		return "", "", false
	}
}

func verificationText(payload Payload, now time.Time) string {
	minutes := int(payload.ExpiresAt.Sub(now).Minutes() + 0.5)
	if minutes < 1 {
		minutes = 1
	}
	return fmt.Sprintf(
		"Your Macro Terminal sign-up code is %s.\n\nIt expires in %d minutes. If you did not request this code, you can ignore this email.",
		payload.Code,
		minutes,
	)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
