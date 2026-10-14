package vector

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Article struct {
	ID          uuid.UUID
	PublishedAt time.Time
	Title       string
	Content     string
}

type Indexer interface {
	StoreArticle(ctx context.Context, article Article) error
	// DeleteArticles removes each article's vector by its predicate; missing articles are a no-op.
	DeleteArticles(ctx context.Context, ids []uuid.UUID) error
}

// Searcher returns article ids ordered closest-first for the query text; callers hydrate authoritative rows themselves.
type Searcher interface {
	SearchArticles(ctx context.Context, query string, limit int) ([]uuid.UUID, error)
	FindSimilarArticles(ctx context.Context, article Article, limit int) ([]Neighbor, error)
}

// Neighbor is a vector-index hit with the score and stored publish date, so callers can filter without re-querying metadata.
type Neighbor struct {
	ID          uuid.UUID
	PublishedAt *time.Time
	Score       float32
}

type Client interface {
	Indexer
	Searcher
}
