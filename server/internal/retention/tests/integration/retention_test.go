//go:build integration

package integration_test

import (
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	"github.com/Rahmannugar/macro-terminal/server/internal/retention/repositories"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRetentionFlushesExpiredArticleGraph seeds an expired article wired to every
// dependent row the flush pass owns, plus a fresh article that must survive, then
// asserts the whole expired graph disappears while the fresh one stays.
func TestRetentionFlushesExpiredArticleGraph(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := repositories.NewRepository(pool)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: "Retention Test Source", Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	// Seeded past the 183-day window so it must be flushed; the fresh one stays.
	expired := seedArticle(t, pool, source.ID, "https://example.test/expired", "-200 days")
	fresh := seedArticle(t, pool, source.ID, "https://example.test/fresh", "-1 day")

	userID, entityPairID, entityID := seedUserPairEntity(t, pool)
	clusterID := seedStoryCluster(t, pool, expired)
	seedDependents(t, pool, expired, userID, entityPairID, entityID)

	cutoff := time.Now().AddDate(0, 0, -183)

	ids, err := repository.ListExpiredArticleIDs(t.Context(), cutoff, 500)
	if err != nil {
		t.Fatalf("list expired article ids: %v", err)
	}
	if len(ids) != 1 || ids[0] != expired {
		t.Fatalf("expired ids = %v, want only the aged article", ids)
	}

	if err := repository.DeleteArticles(t.Context(), ids); err != nil {
		t.Fatalf("delete expired articles: %v", err)
	}

	assertCountZero(t, pool, "articles", "id = $1", expired)
	assertCountZero(t, pool, "unmapped_articles", "article_id = $1", expired)
	assertCountZero(t, pool, "article_entities", "article_id = $1", expired)
	assertCountZero(t, pool, "article_enrichments", "article_id = $1", expired)
	assertCountZero(t, pool, "article_story_clusters", "article_id = $1", expired)
	assertCountZero(t, pool, "notifications", "subject_type = 'article' AND subject_id = $1", expired)
	assertCountZero(t, pool, "outbox", "type = 'article_index' AND payload->>'article_id' = $1", expired)
	assertCountZero(t, pool, "outbox", "type = 'article_content' AND payload->>'article_id' = $1", expired)
	assertCountZero(t, pool, "outbox", "type = 'asset_notification' AND payload->>'subject_id' = $1", expired)
	assertCountZero(t, pool, "outbox",
		"type = 'webhook_delivery' AND (payload->>'notification_id')::uuid IN (SELECT id FROM notifications WHERE subject_type = 'article')",
	)

	if !exists(t, pool, "articles", "id = $1", fresh) {
		t.Fatal("the fresh article was deleted, want it kept")
	}
	if exists(t, pool, "story_clusters", "id = $1", clusterID) {
		t.Fatal("the emptied story cluster survived, want it removed")
	}
}

// TestRetentionPurgesAgedHistory asserts the aged audit and read-state purges keep
// recent rows and drop only what is past each window.
func TestRetentionPurgesAgedHistory(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := repositories.NewRepository(pool)

	agedDone := insertOutbox(t, pool, "article_index", "done", "-30 days")
	recentDone := insertOutbox(t, pool, "article_index", "done", "-1 day")
	agedFailed := insertOutbox(t, pool, "article_index", "failed", "-120 days")
	recentFailed := insertOutbox(t, pool, "article_index", "failed", "-1 day")

	purged, err := repository.PurgeDoneOutboxRows(t.Context(), time.Now().AddDate(0, 0, -14))
	if err != nil {
		t.Fatalf("purge done outbox rows: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged done rows = %d, want 1 (only the aged one)", purged)
	}
	if exists(t, pool, "outbox", "id = $1", agedDone) {
		t.Fatal("the aged done row survived its purge, want it removed")
	}
	if !exists(t, pool, "outbox", "id = $1", recentDone) {
		t.Fatal("the recent done row was purged, want it kept")
	}

	purged, err = repository.PurgeFailedOutboxRows(t.Context(), time.Now().AddDate(0, 0, -90))
	if err != nil {
		t.Fatalf("purge failed outbox rows: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged failed rows = %d, want 1 (only the aged one)", purged)
	}
	if !exists(t, pool, "outbox", "id = $1", recentDone) {
		t.Fatal("the failed purge removed a done row, want done rows untouched")
	}
	if !exists(t, pool, "outbox", "id = $1", recentFailed) {
		t.Fatal("the recent failed row was purged, want it kept")
	}
	if exists(t, pool, "outbox", "id = $1", agedFailed) {
		t.Fatal("the aged failed row survived its purge, want it removed")
	}
}

func seedArticle(t *testing.T, pool *pgxpool.Pool, sourceID uuid.UUID, url, age string) uuid.UUID {
	t.Helper()
	id := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO articles (id, source_id, title, url, published_at)
		 VALUES ($1, $2, $3, $4, now() + interval '`+age+`')`,
		id, sourceID, "Retention subject "+url, url); err != nil {
		t.Fatalf("insert article: %v", err)
	}
	return id
}

func seedStoryCluster(t *testing.T, pool *pgxpool.Pool, articleID uuid.UUID) uuid.UUID {
	t.Helper()
	clusterID := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO story_clusters (id, title) VALUES ($1, 'Retention cluster')`,
		clusterID); err != nil {
		t.Fatalf("insert story cluster: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO article_story_clusters (article_id, story_cluster_id) VALUES ($1, $2)`,
		articleID, clusterID); err != nil {
		t.Fatalf("insert article story cluster: %v", err)
	}
	return clusterID
}

// seedDependents wires the expired article to every dependent row the flush owns:
// the unmapped fallback, the entity link, the enrichment, a queued index job, a
// queued content job, a fan-out row, an in-app notification, and its webhook delivery.
func seedDependents(t *testing.T, pool *pgxpool.Pool, articleID, userID, entityPairID, entityID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO unmapped_articles (id, article_id) VALUES ($1, $2)`,
		testID(t), articleID); err != nil {
		t.Fatalf("insert unmapped article: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO article_entities (article_id, entity_id) VALUES ($1, $2)`,
		articleID, entityID); err != nil {
		t.Fatalf("insert article entity: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO article_enrichments (id, article_id, model, result)
		 VALUES ($1, $2, 'gemini-3.1-flash-lite', '{"entities":[],"topics":[]}'::jsonb)`,
		testID(t), articleID); err != nil {
		t.Fatalf("insert article enrichment: %v", err)
	}
	for _, jobType := range []string{"article_index", "article_content", "article_enrichment", "article_cluster"} {
		if _, err := pool.Exec(t.Context(),
			`INSERT INTO outbox (id, type, payload)
			 VALUES ($1, $2, jsonb_build_object('article_id', $3::text))`,
			testID(t), jobType, articleID); err != nil {
			t.Fatalf("insert %s outbox row: %v", jobType, err)
		}
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO outbox (id, type, payload)
		 VALUES ($1, 'asset_notification', jsonb_build_object('subject_type', 'article', 'subject_id', $2::text))`,
		testID(t), articleID); err != nil {
		t.Fatalf("insert asset_notification row: %v", err)
	}
	notificationID := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO notifications (id, user_id, type, entity_pair_id, subject_type, subject_id, channel, title)
		 VALUES ($1, $2, 'new_article', $3, 'article', $4, 'in-app', 'Retention subject')`,
		notificationID, userID, entityPairID, articleID); err != nil {
		t.Fatalf("insert notification: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO outbox (id, type, payload)
		 VALUES ($1, 'webhook_delivery', jsonb_build_object('notification_id', $2::text))`,
		testID(t), notificationID); err != nil {
		t.Fatalf("insert webhook delivery: %v", err)
	}
}

func seedUserPairEntity(t *testing.T, pool *pgxpool.Pool) (userID, entityPairID, entityID uuid.UUID) {
	t.Helper()
	userID, entityPairID = testID(t), testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, username, authlier_subject_id) VALUES ($1, $2, $3)`,
		userID, "retention-user", userID.String()); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	baseID, quoteID := testID(t), testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO entities (id, code, name, type)
		 VALUES ($1, 'fed', 'Federal Reserve', 'central_bank'), ($2, 'ecb', 'ECB', 'central_bank')`,
		baseID, quoteID); err != nil {
		t.Fatalf("insert entities: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO entity_pairs (id, base_entity_id, quote_entity_id, symbol) VALUES ($1, $2, $3, 'FED_ECB')`,
		entityPairID, baseID, quoteID); err != nil {
		t.Fatalf("insert entity pair: %v", err)
	}
	return userID, entityPairID, baseID
}

func insertOutbox(t *testing.T, pool *pgxpool.Pool, jobType, status, age string) uuid.UUID {
	t.Helper()
	id := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO outbox (id, type, payload, status, updated_at)
		 VALUES ($1, $2, jsonb_build_object('article_id', $3::text), $4, now() + interval '`+age+`')`,
		id, jobType, uuid.New().String(), status); err != nil {
		t.Fatalf("insert outbox row: %v", err)
	}
	return id
}

func assertCountZero(t *testing.T, pool *pgxpool.Pool, table, condition string, args ...any) {
	t.Helper()
	var count int64
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM `+table+` WHERE `+condition, args...).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != 0 {
		t.Fatalf("%s still has %d row(s) for the expired article", table, count)
	}
}

func exists(t *testing.T, pool *pgxpool.Pool, table, condition string, args ...any) bool {
	t.Helper()
	var found bool
	if err := pool.QueryRow(t.Context(),
		`SELECT EXISTS (SELECT 1 FROM `+table+` WHERE `+condition+`)`, args...).Scan(&found); err != nil {
		t.Fatalf("check %s: %v", table, err)
	}
	return found
}

func testID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := ids.New()
	if err != nil {
		t.Fatalf("generate ID: %v", err)
	}
	return id
}
