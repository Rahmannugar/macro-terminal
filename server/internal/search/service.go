package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	"github.com/google/uuid"
)

const (
	DefaultPageSize    = 25
	MaximumPageSize    = 100
	MaximumQueryLength = 500
	// maximumWindow bounds cursor depth: every page asks the index for offset+limit+1 neighbours.
	maximumWindow = 200
)

var (
	ErrQueryInvalid    = errors.New("search query is invalid")
	ErrUnavailable     = errors.New("semantic search is unavailable")
	ErrWindowExhausted = errors.New("search result window is exhausted")
)

type VectorSearcher interface {
	SearchArticles(ctx context.Context, query string, limit int) ([]uuid.UUID, error)
}

type ArticleHydrator interface {
	GetArticlesByIDs(ctx context.Context, ids []uuid.UUID) ([]models.StoredArticle, error)
}

type Service struct {
	searcher VectorSearcher
	hydrator ArticleHydrator
}

func NewService(searcher VectorSearcher, hydrator ArticleHydrator) *Service {
	return &Service{searcher: searcher, hydrator: hydrator}
}

// Search resolves one page; missing hydrated rows are skipped without shifting the rank-based cursor.
func (service *Service) Search(
	ctx context.Context,
	text string,
	limit int,
	cursor *Cursor,
) ([]models.StoredArticle, *Cursor, error) {
	query := strings.TrimSpace(text)
	if query == "" || utf8.RuneCountInString(query) > MaximumQueryLength {
		return nil, nil, ErrQueryInvalid
	}
	limit = clampLimit(limit)

	offset := 0
	if cursor != nil {
		offset = cursor.Offset
	}
	if offset+limit > maximumWindow {
		return nil, nil, ErrWindowExhausted
	}
	if service.searcher == nil {
		return nil, nil, ErrUnavailable
	}

	ids, err := service.searcher.SearchArticles(ctx, query, offset+limit+1)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: search article vectors: %w", ErrUnavailable, err)
	}
	if offset >= len(ids) {
		return []models.StoredArticle{}, nil, nil
	}

	var next *Cursor
	if len(ids) > offset+limit {
		next = &Cursor{Offset: offset + limit}
	}
	pageEnd := offset + limit
	if pageEnd > len(ids) {
		pageEnd = len(ids)
	}

	hydrated, err := service.hydrator.GetArticlesByIDs(ctx, ids[offset:pageEnd])
	if err != nil {
		return nil, nil, fmt.Errorf("hydrate search results: %w", err)
	}
	byID := make(map[uuid.UUID]models.StoredArticle, len(hydrated))
	for _, article := range hydrated {
		byID[article.ID] = article
	}
	ordered := make([]models.StoredArticle, 0, len(hydrated))
	for _, id := range ids[offset:pageEnd] {
		if article, ok := byID[id]; ok {
			ordered = append(ordered, article)
		}
	}
	return ordered, next, nil
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
