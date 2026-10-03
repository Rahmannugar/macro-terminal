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

func NewIndexer(addr string) (Indexer, error) {
	return newGrpcClient(addr)
}

func NewSearcher(addr string) (Searcher, error) {
	return newGrpcClient(addr)
}

// NewClient dials once for callers that need both indexing and search.
func NewClient(addr string) (Client, error) {
	return newGrpcClient(addr)
}
