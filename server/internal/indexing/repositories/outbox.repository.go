package repositories

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/indexing/models"
	indexingdb "github.com/Rahmannugar/macro-terminal/server/internal/indexing/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OutboxRepository struct {
	pool    *pgxpool.Pool
	queries *indexingdb.Queries
}

func NewOutboxRepository(pool *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{pool: pool, queries: indexingdb.New(pool)}
}

func (repository *OutboxRepository) EnqueueMissingIndexJobs(ctx context.Context, limit int32) (int64, error) {
	queued, err := repository.queries.EnqueueMissingIndexJobs(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("enqueue missing index jobs: %w", err)
	}
	return queued, nil
}

func (repository *OutboxRepository) ReclaimStaleIndexJobs(ctx context.Context) (int64, error) {
	reclaimed, err := repository.queries.ReclaimStaleIndexJobs(ctx)
	if err != nil {
		return 0, fmt.Errorf("reclaim stale index jobs: %w", err)
	}
	return reclaimed, nil
}

func (repository *OutboxRepository) ClaimBatch(ctx context.Context, limit int32) ([]models.ClaimedJob, error) {
	rows, err := repository.queries.ClaimIndexBatch(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("claim index batch: %w", err)
	}
	claimed := make([]models.ClaimedJob, 0, len(rows))
	for _, row := range rows {
		claimed = append(claimed, models.ClaimedJob{
			ID:        row.ID,
			ArticleID: row.ArticleID,
			Attempts:  row.Attempts,
		})
	}
	return claimed, nil
}

func (repository *OutboxRepository) Complete(ctx context.Context, id uuid.UUID) error {
	if err := repository.queries.CompleteOutboxJob(ctx, id); err != nil {
		return fmt.Errorf("complete index job: %w", err)
	}
	return nil
}

func (repository *OutboxRepository) Fail(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string) error {
	err := repository.queries.FailOutboxJob(ctx, indexingdb.FailOutboxJobParams{
		ID:        id,
		Attempts:  maxAttempts,
		LastError: &cause,
	})
	if err != nil {
		return fmt.Errorf("fail index job: %w", err)
	}
	return nil
}

func (repository *OutboxRepository) FailPermanently(ctx context.Context, id uuid.UUID, cause string) error {
	err := repository.queries.FailOutboxJobPermanently(ctx, indexingdb.FailOutboxJobPermanentlyParams{
		ID:        id,
		LastError: &cause,
	})
	if err != nil {
		return fmt.Errorf("fail index job permanently: %w", err)
	}
	return nil
}

func (repository *OutboxRepository) Article(ctx context.Context, id uuid.UUID) (models.Article, error) {
	row, err := repository.queries.GetArticleForIndexing(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Article{}, fmt.Errorf("article %s: %w", id, pgx.ErrNoRows)
	}
	if err != nil {
		return models.Article{}, fmt.Errorf("get article for indexing: %w", err)
	}
	return models.Article{
		ID:          row.ID,
		Title:       row.Title,
		Content:     row.Content,
		PublishedAt: timeValue(row.PublishedAt),
	}, nil
}

func (repository *OutboxRepository) ReopenIndexJobs(ctx context.Context) (int64, error) {
	reopened, err := repository.queries.ReopenIndexJobs(ctx)
	if err != nil {
		return 0, fmt.Errorf("reopen index jobs: %w", err)
	}
	return reopened, nil
}

func timeValue(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
