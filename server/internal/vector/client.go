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
}

func NewIndexer(addr string) (Indexer, error) {
	return newGrpcClient(addr)
}

func NewSearcher(addr string) (Searcher, error) {
	return newGrpcClient(addr)
}
