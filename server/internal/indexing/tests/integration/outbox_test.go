//go:build integration

package integration_test

import (
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	"github.com/Rahmannugar/macro-terminal/server/internal/indexing/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIndexingOutboxLifecycle(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := repositories.NewOutboxRepository(pool)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: "Indexing Outbox Test Source", Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	enriched := seedArticle(t, pool, source.ID, "Fed holds rates steady", "https://example.test/a")
	seedArticle(t, pool, source.ID, "ECB signals a cut", "https://example.test/b")
	seedEnrichment(t, pool, enriched)

	queued, err := repository.EnqueueMissingIndexJobs(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue missing index jobs: %v", err)
	}
	if queued != 1 {
		t.Fatalf("first enqueue = %d, want 1 (only the enriched article)", queued)
	}
	queued, err = repository.EnqueueMissingIndexJobs(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue repeat: %v", err)
	}
	if queued != 0 {
		t.Fatalf("repeat enqueue = %d, want 0", queued)
	}

	claimed, err := repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim batch: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ArticleID != enriched || claimed[0].Attempts != 1 {
		t.Fatalf("claimed = %+v, want the enriched article on attempt 1", claimed)
	}
	if got := outboxCount(t, pool, "status = 'processing'"); got != 1 {
		t.Fatalf("processing rows = %d, want 1", got)
	}

	if err := repository.Complete(t.Context(), claimed[0].ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got := outboxCount(t, pool, "status = 'done'"); got != 1 {
		t.Fatalf("done rows = %d, want 1", got)
	}

	queued, err = repository.EnqueueMissingIndexJobs(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue after claim: %v", err)
	}
	if queued != 0 {
		t.Fatalf("enqueue after claim = %d, want 0 (the done row blocks requeueing)", queued)
	}

	failing := seedArticle(t, pool, source.ID, "BoJ holds policy", "https://example.test/c")
	seedEnrichment(t, pool, failing)
	queued, err = repository.EnqueueMissingIndexJobs(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue the second enriched article: %v", err)
	}
	if queued != 1 {
		t.Fatalf("enqueue = %d, want 1 for the second enriched article", queued)
	}
	claimed, err = repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim the second article: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}
	if err := repository.Fail(t.Context(), claimed[0].ID, 8, "indexer unavailable"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	status, attempts, lastError, availableAt := outboxRow(t, pool, claimed[0].ID)
	if status != "pending" || attempts != 1 || lastError != "indexer unavailable" {
		t.Fatalf("row = status %s attempts %d error %q, want pending/1/indexer unavailable",
			status, attempts, lastError)
	}
	if !availableAt.After(time.Now().Add(20 * time.Second)) {
		t.Fatalf("available_at = %s, want backoff into the future", availableAt)
	}

	claimed, err = repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim while backing off: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed during backoff = %d, want 0", len(claimed))
	}

	if _, err := pool.Exec(t.Context(),
		`UPDATE outbox SET status = 'processing', updated_at = now() - interval '11 minutes' WHERE id = $1`,
		claimedID(t, pool, failing)); err != nil {
		t.Fatalf("age the row: %v", err)
	}
	reclaimed, err := repository.ReclaimStaleIndexJobs(t.Context())
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if reclaimed != 1 {
		t.Fatalf("reclaimed = %d, want 1", reclaimed)
	}

	if _, err := pool.Exec(t.Context(),
		`UPDATE outbox SET attempts = 7, status = 'pending', available_at = now() WHERE id = $1`,
		claimedID(t, pool, failing)); err != nil {
		t.Fatalf("prime the attempt budget: %v", err)
	}
	claimed, err = repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim final attempt: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Attempts != 8 {
		t.Fatalf("claimed = %+v, want one row on attempt 8", claimed)
	}
	if err := repository.Fail(t.Context(), claimed[0].ID, 8, "exhausted"); err != nil {
		t.Fatalf("fail final attempt: %v", err)
	}
	status, attempts, _, _ = outboxRow(t, pool, claimed[0].ID)
	if status != "failed" || attempts != 8 {
		t.Fatalf("row = %s attempts %d, want failed/8", status, attempts)
	}

	orphanID := uuid.New()
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO outbox (id, type, payload) VALUES ($1, 'article_index', jsonb_build_object('article_id', $2::text))`,
		orphanID, uuid.New().String()); err != nil {
		t.Fatalf("insert orphan payload: %v", err)
	}
	claimed, err = repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim orphan: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != orphanID {
		t.Fatalf("claimed = %+v, want the orphan row", claimed)
	}
	if _, err := repository.Article(t.Context(), claimed[0].ArticleID); err == nil {
		t.Fatal("Article succeeded for a missing article, want an error")
	}
	if err := repository.FailPermanently(t.Context(), claimed[0].ID, "article not found"); err != nil {
		t.Fatalf("fail permanently: %v", err)
	}
	status, _, lastError, _ = outboxRow(t, pool, orphanID)
	if status != "failed" || lastError != "article not found" {
		t.Fatalf("orphan row = %s error %q, want failed/article not found", status, lastError)
	}

	reopened, err := repository.ReopenIndexJobs(t.Context())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened != 3 {
		t.Fatalf("reopened = %d, want 3 (done, failed, orphan failed)", reopened)
	}
	if got := outboxCount(t, pool, "status = 'pending' AND attempts = 0 AND last_error IS NULL"); got != 3 {
		t.Fatalf("reopened rows = %d, want 3 reset for the rebuild", got)
	}
	queued, err = repository.EnqueueMissingIndexJobs(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue after reopen: %v", err)
	}
	if queued != 0 {
		t.Fatalf("enqueue after reopen = %d, want 0", queued)
	}

	article, err := repository.Article(t.Context(), enriched)
	if err != nil {
		t.Fatalf("load enriched article: %v", err)
	}
	if article.Title != "Fed holds rates steady" || article.Content == "" || article.PublishedAt == nil {
		t.Fatalf("article = %+v, want the seeded title, content, and publish date", article)
	}
}

func seedArticle(t *testing.T, pool *pgxpool.Pool, sourceID uuid.UUID, title, url string) uuid.UUID {
	t.Helper()
	id := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO articles (id, source_id, title, content, url, published_at)
		 VALUES ($1, $2, $3, $4, $5, now() - interval '1 day')`,
		id, sourceID, title, "Article body for indexing tests.", url); err != nil {
		t.Fatalf("insert article: %v", err)
	}
	return id
}

func seedEnrichment(t *testing.T, pool *pgxpool.Pool, articleID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO article_enrichments (id, article_id, model, result)
		 VALUES ($1, $2, 'gemini-3.1-flash-lite', '{"entities":[],"topics":[],"concepts":[]}'::jsonb)`,
		uuid.New(), articleID); err != nil {
		t.Fatalf("insert enrichment: %v", err)
	}
}

func claimedID(t *testing.T, pool *pgxpool.Pool, articleID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(t.Context(),
		`SELECT id FROM outbox WHERE type = 'article_index' AND payload->>'article_id' = $1`,
		articleID.String()).Scan(&id)
	if err != nil {
		t.Fatalf("find outbox row for article: %v", err)
	}
	return id
}

func outboxRow(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) (status string, attempts int32, lastError string, availableAt time.Time) {
	t.Helper()
	var nullableError *string
	err := pool.QueryRow(t.Context(),
		`SELECT status, attempts, last_error, available_at FROM outbox WHERE id = $1`, id,
	).Scan(&status, &attempts, &nullableError, &availableAt)
	if err != nil {
		t.Fatalf("read outbox row: %v", err)
	}
	if nullableError != nil {
		lastError = *nullableError
	}
	return status, attempts, lastError, availableAt
}

func outboxCount(t *testing.T, pool *pgxpool.Pool, condition string) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM outbox WHERE type = 'article_index' AND (`+condition+`)`).Scan(&count); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	return count
}

func testID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := ids.New()
	if err != nil {
		t.Fatalf("generate ID: %v", err)
	}
	return id
}
