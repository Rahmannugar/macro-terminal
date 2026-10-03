package repositories

import (
	"context"
	"fmt"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	articledb "github.com/Rahmannugar/macro-terminal/server/internal/articles/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ArticleRepository struct {
	pool    *pgxpool.Pool
	queries *articledb.Queries
}

func NewArticleRepository(pool *pgxpool.Pool) *ArticleRepository {
	return &ArticleRepository{pool: pool, queries: articledb.New(pool)}
}

// PersistArticles stores one configuration's candidates and their mapping
// outcomes in a single transaction, so a partial pass cannot leave an
// article without its links or queue row. Re-running the same batch
// changes nothing: articles upsert by source and URL, links and queue
// rows conflict on their own keys, and a candidate that maps after having
// been queued clears its still-pending queue row.
func (repository *ArticleRepository) PersistArticles(
	ctx context.Context,
	entries []models.PersistEntry,
) (models.PersistStats, error) {
	stats := models.PersistStats{}
	if len(entries) == 0 {
		return stats, nil
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return stats, fmt.Errorf("begin persist transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repository.queries.WithTx(tx)

	for _, entry := range entries {
		article, err := queries.UpsertArticle(ctx, articledb.UpsertArticleParams{
			ID:          uuid.New(),
			SourceID:    entry.SourceID,
			Title:       entry.Title,
			Content:     contentPointer(entry.Content),
			Url:         entry.URL,
			PublishedAt: timePointer(entry.PublishedAt),
		})
		if err != nil {
			return stats, fmt.Errorf("upsert article: %w", err)
		}

		switch {
		case len(entry.EntityIDs) > 0:
			for _, entityID := range entry.EntityIDs {
				if err := queries.UpsertArticleEntity(ctx, articledb.UpsertArticleEntityParams{
					ArticleID: article.ID,
					EntityID:  entityID,
				}); err != nil {
					return stats, fmt.Errorf("upsert article entity: %w", err)
				}
			}
			resolved, err := queries.ResolveUnmappedArticle(ctx, article.ID)
			if err != nil {
				return stats, fmt.Errorf("resolve unmapped article: %w", err)
			}
			stats.Resolved += int(resolved)
		case entry.QueueUnmapped:
			queued, err := queries.UpsertUnmappedArticle(ctx, articledb.UpsertUnmappedArticleParams{
				ID:        uuid.New(),
				ArticleID: article.ID,
			})
			if err != nil {
				return stats, fmt.Errorf("queue unmapped article: %w", err)
			}
			stats.UnmappedQueued += int(queued)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return stats, fmt.Errorf("commit persist transaction: %w", err)
	}
	return stats, nil
}

func (repository *ArticleRepository) GetArticlesByIDs(ctx context.Context, ids []uuid.UUID) ([]models.StoredArticle, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := repository.queries.GetArticlesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get articles by ids: %w", err)
	}
	articles := make([]models.StoredArticle, 0, len(rows))
	for _, row := range rows {
		var publishedAt *time.Time
		if row.PublishedAt.Valid {
			publishedAt = &row.PublishedAt.Time
		}
		var content string
		if row.Content != nil {
			content = *row.Content
		}
		articles = append(articles, models.StoredArticle{
			ID:          row.ID,
			SourceID:    row.SourceID,
			SourceName:  row.SourceName,
			Title:       row.Title,
			Content:     content,
			URL:         row.Url,
			PublishedAt: publishedAt,
		})
	}
	return articles, nil
}

// contentPointer stores an empty snippet as NULL rather than an empty
// string; both read back the same, NULL keeps the column honest.
func contentPointer(content string) *string {
	if content == "" {
		return nil
	}
	return &content
}

func timePointer(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}
