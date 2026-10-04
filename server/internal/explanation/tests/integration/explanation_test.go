//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
	articlemodels "github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	articlerepositories "github.com/Rahmannugar/macro-terminal/server/internal/articles/repositories"
	calendarmodels "github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	calendarrepositories "github.com/Rahmannugar/macro-terminal/server/internal/calendar/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/clustering"
	clusteringrepositories "github.com/Rahmannugar/macro-terminal/server/internal/clustering/repositories"
	enrichmentrepositories "github.com/Rahmannugar/macro-terminal/server/internal/enrichment/repositories"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	entityrepositories "github.com/Rahmannugar/macro-terminal/server/internal/entities/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/explanation"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type recordingExplainer struct {
	articleCalls int
	eventCalls   int
	article      ai.ExplainArticleInput
	event        ai.ExplainEventInput
}

func (recorder *recordingExplainer) ExplainArticle(_ context.Context, input ai.ExplainArticleInput) (string, error) {
	recorder.articleCalls++
	recorder.article = input
	return "generated article explanation", nil
}

func (recorder *recordingExplainer) ExplainEvent(_ context.Context, input ai.ExplainEventInput) (string, error) {
	recorder.eventCalls++
	recorder.event = input
	return "generated event explanation", nil
}

func TestExplainArticleReadsContextFromPostgreSQL(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	fixture := seedContext(t, pool)

	response, err := fixture.service.ExplainArticle(t.Context(), fixture.articleOneID)
	if err != nil {
		t.Fatalf("ExplainArticle: %v", err)
	}
	if response.Explanation.Text != "generated article explanation" {
		t.Fatalf("text = %q, want the generated answer", response.Explanation.Text)
	}

	input := fixture.explainer.article
	if input.Title != "Fed holds rates steady" || input.SourceName != "Explanation Source" {
		t.Errorf("article = %q from %q, want the stored row", input.Title, input.SourceName)
	}
	if len(input.Enrichment.Entities) != 1 || input.Enrichment.Entities[0] != "USD" ||
		len(input.Enrichment.Topics) != 1 || input.Enrichment.Topics[0] != "monetary_policy" {
		t.Errorf("enrichment = %+v, want the stored JSON result", input.Enrichment)
	}
	if len(input.Entities) != 2 || input.Entities[0] != "EUR Euro" || input.Entities[1] != "USD US Dollar" {
		t.Errorf("entities = %v, want both linked entities ordered by code", input.Entities)
	}
	if len(input.Pairs) != 1 || input.Pairs[0] != "EUR/USD" {
		t.Errorf("pairs = %v, want the affected pair symbol", input.Pairs)
	}
	if input.ClusterTitle != "Fed policy outlook" {
		t.Errorf("cluster title = %q, want the story cluster title", input.ClusterTitle)
	}
	if len(input.RelatedTitles) != 1 || input.RelatedTitles[0] != "Treasury yields slip after Fed" {
		t.Errorf("related titles = %v, want the cluster mate excluding self", input.RelatedTitles)
	}
	if len(input.KnowledgeTerms) != 1 || input.KnowledgeTerms[0] != "dot plot (topic)" {
		t.Errorf("knowledge terms = %v, want the term linked to an entity", input.KnowledgeTerms)
	}

	if _, err := fixture.service.ExplainArticle(t.Context(), fixture.articleOneID); err != nil {
		t.Fatalf("cached ExplainArticle: %v", err)
	}
	if fixture.explainer.articleCalls != 1 {
		t.Fatalf("explainer calls = %d, want the answer served from Redis", fixture.explainer.articleCalls)
	}
}

func TestExplainCalendarEventReadsContextFromPostgreSQL(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	fixture := seedContext(t, pool)

	response, err := fixture.service.ExplainEvent(t.Context(), fixture.eventID)
	if err != nil {
		t.Fatalf("ExplainEvent: %v", err)
	}
	if response.Explanation.Text != "generated event explanation" {
		t.Fatalf("text = %q, want the generated answer", response.Explanation.Text)
	}

	input := fixture.explainer.event
	if input.Indicator != "Consumer Price Index" || input.IndicatorType != "inflation" {
		t.Errorf("indicator = %q (%q), want the joined row", input.Indicator, input.IndicatorType)
	}
	if input.Previous != nil {
		t.Errorf("previous = %v, want nil for an unreleased reading", *input.Previous)
	}
	if input.Consensus == nil || *input.Consensus != 3.1 || input.Actual == nil || *input.Actual != 2.7 {
		t.Errorf("values = %v / %v, want 3.1 and 2.7", input.Consensus, input.Actual)
	}
	if len(input.Entities) != 1 || input.Entities[0] != "USD US Dollar" {
		t.Errorf("entities = %v, want the linked event entity", input.Entities)
	}
	if len(input.Pairs) != 1 || input.Pairs[0] != "EUR/USD" {
		t.Errorf("pairs = %v, want the affected pair symbol", input.Pairs)
	}
	wantRecent := []string{"Treasury yields slip after Fed", "Fed holds rates steady"}
	if len(input.RecentArticles) != 2 ||
		input.RecentArticles[0] != wantRecent[0] ||
		input.RecentArticles[1] != wantRecent[1] {
		t.Errorf("recent articles = %v, want the newest-first shared-entity coverage", input.RecentArticles)
	}
}

type explanationFixture struct {
	service      *explanation.Service
	explainer    *recordingExplainer
	articleOneID uuid.UUID
	eventID      uuid.UUID
}

func seedContext(t *testing.T, pool *pgxpool.Pool) *explanationFixture {
	t.Helper()
	ctx := t.Context()

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(ctx, sourcemodels.Source{
		ID:   uuid.New(),
		Name: "Explanation Source",
		Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	entityRepository := entityrepositories.NewEntityRepository(pool)
	usd, err := entityRepository.UpsertEntity(ctx, entitymodels.Entity{
		ID: uuid.New(), Code: "USD", Name: "US Dollar", Type: "currency",
	})
	if err != nil {
		t.Fatalf("create USD entity: %v", err)
	}
	eur, err := entityRepository.UpsertEntity(ctx, entitymodels.Entity{
		ID: uuid.New(), Code: "EUR", Name: "Euro", Type: "currency",
	})
	if err != nil {
		t.Fatalf("create EUR entity: %v", err)
	}
	if _, err := entityRepository.UpsertEntityPair(ctx, entitymodels.EntityPair{
		ID: uuid.New(), BaseEntityID: eur.ID, QuoteEntityID: usd.ID, Symbol: "EUR/USD",
	}); err != nil {
		t.Fatalf("create pair: %v", err)
	}
	if _, err := entityRepository.UpsertKnowledgeTerm(ctx, entitymodels.KnowledgeTerm{
		ID: uuid.New(), Name: "dot plot", Type: "topic", EntityID: usd.ID,
	}); err != nil {
		t.Fatalf("create knowledge term: %v", err)
	}
	indicator, err := entityRepository.UpsertIndicator(ctx, entitymodels.Indicator{
		ID: uuid.New(), Name: "Consumer Price Index", EntityID: usd.ID, Type: "inflation",
	})
	if err != nil {
		t.Fatalf("create indicator: %v", err)
	}

	articleRepository := articlerepositories.NewArticleRepository(pool, nil)
	publishedOne := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	publishedTwo := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	if _, err := articleRepository.PersistArticles(ctx, []articlemodels.PersistEntry{
		{
			SourceID: source.ID, Title: "Fed holds rates steady",
			Content: "The Federal Reserve left rates unchanged.",
			URL:     "https://example.com/fed-holds", PublishedAt: &publishedOne,
			EntityIDs: []uuid.UUID{usd.ID, eur.ID},
		},
		{
			SourceID: source.ID, Title: "Treasury yields slip after Fed",
			Content: "Benchmark yields fell.",
			URL:     "https://example.com/yields-slip", PublishedAt: &publishedTwo,
			EntityIDs: []uuid.UUID{usd.ID, eur.ID},
		},
	}); err != nil {
		t.Fatalf("persist articles: %v", err)
	}
	articleIDs := articleIDsByTitle(t, pool)

	enrichmentRepository := enrichmentrepositories.NewOutboxRepository(pool, nil)
	if _, err := enrichmentRepository.StoreEnrichment(ctx, articleIDs["Fed holds rates steady"],
		"gemini-3.1-flash-lite",
		json.RawMessage(`{"entities":["USD"],"topics":["monetary_policy"],"concepts":["rate decision"]}`),
	); err != nil {
		t.Fatalf("store enrichment: %v", err)
	}

	clusterRepository := clusteringrepositories.NewRepository(pool, nil)
	clusterID := uuid.New()
	if err := clusterRepository.CreateStoryCluster(ctx, clusterID, "Fed policy outlook"); err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	for _, id := range []uuid.UUID{articleIDs["Fed holds rates steady"], articleIDs["Treasury yields slip after Fed"]} {
		if _, err := clusterRepository.LinkArticleToCluster(ctx, id, clusterID); err != nil {
			t.Fatalf("link article to cluster: %v", err)
		}
	}

	eventRepository := calendarrepositories.NewEventRepository(pool, nil)
	consensus, actual := 3.1, 2.7
	if _, err := eventRepository.PersistEvents(ctx, []calendarmodels.PersistEntry{{
		SourceID:    source.ID,
		IndicatorID: indicator.ID,
		EntityID:    usd.ID,
		ScheduledAt: time.Date(2026, 10, 3, 12, 30, 0, 0, time.UTC),
		Consensus:   &consensus,
		Actual:      &actual,
	}}); err != nil {
		t.Fatalf("persist calendar event: %v", err)
	}
	eventID := singleEventID(t, pool)

	cacheServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(cacheServer.Close)

	explainer := &recordingExplainer{}
	service := explanation.NewService(
		articleRepository,
		enrichmentRepository,
		entityRepository,
		eventRepository,
		clusterRepository,
		clustering.NewService(clusterRepository, nil, articleRepository),
		explainer,
		cache.NewJSONStoreWithTTL(redis.NewClient(&redis.Options{Addr: cacheServer.Addr()}), explanation.CacheTTL),
	)
	return &explanationFixture{
		service:      service,
		explainer:    explainer,
		articleOneID: articleIDs["Fed holds rates steady"],
		eventID:      eventID,
	}
}

func articleIDsByTitle(t *testing.T, pool *pgxpool.Pool) map[string]uuid.UUID {
	t.Helper()
	rows, err := pool.Query(t.Context(), `SELECT id, title FROM articles`)
	if err != nil {
		t.Fatalf("list articles: %v", err)
	}
	defer rows.Close()

	found := make(map[string]uuid.UUID)
	for rows.Next() {
		var id uuid.UUID
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			t.Fatalf("scan article: %v", err)
		}
		found[title] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate articles: %v", err)
	}
	return found
}

func singleEventID(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(t.Context(), `SELECT id FROM calendar_events`).Scan(&id); err != nil {
		t.Fatalf("read calendar event: %v", err)
	}
	return id
}
