package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	articledb "github.com/Rahmannugar/macro-terminal/server/internal/articles/repositories/generated"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ArticleRepository struct {
	pool    *pgxpool.Pool
	queries *articledb.Queries
	store   *cache.JSONStore
}

func NewArticleRepository(pool *pgxpool.Pool, store *cache.JSONStore) *ArticleRepository {
	return &ArticleRepository{pool: pool, queries: articledb.New(pool), store: store}
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

	storedIDs := make([]uuid.UUID, 0, len(entries))
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
		storedIDs = append(storedIDs, article.ID)

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
	repository.populateCache(ctx, storedIDs)
	return stats, nil
}

// populateCache refreshes cached copies only after the transaction has
// committed, so a rolled-back write can never leave Redis holding an
// article PostgreSQL does not have.
func (repository *ArticleRepository) populateCache(ctx context.Context, ids []uuid.UUID) {
	if repository.store == nil || len(ids) == 0 {
		return
	}
	rows, err := repository.queries.GetArticlesByIDs(ctx, ids)
	if err != nil {
		return
	}
	for _, row := range rows {
		_ = repository.store.Set(ctx, cache.ArticleKey(row.ID), storedArticleFromRow(row))
	}
}

func (repository *ArticleRepository) GetArticlesByIDs(ctx context.Context, ids []uuid.UUID) ([]models.StoredArticle, error) {
	unique := uniqueIDs(ids)
	if len(unique) == 0 {
		return nil, nil
	}
	keys := make([]string, len(unique))
	for index, id := range unique {
		keys[index] = cache.ArticleKey(id)
	}
	cached, err := repository.store.GetMany(ctx, keys)
	if err != nil {
		cached = nil
	}
	articles := make(map[uuid.UUID]models.StoredArticle, len(unique))
	missing := make([]uuid.UUID, 0, len(unique))
	for index, id := range unique {
		raw, ok := cached[keys[index]]
		if ok {
			var stored models.StoredArticle
			if err := json.Unmarshal(raw, &stored); err == nil {
				articles[id] = stored
				continue
			}
		}
		missing = append(missing, id)
	}
	if len(missing) > 0 {
		rows, err := repository.queries.GetArticlesByIDs(ctx, missing)
		if err != nil {
			return nil, fmt.Errorf("get articles by ids: %w", err)
		}
		for _, row := range rows {
			stored := storedArticleFromRow(row)
			articles[stored.ID] = stored
			_ = repository.store.Set(ctx, cache.ArticleKey(stored.ID), stored)
		}
	}
	ordered := make([]models.StoredArticle, 0, len(unique))
	for _, id := range unique {
		if stored, ok := articles[id]; ok {
			ordered = append(ordered, stored)
		}
	}
	return ordered, nil
}

// RecentArticlesByEntities returns the newest articles linked to any of
// the entities, hydrating through the cache-backed lookup so stored rows
// stay authoritative.
func (repository *ArticleRepository) RecentArticlesByEntities(
	ctx context.Context,
	entityIDs []uuid.UUID,
	limit int,
) ([]models.StoredArticle, error) {
	if len(entityIDs) == 0 || limit <= 0 {
		return nil, nil
	}
	rows, err := repository.queries.GetRecentArticleIDsByEntities(ctx, articledb.GetRecentArticleIDsByEntitiesParams{
		Limit: int32(limit),
		Ids:   entityIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("get recent article ids by entities: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return repository.GetArticlesByIDs(ctx, ids)
}

func storedArticleFromRow(row articledb.GetArticlesByIDsRow) models.StoredArticle {
	var publishedAt *time.Time
	if row.PublishedAt.Valid {
		publishedAt = &row.PublishedAt.Time
	}
	var content string
	if row.Content != nil {
		content = *row.Content
	}
	return models.StoredArticle{
		ID:          row.ID,
		SourceID:    row.SourceID,
		SourceName:  row.SourceName,
		Title:       row.Title,
		Content:     content,
		URL:         row.Url,
		PublishedAt: publishedAt,
	}
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	unique := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
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

func (repository *ArticleRepository) RecentArticleIDsPage(
	ctx context.Context,
	entityIDs []uuid.UUID,
	cursor *paging.Cursor,
	limit int32,
) ([]models.RecentArticleRef, *paging.Cursor, error) {
	type sortRow struct {
		ID     uuid.UUID
		SortAt pgtype.Timestamptz
	}

	var rows []sortRow
	if len(entityIDs) == 0 {
		params := articledb.GetRecentArticleIDsPageParams{PageSize: limit + 1}
		if cursor != nil {
			params.CursorAt = pgtype.Timestamptz{Time: cursor.At, Valid: true}
			params.CursorID = pgtype.UUID{Bytes: cursor.ID, Valid: true}
		}
		generated, err := repository.queries.GetRecentArticleIDsPage(ctx, params)
		if err != nil {
			return nil, nil, fmt.Errorf("get recent article ids page: %w", err)
		}
		rows = make([]sortRow, 0, len(generated))
		for _, row := range generated {
			rows = append(rows, sortRow{ID: row.ID, SortAt: row.SortAt})
		}
	} else {
		params := articledb.GetRecentArticleIDsByEntitiesPageParams{Ids: entityIDs, PageSize: limit + 1}
		if cursor != nil {
			params.CursorAt = pgtype.Timestamptz{Time: cursor.At, Valid: true}
			params.CursorID = pgtype.UUID{Bytes: cursor.ID, Valid: true}
		}
		generated, err := repository.queries.GetRecentArticleIDsByEntitiesPage(ctx, params)
		if err != nil {
			return nil, nil, fmt.Errorf("get recent article ids by entities page: %w", err)
		}
		rows = make([]sortRow, 0, len(generated))
		for _, row := range generated {
			rows = append(rows, sortRow{ID: row.ID, SortAt: row.SortAt})
		}
	}

	refs := make([]models.RecentArticleRef, 0, len(rows))
	for _, row := range rows {
		if !row.SortAt.Valid {
			continue
		}
		refs = append(refs, models.RecentArticleRef{ID: row.ID, SortAt: row.SortAt.Time})
	}
	if int32(len(refs)) > limit {
		refs = refs[:limit]
		last := refs[len(refs)-1]
		return refs, &paging.Cursor{At: last.SortAt, ID: last.ID}, nil
	}
	return refs, nil, nil
}
