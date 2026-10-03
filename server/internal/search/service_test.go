package search

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	"github.com/google/uuid"
)

type fakeSearcher struct {
	ids     []uuid.UUID
	err     error
	limit   int
	queries []string
}

func (searcher *fakeSearcher) SearchArticles(_ context.Context, query string, limit int) ([]uuid.UUID, error) {
	searcher.queries = append(searcher.queries, query)
	searcher.limit = limit
	return searcher.ids, searcher.err
}

type fakeHydrator struct {
	articles  map[uuid.UUID]models.StoredArticle
	err       error
	requested [][]uuid.UUID
}

func (hydrator *fakeHydrator) GetArticlesByIDs(_ context.Context, ids []uuid.UUID) ([]models.StoredArticle, error) {
	hydrator.requested = append(hydrator.requested, ids)
	if hydrator.err != nil {
		return nil, hydrator.err
	}
	articles := make([]models.StoredArticle, 0, len(ids))
	for _, id := range ids {
		if article, ok := hydrator.articles[id]; ok {
			articles = append(articles, article)
		}
	}
	return articles, nil
}

func rankedIDs(count int) []uuid.UUID {
	ids := make([]uuid.UUID, count)
	for index := range ids {
		ids[index] = uuid.UUID{byte(index + 1)}
	}
	return ids
}

func hydratorFor(ids []uuid.UUID) *fakeHydrator {
	articles := make(map[uuid.UUID]models.StoredArticle, len(ids))
	for _, id := range ids {
		articles[id] = models.StoredArticle{ID: id, Title: "title", Content: "content", URL: "https://example.com"}
	}
	return &fakeHydrator{articles: articles}
}

func TestSearchReturnsFirstPageInVectorOrder(t *testing.T) {
	ids := rankedIDs(30)
	searcher := &fakeSearcher{ids: ids}
	hydrator := hydratorFor(ids)

	articles, next, err := NewService(searcher, hydrator).Search(t.Context(), "  central bank  ", 0, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(articles) != DefaultPageSize {
		t.Fatalf("articles = %d, want %d", len(articles), DefaultPageSize)
	}
	for index, article := range articles {
		if article.ID != ids[index] {
			t.Fatalf("articles[%d].ID = %s, want %s (vector order)", index, article.ID, ids[index])
		}
	}
	if searcher.queries[0] != "central bank" {
		t.Errorf("query = %q, want trimmed text", searcher.queries[0])
	}
	if searcher.limit != DefaultPageSize+1 {
		t.Errorf("searcher limit = %d, want %d", searcher.limit, DefaultPageSize+1)
	}
	if next == nil || next.Offset != DefaultPageSize {
		t.Fatalf("next = %+v, want offset %d", next, DefaultPageSize)
	}
}

func TestSearchAdvancesCursorToSecondPage(t *testing.T) {
	ids := rankedIDs(30)
	searcher := &fakeSearcher{ids: ids}
	hydrator := hydratorFor(ids)

	articles, next, err := NewService(searcher, hydrator).Search(t.Context(), "rates", 25, &Cursor{Offset: 25})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if searcher.limit != 51 {
		t.Errorf("searcher limit = %d, want 51", searcher.limit)
	}
	if len(articles) != 5 {
		t.Fatalf("articles = %d, want 5", len(articles))
	}
	if articles[0].ID != ids[25] || articles[4].ID != ids[29] {
		t.Errorf("page spans %s..%s, want %s..%s", articles[0].ID, articles[4].ID, ids[25], ids[29])
	}
	if next != nil {
		t.Errorf("next = %+v, want nil at the end of the ranking", next)
	}
}

func TestSearchSkipsArticlesMissingFromTheDatabase(t *testing.T) {
	ids := rankedIDs(3)
	hydrator := hydratorFor(ids)
	delete(hydrator.articles, ids[1])
	searcher := &fakeSearcher{ids: ids}

	articles, next, err := NewService(searcher, hydrator).Search(t.Context(), "rates", 10, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(articles) != 2 || articles[0].ID != ids[0] || articles[1].ID != ids[2] {
		t.Fatalf("articles = %v, want ids[0] then ids[2]", articles)
	}
	if next != nil {
		t.Errorf("next = %+v, want nil", next)
	}
}

func TestSearchStopsWhenTheCursorIsPastTheRanking(t *testing.T) {
	searcher := &fakeSearcher{ids: rankedIDs(3)}
	hydrator := hydratorFor(searcher.ids)

	articles, next, err := NewService(searcher, hydrator).Search(t.Context(), "rates", 25, &Cursor{Offset: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(articles) != 0 || articles == nil {
		t.Fatalf("articles = %v, want an empty page", articles)
	}
	if next != nil {
		t.Errorf("next = %+v, want nil", next)
	}
	if len(hydrator.requested) != 0 {
		t.Errorf("hydrator called with %v, want no call for an empty page", hydrator.requested)
	}
}

func TestSearchRejectsInvalidQueries(t *testing.T) {
	service := NewService(&fakeSearcher{}, hydratorFor(nil))
	for _, text := range []string{"", "   ", string(make([]rune, MaximumQueryLength+1))} {
		if _, _, err := service.Search(t.Context(), text, 25, nil); !errors.Is(err, ErrQueryInvalid) {
			t.Errorf("search %q error = %v, want ErrQueryInvalid", text, err)
		}
	}
	if _, _, err := service.Search(t.Context(), string(make([]rune, MaximumQueryLength)), 25, nil); err != nil {
		t.Errorf("search at the length limit error = %v, want success", err)
	}
}

func TestSearchWithoutAConfiguredIndexerIsUnavailable(t *testing.T) {
	service := NewService(nil, hydratorFor(nil))
	if _, _, err := service.Search(t.Context(), "rates", 25, nil); !errors.Is(err, ErrUnavailable) {
		t.Errorf("search error = %v, want ErrUnavailable", err)
	}
}

func TestSearchReportsIndexerFailuresAsUnavailable(t *testing.T) {
	searcher := &fakeSearcher{err: errors.New("index unreachable")}
	service := NewService(searcher, hydratorFor(nil))
	if _, _, err := service.Search(t.Context(), "rates", 25, nil); !errors.Is(err, ErrUnavailable) {
		t.Errorf("search error = %v, want ErrUnavailable", err)
	}
}

func TestSearchPropagatesHydrationFailures(t *testing.T) {
	hydrator := &fakeHydrator{err: fmt.Errorf("database down")}
	service := NewService(&fakeSearcher{ids: rankedIDs(1)}, hydrator)
	_, _, err := service.Search(t.Context(), "rates", 25, nil)
	if err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("search error = %v, want a plain hydration error", err)
	}
}
