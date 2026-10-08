//go:build integration

package integration_test

import (
	"encoding/json"
	"testing"
	"time"

	articlemodels "github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	articlerepositories "github.com/Rahmannugar/macro-terminal/server/internal/articles/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	entityrepositories "github.com/Rahmannugar/macro-terminal/server/internal/entities/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
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

	articleRepository := articlerepositories.NewArticleRepository(pool, nil)
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

func TestPersistArticlesKeepsStoredContentWhenDeliveryHasNone(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID:   testID(t),
		Name: "Article Content Preservation Source",
		Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	articleRepository := articlerepositories.NewArticleRepository(pool, nil)
	entry := articlemodels.PersistEntry{
		SourceID: source.ID,
		Title:    "PBOC adjusts policy tools",
		Content:  "<p>Hydrated article body.</p>",
		URL:      "https://example.com/pboc-tools",
	}
	if _, err := articleRepository.PersistArticles(t.Context(), []articlemodels.PersistEntry{entry}); err != nil {
		t.Fatalf("persist article: %v", err)
	}

	// A listing re-delivery without a body must not erase stored content.
	emptyDelivery := entry
	emptyDelivery.Content = ""
	if _, err := articleRepository.PersistArticles(t.Context(), []articlemodels.PersistEntry{emptyDelivery}); err != nil {
		t.Fatalf("persist empty delivery: %v", err)
	}
	assertContent(t, pool, source.ID, entry.URL, entry.Content)

	// A delivery with new text still wins.
	entry.Content = "<p>Updated full text.</p>"
	if _, err := articleRepository.PersistArticles(t.Context(), []articlemodels.PersistEntry{entry}); err != nil {
		t.Fatalf("persist updated delivery: %v", err)
	}
	assertContent(t, pool, source.ID, entry.URL, entry.Content)
}

func assertContent(t *testing.T, pool *pgxpool.Pool, sourceID uuid.UUID, url, want string) {
	t.Helper()
	var got *string
	if err := pool.QueryRow(t.Context(),
		`SELECT content FROM articles WHERE source_id = $1 AND url = $2`,
		sourceID, url,
	).Scan(&got); err != nil {
		t.Fatalf("read stored content: %v", err)
	}
	if got == nil || *got != want {
		t.Fatalf("stored content = %v, want %q", got, want)
	}
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

func newCacheStore(t *testing.T) (*cache.JSONStore, *miniredis.Miniredis) {
	t.Helper()
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(server.Close)
	return cache.NewJSONStore(redis.NewClient(&redis.Options{Addr: server.Addr()})), server
}

func TestArticleCacheWriteThroughAndReadThrough(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: "Article Cache Test Source", Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	store, server := newCacheStore(t)
	repository := articlerepositories.NewArticleRepository(pool, store)

	published := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	if _, err := repository.PersistArticles(t.Context(), []articlemodels.PersistEntry{{
		SourceID:    source.ID,
		Title:       "Cache fed funds",
		Content:     "Cached content for the write-through test.",
		URL:         "https://example.com/cache-fed",
		PublishedAt: &published,
	}}); err != nil {
		t.Fatalf("persist article: %v", err)
	}

	ids := articleIDs(t, pool, source.ID)
	if len(ids) != 1 {
		t.Fatalf("article ids = %v, want one row", ids)
	}
	key := cache.ArticleKey(ids[0])

	raw, err := server.Get(key)
	if err != nil {
		t.Fatalf("read cached article: %v", err)
	}
	var cached articlemodels.StoredArticle
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		t.Fatalf("decode cached article %q: %v", raw, err)
	}
	if cached.Title != "Cache fed funds" || cached.SourceName != source.Name {
		t.Fatalf("cached article = %+v, want the hydrated row", cached)
	}
	if ttl := server.TTL(key); ttl > time.Hour || ttl < 54*time.Minute {
		t.Fatalf("ttl = %s, want between 54m and 1h", ttl)
	}

	if _, err := pool.Exec(t.Context(), `DELETE FROM articles WHERE id = $1`, ids[0]); err != nil {
		t.Fatalf("delete article: %v", err)
	}
	found, err := repository.GetArticlesByIDs(t.Context(), ids)
	if err != nil {
		t.Fatalf("read through cache: %v", err)
	}
	if len(found) != 1 || found[0].Title != "Cache fed funds" {
		t.Fatalf("articles = %+v, want the cached row without PostgreSQL", found)
	}

	if err := server.Set(key, "not json"); err != nil {
		t.Fatalf("corrupt cached article: %v", err)
	}
	found, err = repository.GetArticlesByIDs(t.Context(), ids)
	if err != nil {
		t.Fatalf("read with corrupt cache: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("articles = %+v, want a miss that falls back to PostgreSQL", found)
	}
}

func TestArticleCacheKeepsWorkingWhenRedisIsUnreachable(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: "Article Cache Outage Source", Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	store, server := newCacheStore(t)
	repository := articlerepositories.NewArticleRepository(pool, store)

	entry := articlemodels.PersistEntry{
		SourceID: source.ID,
		Title:    "Article written during the outage",
		URL:      "https://example.com/outage-fed",
	}
	if _, err := repository.PersistArticles(t.Context(), []articlemodels.PersistEntry{entry}); err != nil {
		t.Fatalf("persist during outage: %v", err)
	}

	server.Close()

	found, err := repository.GetArticlesByIDs(t.Context(), articleIDs(t, pool, source.ID))
	if err != nil {
		t.Fatalf("read during outage: %v", err)
	}
	if len(found) != 1 || found[0].Title != entry.Title {
		t.Fatalf("articles = %+v, want PostgreSQL to answer without Redis", found)
	}

	if _, err := repository.PersistArticles(t.Context(), []articlemodels.PersistEntry{{
		SourceID: source.ID,
		Title:    "Second article written during the outage",
		URL:      "https://example.com/outage-fed-2",
	}}); err != nil {
		t.Fatalf("second persist during outage: %v", err)
	}
}

func TestPersistArticlesKeepsStoredImageWhenDeliveryHasNone(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID:   testID(t),
		Name: "Article Image Preservation Source",
		Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	articleRepository := articlerepositories.NewArticleRepository(pool, nil)
	entry := articlemodels.PersistEntry{
		SourceID: source.ID,
		Title:    "Rate decision published",
		Content:  "Body.",
		URL:      "https://example.com/rate-decision",
		ImageURL: "https://cdn.example.com/photos/rate.jpg",
	}
	if _, err := articleRepository.PersistArticles(t.Context(), []articlemodels.PersistEntry{entry}); err != nil {
		t.Fatalf("persist article: %v", err)
	}
	assertImage(t, pool, source.ID, entry.URL, &entry.ImageURL)

	// A re-delivery without an image must not erase the stored link.
	emptyDelivery := entry
	emptyDelivery.ImageURL = ""
	if _, err := articleRepository.PersistArticles(t.Context(), []articlemodels.PersistEntry{emptyDelivery}); err != nil {
		t.Fatalf("persist imageless delivery: %v", err)
	}
	assertImage(t, pool, source.ID, entry.URL, &entry.ImageURL)

	// A delivery with a new image still wins.
	entry.ImageURL = "https://cdn.example.com/photos/rate-updated.jpg"
	if _, err := articleRepository.PersistArticles(t.Context(), []articlemodels.PersistEntry{entry}); err != nil {
		t.Fatalf("persist updated delivery: %v", err)
	}
	assertImage(t, pool, source.ID, entry.URL, &entry.ImageURL)
}

func assertImage(t *testing.T, pool *pgxpool.Pool, sourceID uuid.UUID, url string, want *string) {
	t.Helper()
	var got *string
	if err := pool.QueryRow(t.Context(),
		`SELECT image_url FROM articles WHERE source_id = $1 AND url = $2`,
		sourceID, url,
	).Scan(&got); err != nil {
		t.Fatalf("read stored image: %v", err)
	}
	if got == nil || want == nil || *got != *want {
		t.Fatalf("stored image = %v, want %v", got, want)
	}
}
