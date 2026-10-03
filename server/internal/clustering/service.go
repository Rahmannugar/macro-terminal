package clustering

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/vector"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	DefaultPageSize = 25
	MaximumPageSize = 100
)

var (
	ErrNotFound    = errors.New("article not found")
	ErrUnavailable = errors.New("related article lookup is unavailable")
)

type ClusterReader interface {
	ClusterOfArticle(ctx context.Context, articleID uuid.UUID) (uuid.UUID, error)
	MembersOfCluster(ctx context.Context, clusterID uuid.UUID) ([]uuid.UUID, error)
}

type ArticleHydrator interface {
	GetArticlesByIDs(ctx context.Context, ids []uuid.UUID) ([]models.StoredArticle, error)
}

type VectorSearcher interface {
	FindSimilarArticles(ctx context.Context, article vector.Article, limit int) ([]vector.Neighbor, error)
}

type Service struct {
	clusters ClusterReader
	searcher VectorSearcher
	hydrator ArticleHydrator
}

func NewService(clusters ClusterReader, searcher VectorSearcher, hydrator ArticleHydrator) *Service {
	return &Service{clusters: clusters, searcher: searcher, hydrator: hydrator}
}

// Related lists cluster mates first (most recent first), then semantic neighbours; duplicate and self ids are dropped.
func (service *Service) Related(
	ctx context.Context,
	articleID uuid.UUID,
	limit int,
) ([]models.StoredArticle, error) {
	limit = clampLimit(limit)

	selfs, err := service.hydrator.GetArticlesByIDs(ctx, []uuid.UUID{articleID})
	if err != nil {
		return nil, fmt.Errorf("hydrate related article: %w", err)
	}
	if len(selfs) == 0 {
		return nil, ErrNotFound
	}
	self := selfs[0]

	memberIDs, err := service.memberIDs(ctx, articleID)
	if err != nil {
		return nil, err
	}
	neighborIDs, err := service.neighborIDs(ctx, self, len(memberIDs), limit)
	if err != nil {
		return nil, err
	}

	candidates := make([]uuid.UUID, 0, len(memberIDs)+len(neighborIDs))
	candidates = append(candidates, memberIDs...)
	candidates = append(candidates, neighborIDs...)
	hydrated, err := service.hydrator.GetArticlesByIDs(ctx, candidates)
	if err != nil {
		return nil, fmt.Errorf("hydrate related articles: %w", err)
	}
	byID := make(map[uuid.UUID]models.StoredArticle, len(hydrated))
	for _, article := range hydrated {
		byID[article.ID] = article
	}

	related := make([]models.StoredArticle, 0, limit)
	included := map[uuid.UUID]struct{}{articleID: {}}

	members := make([]models.StoredArticle, 0, len(memberIDs))
	for _, id := range memberIDs {
		if article, ok := byID[id]; ok {
			members = append(members, article)
		}
	}
	sortByRecency(members)
	for _, article := range members {
		if len(related) == limit {
			break
		}
		related = append(related, article)
		included[article.ID] = struct{}{}
	}
	for _, id := range neighborIDs {
		if len(related) == limit {
			break
		}
		if _, ok := included[id]; ok {
			continue
		}
		article, ok := byID[id]
		if !ok {
			continue
		}
		related = append(related, article)
		included[id] = struct{}{}
	}
	return related, nil
}

func (service *Service) memberIDs(ctx context.Context, articleID uuid.UUID) ([]uuid.UUID, error) {
	clusterID, err := service.clusters.ClusterOfArticle(ctx, articleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load story cluster: %w", err)
	}
	members, err := service.clusters.MembersOfCluster(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("load story cluster members: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(members))
	for _, id := range members {
		if id != articleID {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (service *Service) neighborIDs(
	ctx context.Context,
	self models.StoredArticle,
	memberCount int,
	limit int,
) ([]uuid.UUID, error) {
	if service.searcher == nil {
		return nil, nil
	}
	neighbors, err := service.searcher.FindSimilarArticles(ctx, vectorArticle(self), limit+memberCount+1)
	if err != nil {
		return nil, fmt.Errorf("%w: find related vectors: %w", ErrUnavailable, err)
	}
	ids := make([]uuid.UUID, 0, len(neighbors))
	for _, neighbor := range neighbors {
		if neighbor.ID == self.ID || neighbor.Score < minSimilarity {
			continue
		}
		ids = append(ids, neighbor.ID)
	}
	return ids, nil
}

func vectorArticle(article models.StoredArticle) vector.Article {
	vectorized := vector.Article{
		ID:      article.ID,
		Title:   article.Title,
		Content: article.Content,
	}
	if article.PublishedAt != nil {
		vectorized.PublishedAt = *article.PublishedAt
	}
	return vectorized
}

func sortByRecency(articles []models.StoredArticle) {
	sort.SliceStable(articles, func(i, j int) bool {
		left, right := articles[i].PublishedAt, articles[j].PublishedAt
		switch {
		case left == nil:
			return false
		case right == nil:
			return true
		default:
			return left.After(*right)
		}
	})
}

func clampLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultPageSize
	case limit > MaximumPageSize:
		return MaximumPageSize
	default:
		return limit
	}
}
