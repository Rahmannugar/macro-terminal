package repositories

import (
	"context"
	"fmt"
	"time"

	retentiondb "github.com/Rahmannugar/macro-terminal/server/internal/retention/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool    *pgxpool.Pool
	queries *retentiondb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, queries: retentiondb.New(pool)}
}

// ListExpiredArticleIDs returns the oldest articles published before the cutoff.
func (repository *Repository) ListExpiredArticleIDs(ctx context.Context, cutoff time.Time, limit int32) ([]uuid.UUID, error) {
	ids, err := repository.queries.ListExpiredArticleIDs(ctx, retentiondb.ListExpiredArticleIDsParams{
		PublishedAt: pgtype.Timestamptz{Time: cutoff, Valid: true},
		Limit:       limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list expired article ids: %w", err)
	}
	return ids, nil
}

// DeleteArticles removes one batch of expired articles and every row that references them
// in a single transaction, so a crash mid-batch leaves the batch fully intact for the next pass.
func (repository *Repository) DeleteArticles(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin article deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := repository.queries.WithTx(tx)
	if err := queries.DeleteWebhookDeliveriesForArticleNotifications(ctx, ids); err != nil {
		return fmt.Errorf("delete webhook deliveries for expired articles: %w", err)
	}
	if err := queries.DeleteArticleNotifications(ctx, ids); err != nil {
		return fmt.Errorf("delete notifications for expired articles: %w", err)
	}
	if err := queries.DeleteArticleOutboxRows(ctx, ids); err != nil {
		return fmt.Errorf("delete outbox rows for expired articles: %w", err)
	}
	if err := queries.DeleteUnmappedArticlesByIDs(ctx, ids); err != nil {
		return fmt.Errorf("delete unmapped rows for expired articles: %w", err)
	}
	if err := queries.DeleteArticleEntitiesByIDs(ctx, ids); err != nil {
		return fmt.Errorf("delete entity links for expired articles: %w", err)
	}
	if err := queries.DeleteArticleEnrichmentsByIDs(ctx, ids); err != nil {
		return fmt.Errorf("delete enrichments for expired articles: %w", err)
	}
	if err := queries.DeleteArticleStoryClusterMembershipsByIDs(ctx, ids); err != nil {
		return fmt.Errorf("delete story cluster memberships for expired articles: %w", err)
	}
	if err := queries.DeleteArticlesByIDs(ctx, ids); err != nil {
		return fmt.Errorf("delete expired articles: %w", err)
	}
	if err := queries.DeleteEmptyStoryClusters(ctx); err != nil {
		return fmt.Errorf("delete empty story clusters: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit article deletion: %w", err)
	}
	return nil
}

// PurgeDoneOutboxRows removes completed rows older than the cutoff and reports how many went.
func (repository *Repository) PurgeDoneOutboxRows(ctx context.Context, before time.Time) (int64, error) {
	purged, err := repository.queries.PurgeDoneOutboxRows(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("purge done outbox rows: %w", err)
	}
	return purged, nil
}

// PurgeFailedOutboxRows removes permanently failed rows older than the cutoff and reports how many went.
func (repository *Repository) PurgeFailedOutboxRows(ctx context.Context, before time.Time) (int64, error) {
	purged, err := repository.queries.PurgeFailedOutboxRows(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("purge failed outbox rows: %w", err)
	}
	return purged, nil
}

// PurgeReadNotifications removes read notifications older than the cutoff and reports how many went.
func (repository *Repository) PurgeReadNotifications(ctx context.Context, before time.Time) (int64, error) {
	purged, err := repository.queries.PurgeReadNotifications(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("purge read notifications: %w", err)
	}
	return purged, nil
}
