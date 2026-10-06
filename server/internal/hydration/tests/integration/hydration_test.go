//go:build integration

package integration_test

import (
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	hydrationrepositories "github.com/Rahmannugar/macro-terminal/server/internal/hydration/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const listingConfig = `{"url":"https://example.test/listing","selectors":{"item":"li","title":"a","content":".body"}}`

func TestContentEnqueueGuards(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := hydrationrepositories.NewRepository(pool, nil)
	sourceRepository := sourcerepositories.NewSourceRepository(pool)

	hydratedSource := seedHydrationSource(t, pool, sourceRepository, "Content Source", "web", listingConfig)
	selectorlessSource := seedHydrationSource(t, pool, sourceRepository, "Selectorless Source", "web",
		`{"url":"https://example.test/other","selectors":{"item":"li","title":"a"}}`)
	apiSource := seedHydrationSource(t, pool, sourceRepository, "API Source", "api",
		`{"url":"https://api.example.test/articles"}`)

	seedHydratableArticle(t, pool, hydratedSource, "Missing body", "https://example.test/a")
	existing := "<p>Existing.</p>"
	seedHydrationArticle(t, pool, hydratedSource, "Already has body", "https://example.test/b", &existing)
	seedHydratableArticle(t, pool, selectorlessSource, "No selector source", "https://example.test/c")
	seedHydratableArticle(t, pool, apiSource, "API source", "https://example.test/d")

	queued, err := repository.EnqueueMissingContentJobs(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue content jobs: %v", err)
	}
	if queued != 1 {
		t.Fatalf("first enqueue = %d, want 1 (only the empty article on the selector source)", queued)
	}
	queued, err = repository.EnqueueMissingContentJobs(t.Context(), 16)
	if err != nil {
		t.Fatalf("repeat enqueue: %v", err)
	}
	if queued != 0 {
		t.Fatalf("repeat enqueue = %d, want 0 (the queued row blocks requeueing)", queued)
	}
}

func TestContentClaimStoreComplete(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := hydrationrepositories.NewRepository(pool, nil)
	sourceRepository := sourcerepositories.NewSourceRepository(pool)

	sourceID := seedHydrationSource(t, pool, sourceRepository, "Lifecycle Source", "web", listingConfig)
	articleID := seedHydratableArticle(t, pool, sourceID, "Lifecycle article", "https://example.test/lifecycle")

	if _, err := repository.EnqueueMissingContentJobs(t.Context(), 16); err != nil {
		t.Fatalf("enqueue content jobs: %v", err)
	}
	claimed, err := repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim batch: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}
	work := claimed[0]
	if work.ArticleID != articleID || work.Attempts != 1 {
		t.Fatalf("claimed = %+v, want the article on attempt 1", work)
	}

	target, err := repository.ContentTarget(t.Context(), work.ArticleID)
	if err != nil {
		t.Fatalf("content target: %v", err)
	}
	if target.ArticleURL != "https://example.test/lifecycle" {
		t.Errorf("article URL = %q", target.ArticleURL)
	}
	if target.SourceName != "Lifecycle Source" {
		t.Errorf("source name = %q", target.SourceName)
	}
	if target.ConfigurationType != "web" {
		t.Errorf("configuration type = %q", target.ConfigurationType)
	}

	stored, err := repository.StoreContent(t.Context(), work.ArticleID, "<p>Hydrated body.</p>")
	if err != nil {
		t.Fatalf("store content: %v", err)
	}
	if stored != 1 {
		t.Fatalf("stored = %d, want 1", stored)
	}
	stored, err = repository.StoreContent(t.Context(), work.ArticleID, "<p>Should not overwrite.</p>")
	if err != nil {
		t.Fatalf("second store content: %v", err)
	}
	if stored != 0 {
		t.Fatalf("second stored = %d, want 0 (existing content is never overwritten)", stored)
	}

	if err := repository.Complete(t.Context(), work.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	claimed, err = repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim after complete: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed after complete = %d, want 0", len(claimed))
	}
}

func TestPermanentContentFailureBlocksRequeue(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := hydrationrepositories.NewRepository(pool, nil)
	sourceRepository := sourcerepositories.NewSourceRepository(pool)

	sourceID := seedHydrationSource(t, pool, sourceRepository, "Failing Source", "web", listingConfig)
	seedHydratableArticle(t, pool, sourceID, "Failing article", "https://example.test/fail")

	if _, err := repository.EnqueueMissingContentJobs(t.Context(), 16); err != nil {
		t.Fatalf("enqueue content jobs: %v", err)
	}
	claimed, err := repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim batch: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}
	if err := repository.FailPermanently(t.Context(), claimed[0].ID, "content selector matched nothing"); err != nil {
		t.Fatalf("fail permanently: %v", err)
	}

	queued, err := repository.EnqueueMissingContentJobs(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue after failure: %v", err)
	}
	if queued != 0 {
		t.Fatalf("enqueue after failure = %d, want 0 (the failed row blocks requeueing)", queued)
	}
	claimed, err = repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim after failure: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed after failure = %d, want 0", len(claimed))
	}
}

func seedHydrationSource(
	t *testing.T,
	pool *pgxpool.Pool,
	sourceRepository *sourcerepositories.SourceRepository,
	name string,
	kind string,
	config string,
) uuid.UUID {
	t.Helper()
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: name, Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if _, err := sourceRepository.CreateSourceConfiguration(t.Context(), sourcemodels.SourceConfiguration{
		ID:       testID(t),
		SourceID: source.ID,
		Type:     kind,
		Config:   []byte(config),
	}); err != nil {
		t.Fatalf("create source configuration: %v", err)
	}
	return source.ID
}

func seedHydratableArticle(t *testing.T, pool *pgxpool.Pool, sourceID uuid.UUID, title, url string) uuid.UUID {
	t.Helper()
	return seedHydrationArticle(t, pool, sourceID, title, url, nil)
}

func seedHydrationArticle(t *testing.T, pool *pgxpool.Pool, sourceID uuid.UUID, title, url string, content *string) uuid.UUID {
	t.Helper()
	id := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO articles (id, source_id, title, content, url, published_at)
		 VALUES ($1, $2, $3, $4, $5, now() - interval '1 hour')`,
		id, sourceID, title, content, url); err != nil {
		t.Fatalf("insert article: %v", err)
	}
	return id
}

func testID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := ids.New()
	if err != nil {
		t.Fatalf("generate ID: %v", err)
	}
	return id
}
