package repositories

import (
	"context"
	"fmt"

	"github.com/Rahmannugar/macro-terminal/server/internal/hydration/models"
	hydrationdb "github.com/Rahmannugar/macro-terminal/server/internal/hydration/repositories/generated"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool    *pgxpool.Pool
	queries *hydrationdb.Queries
	store   *cache.JSONStore
}

func NewRepository(pool *pgxpool.Pool, store *cache.JSONStore) *Repository {
	return &Repository{pool: pool, queries: hydrationdb.New(pool), store: store}
}

func (repository *Repository) EnqueueMissingContentJobs(ctx context.Context, limit int32) (int64, error) {
	queued, err := repository.queries.EnqueueMissingContentJobs(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("enqueue missing content jobs: %w", err)
	}
	return queued, nil
}

func (repository *Repository) ReclaimStaleContentJobs(ctx context.Context) (int64, error) {
	reclaimed, err := repository.queries.ReclaimStaleContentJobs(ctx)
	if err != nil {
		return 0, fmt.Errorf("reclaim stale content jobs: %w", err)
	}
	return reclaimed, nil
}

func (repository *Repository) ClaimBatch(ctx context.Context, limit int32) ([]models.ClaimedJob, error) {
	rows, err := repository.queries.ClaimContentBatch(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("claim content batch: %w", err)
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

func (repository *Repository) Complete(ctx context.Context, id uuid.UUID) error {
	if err := repository.queries.CompleteContentJob(ctx, id); err != nil {
		return fmt.Errorf("complete content job: %w", err)
	}
	return nil
}

func (repository *Repository) Fail(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string) error {
	err := repository.queries.FailContentJob(ctx, hydrationdb.FailContentJobParams{
		ID:        id,
		Attempts:  maxAttempts,
		LastError: &cause,
	})
	if err != nil {
		return fmt.Errorf("fail content job: %w", err)
	}
	return nil
}

func (repository *Repository) FailPermanently(ctx context.Context, id uuid.UUID, cause string) error {
	err := repository.queries.FailContentJobPermanently(ctx, hydrationdb.FailContentJobPermanentlyParams{
		ID:        id,
		LastError: &cause,
	})
	if err != nil {
		return fmt.Errorf("fail content job permanently: %w", err)
	}
	return nil
}

func (repository *Repository) ContentTarget(ctx context.Context, id uuid.UUID) (models.ContentTarget, error) {
	row, err := repository.queries.GetContentTarget(ctx, id)
	if err != nil {
		return models.ContentTarget{}, fmt.Errorf("get content target: %w", err)
	}
	return models.ContentTarget{
		ArticleID:         row.ArticleID,
		ArticleURL:        row.ArticleUrl,
		SourceID:          row.SourceID,
		ConfigurationID:   row.ConfigurationID,
		ConfigurationType: row.ConfigurationType,
		Config:            row.Config,
		SourceName:        row.SourceName,
		SourceType:        row.SourceType,
	}, nil
}

func (repository *Repository) StoreContent(ctx context.Context, id uuid.UUID, content string) (int64, error) {
	stored, err := repository.queries.StoreArticleContent(ctx, hydrationdb.StoreArticleContentParams{
		ID:      id,
		Content: &content,
	})
	if err != nil {
		return 0, fmt.Errorf("store article content: %w", err)
	}
	if stored > 0 {
		_ = repository.store.Delete(ctx, cache.ArticleKey(id), cache.ExplainArticleKey(id))
	}
	return stored, nil
}
