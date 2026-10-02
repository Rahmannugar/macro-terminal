//go:build integration

package integration_test

import (
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	"github.com/Rahmannugar/macro-terminal/server/internal/enrichment/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEnrichmentOutboxLifecycle(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := repositories.NewOutboxRepository(pool)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: "Enrichment Outbox Test Source", Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	articleA := seedArticle(t, pool, source.ID, "Fed holds rates steady", "https://example.test/a")
	articleB := seedArticle(t, pool, source.ID, "ECB signals a cut", "https://example.test/b")

	queued, err := repository.EnqueueMissingArticles(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue missing articles: %v", err)
	}
	if queued != 2 {
		t.Fatalf("first enqueue = %d, want 2", queued)
	}
	queued, err = repository.EnqueueMissingArticles(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue repeat: %v", err)
	}
	if queued != 0 {
		t.Fatalf("repeat enqueue = %d, want 0 (already queued)", queued)
	}

	claimed, err := repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim batch: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed = %d, want 2", len(claimed))
	}
	for _, work := range claimed {
		if work.Attempts != 1 {
			t.Errorf("attempts = %d, want 1 after the first claim", work.Attempts)
		}
	}
	if got := outboxCount(t, pool, "status = 'processing'"); got != 2 {
		t.Fatalf("processing rows = %d, want 2", got)
	}

	var completed uuid.UUID
	for _, work := range claimed {
		if work.ArticleID == articleA {
			completed = work.ID
		}
	}
	if completed == uuid.Nil {
		t.Fatal("article A row was not claimed")
	}
	if err := repository.Complete(t.Context(), completed); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got := outboxCount(t, pool, "status = 'done'"); got != 1 {
		t.Fatalf("done rows = %d, want 1", got)
	}

	var backoffID uuid.UUID
	for _, work := range claimed {
		if work.ArticleID == articleB {
			backoffID = work.ID
		}
	}
	if backoffID == uuid.Nil {
		t.Fatal("article B row was not claimed")
	}
	if err := repository.Fail(t.Context(), backoffID, 8, "provider unavailable"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	status, attempts, lastError, availableAt := outboxRow(t, pool, backoffID)
	if status != "pending" || attempts != 1 || lastError != "provider unavailable" {
		t.Fatalf("row = status %s attempts %d error %q, want pending/1/provider unavailable",
			status, attempts, lastError)
	}
	if !availableAt.After(time.Now().Add(20 * time.Second)) {
		t.Fatalf("available_at = %s, want backoff into the future", availableAt)
	}

	queued, err = repository.EnqueueMissingArticles(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue after claim: %v", err)
	}
	if queued != 0 {
		t.Fatalf("enqueue after claim = %d, want 0", queued)
	}

	claimed, err = repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim while backing off: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed during backoff = %d, want 0", len(claimed))
	}

	stale, err := pool.Exec(t.Context(),
		`UPDATE outbox SET status = 'processing', updated_at = now() - interval '11 minutes' WHERE id = $1`,
		backoffID)
	if err != nil {
		t.Fatalf("age the row: %v", err)
	}
	if stale.RowsAffected() != 1 {
		t.Fatalf("aged rows = %d, want 1", stale.RowsAffected())
	}
	reclaimed, err := repository.ReclaimStaleEnrichmentJobs(t.Context())
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if reclaimed != 1 {
		t.Fatalf("reclaimed = %d, want 1", reclaimed)
	}
	status, _, _, availableAt = outboxRow(t, pool, backoffID)
	if status != "pending" || availableAt.After(time.Now()) {
		t.Fatalf("row = %s available %s, want pending and due now", status, availableAt)
	}

	if _, err := pool.Exec(t.Context(),
		`UPDATE outbox SET attempts = 7, status = 'pending', available_at = now() WHERE id = $1`,
		backoffID); err != nil {
		t.Fatalf("prime the attempt budget: %v", err)
	}
	claimed, err = repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim final attempt: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Attempts != 8 {
		t.Fatalf("claimed = %+v, want one row on attempt 8", claimed)
	}
	if err := repository.Fail(t.Context(), backoffID, 8, "exhausted"); err != nil {
		t.Fatalf("fail final attempt: %v", err)
	}
	status, attempts, _, _ = outboxRow(t, pool, backoffID)
	if status != "failed" || attempts != 8 {
		t.Fatalf("row = %s attempts %d, want failed/8", status, attempts)
	}

	orphanID := uuid.New()
	_, err = pool.Exec(t.Context(),
		`INSERT INTO outbox (id, type, payload) VALUES ($1, 'article_enrichment', jsonb_build_object('article_id', $2::text))`,
		orphanID, uuid.New().String())
	if err != nil {
		t.Fatalf("insert orphan payload: %v", err)
	}
	claimed, err = repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim orphan: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want the orphan row", len(claimed))
	}
	if _, err := repository.Article(t.Context(), claimed[0].ArticleID); err == nil {
		t.Fatal("Article succeeded for a missing article, want an error")
	}
	if err := repository.FailPermanently(t.Context(), claimed[0].ID, "article not found"); err != nil {
		t.Fatalf("fail permanently: %v", err)
	}
	status, _, lastError, _ = outboxRow(t, pool, claimed[0].ID)
	if status != "failed" || lastError != "article not found" {
		t.Fatalf("orphan row = %s error %q, want failed/article not found", status, lastError)
	}

	stored, err := repository.StoreEnrichment(t.Context(), articleA, "gemini-3.1-flash-lite",
		[]byte(`{"entities":["USD"],"topics":["monetary_policy"],"concepts":["rate decision"]}`))
	if err != nil || !stored {
		t.Fatalf("first store = %v, %v, want written", stored, err)
	}
	stored, err = repository.StoreEnrichment(t.Context(), articleA, "gemini-3.1-flash-lite",
		[]byte(`{"entities":[],"topics":[],"concepts":[]}`))
	if err != nil {
		t.Fatalf("replay store: %v", err)
	}
	if stored {
		t.Fatal("replay store wrote a second row, want a no-op")
	}
	if got := outboxCount(t, pool, "true"); got != 3 {
		t.Fatalf("outbox rows = %d, want 3 (done, failed, orphan failed)", got)
	}
}

func seedArticle(t *testing.T, pool *pgxpool.Pool, sourceID uuid.UUID, title, url string) uuid.UUID {
	t.Helper()
	id := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO articles (id, source_id, title, content, url) VALUES ($1, $2, $3, $4, $5)`,
		id, sourceID, title, "Article body for enrichment tests.", url); err != nil {
		t.Fatalf("insert article: %v", err)
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
		`SELECT count(*) FROM outbox WHERE `+condition).Scan(&count); err != nil {
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
