//go:build integration

package integration_test

import (
	"testing"
	"time"

	articlemodels "github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	articlerepositories "github.com/Rahmannugar/macro-terminal/server/internal/articles/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	entityrepositories "github.com/Rahmannugar/macro-terminal/server/internal/entities/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPersistArticlesIsIdempotent(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID:   testID(t),
		Name: "Article Persistence Test Source",
		Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	entityRepository := entityrepositories.NewEntityRepository(pool)
	dollar, err := entityRepository.UpsertEntity(t.Context(), entitymodels.Entity{
		ID: testID(t), Code: "USDP", Name: "United States Dollar (test)", Type: "currency",
	})
	if err != nil {
		t.Fatalf("create dollar entity: %v", err)
	}
	euro, err := entityRepository.UpsertEntity(t.Context(), entitymodels.Entity{
		ID: testID(t), Code: "EURP", Name: "Euro (test)", Type: "currency",
	})
	if err != nil {
		t.Fatalf("create euro entity: %v", err)
	}

	articleRepository := articlerepositories.NewArticleRepository(pool)
	published := time.Date(2026, 10, 1, 8, 30, 0, 0, time.UTC)
	mapped := articlemodels.PersistEntry{
		SourceID:    source.ID,
		Title:       "Fed holds rates steady",
		Content:     "The Federal Reserve kept rates unchanged.",
		URL:         "https://example.com/fed-holds",
		PublishedAt: &published,
		EntityIDs:   []uuid.UUID{dollar.ID, euro.ID},
	}
	unmapped := articlemodels.PersistEntry{
		SourceID:      source.ID,
		Title:         "Sunny weather reported",
		URL:           "https://example.com/weather",
		QueueUnmapped: true,
	}
	batch := []articlemodels.PersistEntry{mapped, unmapped}

	stats, err := articleRepository.PersistArticles(t.Context(), batch)
	if err != nil {
		t.Fatalf("persist batch: %v", err)
	}
	if stats.UnmappedQueued != 1 || stats.Resolved != 0 {
		t.Fatalf("first persist stats = %+v, want {UnmappedQueued:1 Resolved:0}", stats)
	}
	assertCounts(t, pool, 2, 2, 1)
	idsFirst := articleIDs(t, pool, source.ID)

	// Repeated delivery of the same source data changes nothing.
	stats, err = articleRepository.PersistArticles(t.Context(), batch)
	if err != nil {
		t.Fatalf("persist batch again: %v", err)
	}
	if stats.UnmappedQueued != 0 || stats.Resolved != 0 {
		t.Fatalf("repeat persist stats = %+v, want no changes", stats)
	}
	assertCounts(t, pool, 2, 2, 1)
	if idsSecond := articleIDs(t, pool, source.ID); !equalIDs(idsFirst, idsSecond) {
		t.Fatalf("article IDs changed between deliveries: %v -> %v", idsFirst, idsSecond)
	}

	// A refetch without a date must not erase the known publication date.
	refreshed := mapped
	refreshed.PublishedAt = nil
	refreshed.Title = "Fed holds rates steady (updated)"
	if _, err := articleRepository.PersistArticles(
		t.Context(), []articlemodels.PersistEntry{refreshed},
	); err != nil {
		t.Fatalf("persist refreshed article: %v", err)
	}
	var storedTitle string
	var storedPublished time.Time
	err = pool.QueryRow(t.Context(),
		`SELECT title, published_at FROM articles WHERE source_id = $1 AND url = $2`,
		source.ID, mapped.URL,
	).Scan(&storedTitle, &storedPublished)
	if err != nil {
		t.Fatalf("read stored article: %v", err)
	}
	if storedTitle != refreshed.Title {
		t.Fatalf("stored title = %q, want %q", storedTitle, refreshed.Title)
	}
	if !storedPublished.Equal(published) {
		t.Fatalf("stored published_at = %v, want %v (nil refetch must keep the date)", storedPublished, published)
	}

	// An article that maps after being queued leaves the queue.
	resolved := unmapped
	resolved.QueueUnmapped = false
	resolved.EntityIDs = []uuid.UUID{dollar.ID}
	stats, err = articleRepository.PersistArticles(t.Context(), []articlemodels.PersistEntry{resolved})
	if err != nil {
		t.Fatalf("persist resolved article: %v", err)
	}
	if stats.Resolved != 1 || stats.UnmappedQueued != 0 {
		t.Fatalf("resolve stats = %+v, want {UnmappedQueued:0 Resolved:1}", stats)
	}
	assertCounts(t, pool, 2, 3, 0)
}

func testID(t *testing.T) uuid.UUID {
	t.Helper()

	id, err := ids.New()
	if err != nil {
		t.Fatalf("generate ID: %v", err)
	}
	return id
}

func assertCounts(t *testing.T, pool *pgxpool.Pool, articles, links, unmapped int) {
	t.Helper()

	for _, check := range []struct {
		query string
		want  int
	}{
		{`SELECT count(*) FROM articles`, articles},
		{`SELECT count(*) FROM article_entities`, links},
		{`SELECT count(*) FROM unmapped_articles`, unmapped},
	} {
		var got int
		if err := pool.QueryRow(t.Context(), check.query).Scan(&got); err != nil {
			t.Fatalf("count rows (%s): %v", check.query, err)
		}
		if got != check.want {
			t.Fatalf("count for %q = %d, want %d", check.query, got, check.want)
		}
	}
}

func articleIDs(t *testing.T, pool *pgxpool.Pool, sourceID uuid.UUID) []uuid.UUID {
	t.Helper()

	rows, err := pool.Query(t.Context(),
		`SELECT id FROM articles WHERE source_id = $1 ORDER BY url`, sourceID,
	)
	if err != nil {
		t.Fatalf("list article IDs: %v", err)
	}
	defer rows.Close()

	var found []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan article ID: %v", err)
		}
		found = append(found, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate article IDs: %v", err)
	}
	return found
}

func equalIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
