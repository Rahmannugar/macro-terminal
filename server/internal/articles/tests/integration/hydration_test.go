//go:build integration

package integration_test

import (
	"testing"
	"time"

	articlemodels "github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	articlerepositories "github.com/Rahmannugar/macro-terminal/server/internal/articles/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/google/uuid"
)

func TestGetArticlesByIDsHydratesWithSource(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID:   testID(t),
		Name: "Search Hydration Test Source",
		Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	articleRepository := articlerepositories.NewArticleRepository(pool, nil)
	published := time.Date(2026, 10, 2, 9, 15, 0, 0, time.UTC)
	if _, err := articleRepository.PersistArticles(t.Context(), []articlemodels.PersistEntry{
		{
			SourceID: source.ID, Title: "Fed holds rates", Content: "Rates unchanged.",
			URL: "https://example.com/hydrate-fed", PublishedAt: &published,
		},
		{
			SourceID: source.ID, Title: "Jobs beat forecasts", Content: "Payrolls rose.",
			URL: "https://example.com/hydrate-jobs",
		},
	}); err != nil {
		t.Fatalf("persist articles: %v", err)
	}
	stored := articleIDs(t, pool, source.ID)
	if len(stored) != 2 {
		t.Fatalf("stored articles = %d, want 2", len(stored))
	}

	articles, err := articleRepository.GetArticlesByIDs(t.Context(), []uuid.UUID{
		stored[1], stored[0], uuid.New(),
	})
	if err != nil {
		t.Fatalf("get articles by ids: %v", err)
	}
	if len(articles) != 2 {
		t.Fatalf("hydrated articles = %d, want 2 (the unknown id is dropped)", len(articles))
	}
	byID := make(map[uuid.UUID]articlemodels.StoredArticle, len(articles))
	for _, article := range articles {
		if article.SourceName != source.Name {
			t.Errorf("article %s source = %q, want %q", article.ID, article.SourceName, source.Name)
		}
		byID[article.ID] = article
	}
	withDate := byID[stored[0]]
	if withDate.PublishedAt == nil || !withDate.PublishedAt.Equal(published) {
		t.Errorf("published at = %v, want %v", withDate.PublishedAt, published)
	}
	if byID[stored[1]].PublishedAt != nil {
		t.Errorf("published at = %v, want nil for an article without a date", byID[stored[1]].PublishedAt)
	}

	empty, err := articleRepository.GetArticlesByIDs(t.Context(), nil)
	if err != nil {
		t.Fatalf("get articles by empty ids: %v", err)
	}
	if empty != nil {
		t.Fatalf("articles = %v, want nil for no ids", empty)
	}
}
