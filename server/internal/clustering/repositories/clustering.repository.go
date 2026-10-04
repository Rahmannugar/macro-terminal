package repositories

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/clustering/models"
	clusteringdb "github.com/Rahmannugar/macro-terminal/server/internal/clustering/repositories/generated"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool    *pgxpool.Pool
	queries *clusteringdb.Queries
	store   *cache.JSONStore
}

func NewRepository(pool *pgxpool.Pool, store *cache.JSONStore) *Repository {
	return &Repository{pool: pool, queries: clusteringdb.New(pool), store: store}
}

func (repository *Repository) EnqueueMissingClusterJobs(ctx context.Context, limit int32) (int64, error) {
	queued, err := repository.queries.EnqueueMissingClusterJobs(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("enqueue missing cluster jobs: %w", err)
	}
	return queued, nil
}

func (repository *Repository) ReclaimStaleClusterJobs(ctx context.Context) (int64, error) {
	reclaimed, err := repository.queries.ReclaimStaleClusterJobs(ctx)
	if err != nil {
		return 0, fmt.Errorf("reclaim stale cluster jobs: %w", err)
	}
	return reclaimed, nil
}

func (repository *Repository) ClaimBatch(ctx context.Context, limit int32) ([]models.ClaimedJob, error) {
	rows, err := repository.queries.ClaimClusterBatch(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("claim cluster batch: %w", err)
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
	if err := repository.queries.CompleteOutboxJob(ctx, id); err != nil {
		return fmt.Errorf("complete cluster job: %w", err)
	}
	return nil
}

func (repository *Repository) Fail(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string) error {
	err := repository.queries.FailOutboxJob(ctx, clusteringdb.FailOutboxJobParams{
		ID:        id,
		Attempts:  maxAttempts,
		LastError: &cause,
	})
	if err != nil {
		return fmt.Errorf("fail cluster job: %w", err)
	}
	return nil
}

func (repository *Repository) FailPermanently(ctx context.Context, id uuid.UUID, cause string) error {
	err := repository.queries.FailOutboxJobPermanently(ctx, clusteringdb.FailOutboxJobPermanentlyParams{
		ID:        id,
		LastError: &cause,
	})
	if err != nil {
		return fmt.Errorf("fail cluster job permanently: %w", err)
	}
	return nil
}

func (repository *Repository) Article(ctx context.Context, id uuid.UUID) (models.Article, error) {
	row, err := repository.queries.GetArticleForClustering(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Article{}, fmt.Errorf("article %s: %w", id, pgx.ErrNoRows)
	}
	if err != nil {
		return models.Article{}, fmt.Errorf("get article for clustering: %w", err)
	}
	return models.Article{
		ID:          row.ID,
		Title:       row.Title,
		Content:     row.Content,
		PublishedAt: timeValue(row.PublishedAt),
	}, nil
}

func (repository *Repository) ClusterOfArticle(ctx context.Context, articleID uuid.UUID) (uuid.UUID, error) {
	clusterID, err := repository.queries.ClusterOfArticle(ctx, articleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("clusters of article %s: %w", articleID, pgx.ErrNoRows)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("get cluster of article: %w", err)
	}
	return clusterID, nil
}

func (repository *Repository) ClusterIDsForArticles(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := repository.queries.ClusterIDsForArticles(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get cluster ids for articles: %w", err)
	}
	memberships := make(map[uuid.UUID]uuid.UUID, len(rows))
	for _, row := range rows {
		memberships[row.ArticleID] = row.StoryClusterID
	}
	return memberships, nil
}

func (repository *Repository) CreateStoryCluster(ctx context.Context, id uuid.UUID, title string) error {
	if _, err := repository.queries.CreateStoryCluster(ctx, clusteringdb.CreateStoryClusterParams{
		ID:    id,
		Title: title,
	}); err != nil {
		return fmt.Errorf("create story cluster: %w", err)
	}
	_ = repository.store.Set(ctx, cache.StoryClusterKey(id), models.StoryCluster{ID: id, Title: title})
	return nil
}

func (repository *Repository) LinkArticleToCluster(ctx context.Context, articleID, clusterID uuid.UUID) (int64, error) {
	linked, err := repository.queries.LinkArticleToCluster(ctx, clusteringdb.LinkArticleToClusterParams{
		ArticleID:      articleID,
		StoryClusterID: clusterID,
	})
	if err != nil {
		return 0, fmt.Errorf("link article to cluster: %w", err)
	}
	return linked, nil
}

func (repository *Repository) StoryClusterForArticle(ctx context.Context, articleID uuid.UUID) (models.StoryCluster, bool, error) {
	row, err := repository.queries.GetStoryClusterForArticle(ctx, articleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.StoryCluster{}, false, nil
	}
	if err != nil {
		return models.StoryCluster{}, false, fmt.Errorf("get story cluster for article: %w", err)
	}
	return models.StoryCluster{ID: row.ID, Title: row.Title}, true, nil
}

func (repository *Repository) MembersOfClusterForArticle(ctx context.Context, articleID uuid.UUID) ([]uuid.UUID, error) {
	members, err := repository.queries.MembersOfClusterForArticle(ctx, articleID)
	if err != nil {
		return nil, fmt.Errorf("get members of article's cluster: %w", err)
	}
	return members, nil
}

func (repository *Repository) TouchStoryCluster(ctx context.Context, clusterID uuid.UUID) error {
	if err := repository.queries.TouchStoryCluster(ctx, clusterID); err != nil {
		return fmt.Errorf("touch story cluster: %w", err)
	}
	return nil
}

func (repository *Repository) ArticleTitles(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := repository.queries.GetArticleTitles(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get article titles: %w", err)
	}
	titles := make(map[uuid.UUID]string, len(rows))
	for _, row := range rows {
		titles[row.ID] = row.Title
	}
	return titles, nil
}

func timeValue(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
