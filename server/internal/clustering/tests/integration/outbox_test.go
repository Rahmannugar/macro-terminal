//go:build integration

package integration_test

import (
	"testing"

	clusteringrepositories "github.com/Rahmannugar/macro-terminal/server/internal/clustering/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestClusteringDiscoveryAndClusterLinks(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := clusteringrepositories.NewRepository(pool)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: "Clustering Outbox Test Source", Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	indexed := seedArticle(t, pool, source.ID, "Fed holds rates steady", "https://example.test/a")
	neverIndexed := seedArticle(t, pool, source.ID, "ECB signals a cut", "https://example.test/b")
	failedIndex := seedArticle(t, pool, source.ID, "BoJ holds policy", "https://example.test/c")
	seedIndexRow(t, pool, indexed, "done")
	seedIndexRow(t, pool, failedIndex, "failed")

	queued, err := repository.EnqueueMissingClusterJobs(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue missing cluster jobs: %v", err)
	}
	if queued != 1 {
		t.Fatalf("first enqueue = %d, want 1 (only the indexed article)", queued)
	}
	queued, err = repository.EnqueueMissingClusterJobs(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue repeat: %v", err)
	}
	if queued != 0 {
		t.Fatalf("repeat enqueue = %d, want 0 (the queued row blocks requeueing)", queued)
	}

	seedIndexRow(t, pool, neverIndexed, "pending")
	claimed, err := repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim batch: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ArticleID != indexed || claimed[0].Attempts != 1 {
		t.Fatalf("claimed = %+v, want only the done-index article on attempt 1", claimed)
	}

	if err := repository.Complete(t.Context(), claimed[0].ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	queued, err = repository.EnqueueMissingClusterJobs(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue after complete: %v", err)
	}
	if queued != 0 {
		t.Fatalf("enqueue after complete = %d, want 0 (the done row blocks requeueing)", queued)
	}

	clusterID := testID(t)
	if err := repository.CreateStoryCluster(t.Context(), clusterID, "Fed holds rates steady"); err != nil {
		t.Fatalf("create story cluster: %v", err)
	}
	linked, err := repository.LinkArticleToCluster(t.Context(), indexed, clusterID)
	if err != nil {
		t.Fatalf("link article: %v", err)
	}
	if linked != 1 {
		t.Fatalf("first link = %d, want 1", linked)
	}
	linked, err = repository.LinkArticleToCluster(t.Context(), indexed, clusterID)
	if err != nil {
		t.Fatalf("duplicate link: %v", err)
	}
	if linked != 0 {
		t.Fatalf("duplicate link = %d, want 0 (links are idempotent)", linked)
	}
	if err := repository.TouchStoryCluster(t.Context(), clusterID); err != nil {
		t.Fatalf("touch story cluster: %v", err)
	}

	memberOf, err := repository.ClusterOfArticle(t.Context(), indexed)
	if err != nil {
		t.Fatalf("cluster of article: %v", err)
	}
	if memberOf != clusterID {
		t.Fatalf("cluster = %s, want %s", memberOf, clusterID)
	}
	members, err := repository.MembersOfCluster(t.Context(), clusterID)
	if err != nil {
		t.Fatalf("members of cluster: %v", err)
	}
	if len(members) != 1 || members[0] != indexed {
		t.Fatalf("members = %v, want [%s]", members, indexed)
	}
	memberships, err := repository.ClusterIDsForArticles(t.Context(), []uuid.UUID{indexed, neverIndexed})
	if err != nil {
		t.Fatalf("cluster ids for articles: %v", err)
	}
	if memberships[indexed] != clusterID {
		t.Errorf("memberships = %v, want the linked article mapped to the cluster", memberships)
	}
	if _, ok := memberships[neverIndexed]; ok {
		t.Errorf("memberships = %v, want no entry for the unlinked article", memberships)
	}
	titles, err := repository.ArticleTitles(t.Context(), []uuid.UUID{indexed})
	if err != nil {
		t.Fatalf("article titles: %v", err)
	}
	if titles[indexed] != "Fed holds rates steady" {
		t.Errorf("titles = %v, want the seeded headline", titles)
	}
}

func seedArticle(t *testing.T, pool *pgxpool.Pool, sourceID uuid.UUID, title, url string) uuid.UUID {
	t.Helper()
	id := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO articles (id, source_id, title, content, url, published_at)
		 VALUES ($1, $2, $3, $4, $5, now() - interval '1 day')`,
		id, sourceID, title, "Article body for clustering tests.", url); err != nil {
		t.Fatalf("insert article: %v", err)
	}
	return id
}

func seedIndexRow(t *testing.T, pool *pgxpool.Pool, articleID uuid.UUID, status string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO outbox (id, type, payload, status)
		 VALUES ($1, 'article_index', jsonb_build_object('article_id', $2::text), $3)`,
		uuid.New(), articleID.String(), status); err != nil {
		t.Fatalf("insert index outbox row: %v", err)
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
