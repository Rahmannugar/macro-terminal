package repositories

import (
	"context"
	"errors"
	"fmt"

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/emaildelivery/models"
	emaildeliverydb "github.com/Rahmannugar/macro-terminal/server/internal/infra/emaildelivery/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	queries *emaildeliverydb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{queries: emaildeliverydb.New(pool)}
}

func (repository *Repository) ReclaimStaleJobs(ctx context.Context) (int64, error) {
	reclaimed, err := repository.queries.ReclaimStaleEmailJobs(ctx)
	if err != nil {
		return 0, fmt.Errorf("reclaim stale email jobs: %w", err)
	}
	return reclaimed, nil
}

func (repository *Repository) ClaimBatch(ctx context.Context, limit int32) ([]models.ClaimedJob, error) {
	rows, err := repository.queries.ClaimEmailBatch(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("claim email batch: %w", err)
	}
	claimed := make([]models.ClaimedJob, 0, len(rows))
	for _, row := range rows {
		claimed = append(claimed, models.ClaimedJob{
			ID:         row.ID,
			DeliveryID: row.DeliveryID,
			Attempts:   row.Attempts,
		})
	}
	return claimed, nil
}

func (repository *Repository) Complete(ctx context.Context, id uuid.UUID) error {
	if err := repository.queries.CompleteEmailJob(ctx, id); err != nil {
		return fmt.Errorf("complete email job: %w", err)
	}
	return nil
}

func (repository *Repository) Fail(
	ctx context.Context,
	id uuid.UUID,
	maxAttempts int32,
	cause string,
	retryAfterSeconds float64,
) error {
	if err := repository.queries.FailEmailJob(ctx, emaildeliverydb.FailEmailJobParams{
		ID: id, Attempts: maxAttempts, LastError: &cause, RetryAfterSecs: retryAfterSeconds,
	}); err != nil {
		return fmt.Errorf("fail email job: %w", err)
	}
	return nil
}

func (repository *Repository) FailPermanently(ctx context.Context, id uuid.UUID, cause string) error {
	if err := repository.queries.FailEmailJobPermanently(ctx, emaildeliverydb.FailEmailJobPermanentlyParams{
		ID: id, LastError: &cause,
	}); err != nil {
		return fmt.Errorf("fail email job permanently: %w", err)
	}
	return nil
}

func (repository *Repository) Delivery(ctx context.Context, id uuid.UUID) (models.Delivery, error) {
	row, err := repository.queries.GetEmailDelivery(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Delivery{}, err
		}
		return models.Delivery{}, fmt.Errorf("load email delivery: %w", err)
	}
	return models.Delivery{
		ID:         row.ID,
		Template:   row.Template,
		Nonce:      row.PayloadNonce,
		Ciphertext: row.EncryptedPayload,
		ExpiresAt:  row.ExpiresAt.Time,
		Status:     row.Status,
	}, nil
}

func (repository *Repository) MarkDelivered(ctx context.Context, id uuid.UUID, providerID string) (int64, error) {
	var providerMessageID *string
	if providerID != "" {
		providerMessageID = &providerID
	}
	rows, err := repository.queries.MarkEmailDeliveryDelivered(ctx, emaildeliverydb.MarkEmailDeliveryDeliveredParams{
		ID: id, ProviderMessageID: providerMessageID,
	})
	if err != nil {
		return 0, fmt.Errorf("mark email delivery delivered: %w", err)
	}
	return rows, nil
}

func (repository *Repository) MarkTerminal(ctx context.Context, id uuid.UUID, status string, cause string) (int64, error) {
	rows, err := repository.queries.MarkEmailDeliveryTerminal(ctx, emaildeliverydb.MarkEmailDeliveryTerminalParams{
		ID: id, Status: status, LastError: &cause,
	})
	if err != nil {
		return 0, fmt.Errorf("mark email delivery terminal: %w", err)
	}
	return rows, nil
}

func (repository *Repository) NoteError(ctx context.Context, id uuid.UUID, cause string) error {
	if err := repository.queries.NoteEmailDeliveryError(ctx, emaildeliverydb.NoteEmailDeliveryErrorParams{
		ID: id, LastError: &cause,
	}); err != nil {
		return fmt.Errorf("note email delivery error: %w", err)
	}
	return nil
}
