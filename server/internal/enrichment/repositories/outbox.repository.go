package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Rahmannugar/macro-terminal/server/internal/enrichment/models"
	enrichmentdb "github.com/Rahmannugar/macro-terminal/server/internal/enrichment/repositories/generated"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OutboxRepository struct {
	pool    *pgxpool.Pool
	queries *enrichmentdb.Queries
	store   *cache.JSONStore
}

func NewOutboxRepository(pool *pgxpool.Pool, store *cache.JSONStore) *OutboxRepository {
	return &OutboxRepository{pool: pool, queries: enrichmentdb.New(pool), store: store}
}

func (repository *OutboxRepository) EnqueueMissingArticles(ctx context.Context, limit int32) (int64, error) {
	queued, err := repository.queries.EnqueueMissingArticles(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("enqueue missing articles: %w", err)
	}
	return queued, nil
}

func (repository *OutboxRepository) ReclaimStaleEnrichmentJobs(ctx context.Context) (int64, error) {
	reclaimed, err := repository.queries.ReclaimStaleEnrichmentJobs(ctx)
	if err != nil {
		return 0, fmt.Errorf("reclaim stale enrichment jobs: %w", err)
	}
	return reclaimed, nil
}

func (repository *OutboxRepository) ClaimBatch(ctx context.Context, limit int32) ([]models.ClaimedJob, error) {
	rows, err := repository.queries.ClaimEnrichmentBatch(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("claim enrichment batch: %w", err)
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
		return fmt.Errorf("complete enrichment job: %w", err)
	}
	return nil
}

func (repository *OutboxRepository) Fail(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string) error {
	err := repository.queries.FailOutboxJob(ctx, enrichmentdb.FailOutboxJobParams{
		ID:        id,
		Attempts:  maxAttempts,
		LastError: &cause,
	})
	if err != nil {
		return fmt.Errorf("fail enrichment job: %w", err)
	}
	return nil
}

func (repository *OutboxRepository) FailPermanently(ctx context.Context, id uuid.UUID, cause string) error {
	err := repository.queries.FailOutboxJobPermanently(ctx, enrichmentdb.FailOutboxJobPermanentlyParams{
		ID:        id,
		LastError: &cause,
	})
	if err != nil {
		return fmt.Errorf("fail enrichment job permanently: %w", err)
	}
	return nil
}

func (repository *OutboxRepository) Article(ctx context.Context, id uuid.UUID) (models.Article, error) {
	row, err := repository.queries.GetArticleForEnrichment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Article{}, fmt.Errorf("article %s: %w", id, pgx.ErrNoRows)
	}
	if err != nil {
		return models.Article{}, fmt.Errorf("get article for enrichment: %w", err)
	}
	return models.Article{ID: row.ID, Title: row.Title, Content: row.Content}, nil
}

func (repository *OutboxRepository) ArticleEnrichment(ctx context.Context, articleID uuid.UUID) (models.StoredEnrichment, bool, error) {
	row, err := repository.queries.GetArticleEnrichment(ctx, articleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.StoredEnrichment{}, false, nil
	}
	if err != nil {
		return models.StoredEnrichment{}, false, fmt.Errorf("get article enrichment: %w", err)
	}
	return models.StoredEnrichment{
		ID:        row.ID,
		ArticleID: row.ArticleID,
		Model:     row.Model,
		Result:    row.Result,
	}, true, nil
}

func (repository *OutboxRepository) StoreEnrichment(
	ctx context.Context,
	articleID uuid.UUID,
	model string,
	result json.RawMessage,
) (bool, error) {
	id := uuid.New()
	rows, err := repository.queries.StoreArticleEnrichment(ctx, enrichmentdb.StoreArticleEnrichmentParams{
		ID:        id,
		ArticleID: articleID,
		Model:     model,
		Result:    result,
	})
	if err != nil {
		return false, fmt.Errorf("store article enrichment: %w", err)
	}
	if rows > 0 {
		_ = repository.store.Set(ctx, cache.ArticleEnrichmentKey(articleID), models.StoredEnrichment{
			ID:        id,
			ArticleID: articleID,
			Model:     model,
			Result:    result,
		})
	}
	return rows > 0, nil
}
