package explanation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
	articlemodels "github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	calendarmodels "github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	clusteringmodels "github.com/Rahmannugar/macro-terminal/server/internal/clustering/models"
	enrichmentmodels "github.com/Rahmannugar/macro-terminal/server/internal/enrichment/models"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type fakeArticles struct {
	articles  map[uuid.UUID]articlemodels.StoredArticle
	recent    []articlemodels.StoredArticle
	lookupErr error
}

func (fakes *fakeArticles) GetArticlesByIDs(_ context.Context, ids []uuid.UUID) ([]articlemodels.StoredArticle, error) {
	if fakes.lookupErr != nil {
		return nil, fakes.lookupErr
	}
	found := make([]articlemodels.StoredArticle, 0, len(ids))
	for _, id := range ids {
		if article, ok := fakes.articles[id]; ok {
			found = append(found, article)
		}
	}
	return found, nil
}

func (fakes *fakeArticles) RecentArticlesByEntities(_ context.Context, _ []uuid.UUID, _ int) ([]articlemodels.StoredArticle, error) {
	return fakes.recent, nil
}

type fakeEnrichment struct {
	stored enrichmentmodels.StoredEnrichment
	ok     bool
}

func (fakes *fakeEnrichment) ArticleEnrichment(_ context.Context, _ uuid.UUID) (enrichmentmodels.StoredEnrichment, bool, error) {
	return fakes.stored, fakes.ok, nil
}

type fakeGraph struct {
	articleEntities  []entitymodels.Entity
	eventEntities    []entitymodels.Entity
	pairsByEntity    map[uuid.UUID][]entitymodels.EntityPair
	terms            []entitymodels.KnowledgeTerm
	entityLookupFail bool
}

func (fakes *fakeGraph) EntitiesForArticle(_ context.Context, _ uuid.UUID) ([]entitymodels.Entity, error) {
	if fakes.entityLookupFail {
		return nil, errors.New("database down")
	}
	return fakes.articleEntities, nil
}

func (fakes *fakeGraph) EntitiesForCalendarEvent(_ context.Context, _ uuid.UUID) ([]entitymodels.Entity, error) {
	return fakes.eventEntities, nil
}

func (fakes *fakeGraph) EntityPairsContainingEntity(_ context.Context, entityID uuid.UUID) ([]entitymodels.EntityPair, error) {
	return fakes.pairsByEntity[entityID], nil
}

func (fakes *fakeGraph) KnowledgeTermsForEntities(_ context.Context, _ []uuid.UUID) ([]entitymodels.KnowledgeTerm, error) {
	return fakes.terms, nil
}

type fakeEvents struct {
	event calendarmodels.EventContext
	ok    bool
}

func (fakes *fakeEvents) CalendarEvent(_ context.Context, _ uuid.UUID) (calendarmodels.EventContext, bool, error) {
	return fakes.event, fakes.ok, nil
}

type fakeClusters struct {
	cluster clusteringmodels.StoryCluster
	ok      bool
}

func (fakes *fakeClusters) StoryClusterForArticle(_ context.Context, _ uuid.UUID) (clusteringmodels.StoryCluster, bool, error) {
	return fakes.cluster, fakes.ok, nil
}

type fakeRelated struct {
	articles []articlemodels.StoredArticle
	err      error
}

func (fakes *fakeRelated) Related(_ context.Context, _ uuid.UUID, _ int) ([]articlemodels.StoredArticle, error) {
	return fakes.articles, fakes.err
}

type fakeExplainer struct {
	articleCalls int
	eventCalls   int
	article      ai.ExplainArticleInput
	event        ai.ExplainEventInput
	err          error
}

func (fakes *fakeExplainer) ExplainArticle(_ context.Context, input ai.ExplainArticleInput) (string, error) {
	fakes.articleCalls++
	fakes.article = input
	if fakes.err != nil {
		return "", fakes.err
	}
	return "article explanation", nil
}

func (fakes *fakeExplainer) ExplainEvent(_ context.Context, input ai.ExplainEventInput) (string, error) {
	fakes.eventCalls++
	fakes.event = input
	if fakes.err != nil {
		return "", fakes.err
	}
	return "event explanation", nil
}

type fixture struct {
	service   *Service
	explainer *fakeExplainer
	articleID uuid.UUID
	eventID   uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	articleID := uuid.New()
	eventID := uuid.New()
	usd := entitymodels.Entity{ID: uuid.New(), Code: "USD", Name: "US Dollar", Type: "currency"}
	eur := entitymodels.Entity{ID: uuid.New(), Code: "EUR", Name: "Euro", Type: "currency"}
	published := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	relatedID := uuid.New()
	consensus := 3.1
	actual := 2.7

	explainer := &fakeExplainer{}
	service := NewService(
		&fakeArticles{
			articles: map[uuid.UUID]articlemodels.StoredArticle{
				articleID: {
					ID:          articleID,
					SourceID:    uuid.New(),
					SourceName:  "Reuters",
					Title:       "Fed holds rates steady",
					Content:     "The Federal Reserve left rates unchanged.",
					URL:         "https://www.reuters.com/markets/us/",
					PublishedAt: &published,
				},
				relatedID: {ID: relatedID, SourceName: "Bloomberg", Title: "Treasury yields slip after Fed"},
			},
			recent: []articlemodels.StoredArticle{
				{ID: relatedID, SourceName: "Bloomberg", Title: "Dollar steadies ahead of CPI"},
			},
		},
		&fakeEnrichment{
			stored: enrichmentmodels.StoredEnrichment{
				ID:        uuid.New(),
				ArticleID: articleID,
				Model:     "gemini-3.1-flash-lite",
				Result: json.RawMessage(
					`{"entities":["USD"],"topics":["monetary_policy"],"concepts":["rate decision"]}`,
				),
			},
			ok: true,
		},
		&fakeGraph{
			articleEntities: []entitymodels.Entity{usd, eur},
			eventEntities:   []entitymodels.Entity{usd},
			pairsByEntity: map[uuid.UUID][]entitymodels.EntityPair{
				usd.ID: {{ID: uuid.New(), Symbol: "EUR/USD"}},
				eur.ID: {{ID: uuid.New(), Symbol: "EUR/USD"}, {ID: uuid.New(), Symbol: "GBP/EUR"}},
			},
			terms: []entitymodels.KnowledgeTerm{{Name: "dot plot", Type: "topic", EntityID: usd.ID}},
		},
		&fakeEvents{
			event: calendarmodels.EventContext{
				StoredEvent: calendarmodels.StoredEvent{
					ID:          eventID,
					SourceID:    uuid.New(),
					IndicatorID: uuid.New(),
					ScheduledAt: time.Date(2026, 10, 3, 12, 30, 0, 0, time.UTC),
					Consensus:   &consensus,
					Actual:      &actual,
				},
				IndicatorName: "Consumer Price Index",
				IndicatorType: "inflation",
			},
			ok: true,
		},
		&fakeClusters{cluster: clusteringmodels.StoryCluster{ID: uuid.New(), Title: "Fed policy outlook"}, ok: true},
		&fakeRelated{articles: []articlemodels.StoredArticle{
			{ID: relatedID, SourceName: "Bloomberg", Title: "Treasury yields slip after Fed"},
		}},
		explainer,
		nil,
	)
	return &fixture{service: service, explainer: explainer, articleID: articleID, eventID: eventID}
}

func TestExplainArticleAssemblesContextAndReturnsText(t *testing.T) {
	fixture := newFixture(t)

	response, err := fixture.service.ExplainArticle(t.Context(), fixture.articleID)
	if err != nil {
		t.Fatalf("ExplainArticle: %v", err)
	}
	if response.Explanation.Text != "article explanation" {
		t.Fatalf("text = %q, want the generated answer", response.Explanation.Text)
	}
	if fixture.explainer.articleCalls != 1 {
		t.Fatalf("explainer calls = %d, want 1", fixture.explainer.articleCalls)
	}

	input := fixture.explainer.article
	if input.Title != "Fed holds rates steady" || input.SourceName != "Reuters" {
		t.Errorf("article context = %+v, want the stored article", input)
	}
	if len(input.Enrichment.Entities) != 1 || input.Enrichment.Entities[0] != "USD" ||
		len(input.Enrichment.Topics) != 1 || input.Enrichment.Topics[0] != "monetary_policy" {
		t.Errorf("enrichment = %+v, want the stored result", input.Enrichment)
	}
	if len(input.Entities) != 2 || input.Entities[0] != "USD US Dollar" || input.Entities[1] != "EUR Euro" {
		t.Errorf("entities = %v, want both linked entities labeled", input.Entities)
	}
	if len(input.Pairs) != 2 || input.Pairs[0] != "EUR/USD" || input.Pairs[1] != "GBP/EUR" {
		t.Errorf("pairs = %v, want the deduplicated symbols in entity order", input.Pairs)
	}
	if input.ClusterTitle != "Fed policy outlook" {
		t.Errorf("cluster title = %q, want the story cluster", input.ClusterTitle)
	}
	if len(input.RelatedTitles) != 1 || input.RelatedTitles[0] != "Treasury yields slip after Fed" {
		t.Errorf("related titles = %v, want the cluster mate headline", input.RelatedTitles)
	}
	if len(input.KnowledgeTerms) != 1 || input.KnowledgeTerms[0] != "dot plot (topic)" {
		t.Errorf("knowledge terms = %v, want the labeled entity term", input.KnowledgeTerms)
	}
}

func TestExplainArticleReportsMissingArticle(t *testing.T) {
	fixture := newFixture(t)

	_, err := fixture.service.ExplainArticle(t.Context(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if fixture.explainer.articleCalls != 0 {
		t.Fatalf("explainer calls = %d, want 0", fixture.explainer.articleCalls)
	}
}

func TestExplainArticleReportsStorageFailure(t *testing.T) {
	fixture := newFixture(t)
	fixture.service.articles.(*fakeArticles).lookupErr = context.DeadlineExceeded

	if _, err := fixture.service.ExplainArticle(t.Context(), fixture.articleID); err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want a plain storage failure", err)
	}
}

func TestExplainArticleDegradesWhenRelatedLookupFails(t *testing.T) {
	fixture := newFixture(t)
	fixture.service.related.(*fakeRelated).err = errors.New("vector search unreachable")

	response, err := fixture.service.ExplainArticle(t.Context(), fixture.articleID)
	if err != nil {
		t.Fatalf("ExplainArticle: %v", err)
	}
	if response.Explanation.Text != "article explanation" {
		t.Fatalf("text = %q, want the generated answer", response.Explanation.Text)
	}
	if len(fixture.explainer.article.RelatedTitles) != 0 {
		t.Errorf("related titles = %v, want none when the lookup fails", fixture.explainer.article.RelatedTitles)
	}
}

func TestExplainArticleReportsAIOutage(t *testing.T) {
	fixture := newFixture(t)
	fixture.explainer.err = ai.ErrMissingKey

	_, err := fixture.service.ExplainArticle(t.Context(), fixture.articleID)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestExplainEventAssemblesContextAndReturnsText(t *testing.T) {
	fixture := newFixture(t)

	response, err := fixture.service.ExplainEvent(t.Context(), fixture.eventID)
	if err != nil {
		t.Fatalf("ExplainEvent: %v", err)
	}
	if response.Explanation.Text != "event explanation" {
		t.Fatalf("text = %q, want the generated answer", response.Explanation.Text)
	}

	input := fixture.explainer.event
	if input.Indicator != "Consumer Price Index" || input.IndicatorType != "inflation" {
		t.Errorf("indicator = %q (%q), want the joined indicator", input.Indicator, input.IndicatorType)
	}
	if input.Consensus == nil || *input.Consensus != 3.1 || input.Actual == nil || *input.Actual != 2.7 {
		t.Errorf("values = consensus %v actual %v, want 3.1 and 2.7", input.Consensus, input.Actual)
	}
	if len(input.Entities) != 1 || input.Entities[0] != "USD US Dollar" {
		t.Errorf("entities = %v, want the linked entity", input.Entities)
	}
	if len(input.Pairs) != 1 || input.Pairs[0] != "EUR/USD" {
		t.Errorf("pairs = %v, want the pair symbol", input.Pairs)
	}
	if len(input.RecentArticles) != 1 || input.RecentArticles[0] != "Dollar steadies ahead of CPI" {
		t.Errorf("recent articles = %v, want the context headline", input.RecentArticles)
	}
}

func TestExplainEventReportsMissingEvent(t *testing.T) {
	fixture := newFixture(t)
	fixture.service.events.(*fakeEvents).ok = false

	_, err := fixture.service.ExplainEvent(t.Context(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if fixture.explainer.eventCalls != 0 {
		t.Fatalf("explainer calls = %d, want 0", fixture.explainer.eventCalls)
	}
}

func TestExplainArticleServesCachedAnswerAndRegeneratesAfterExpiry(t *testing.T) {
	fixture := newFixture(t)
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(server.Close)
	store := cache.NewJSONStoreWithTTL(redis.NewClient(&redis.Options{Addr: server.Addr()}), CacheTTL)
	fixture.service.store = store

	first, err := fixture.service.ExplainArticle(t.Context(), fixture.articleID)
	if err != nil {
		t.Fatalf("first ExplainArticle: %v", err)
	}
	second, err := fixture.service.ExplainArticle(t.Context(), fixture.articleID)
	if err != nil {
		t.Fatalf("cached ExplainArticle: %v", err)
	}
	if first != second {
		t.Fatalf("cached response = %+v, want the stored answer %+v", second, first)
	}
	if fixture.explainer.articleCalls != 1 {
		t.Fatalf("explainer calls = %d, want 1 while the answer is cached", fixture.explainer.articleCalls)
	}

	server.FastForward(CacheTTL + time.Second)
	if _, err := fixture.service.ExplainArticle(t.Context(), fixture.articleID); err != nil {
		t.Fatalf("regenerated ExplainArticle: %v", err)
	}
	if fixture.explainer.articleCalls != 2 {
		t.Fatalf("explainer calls = %d, want 2 after the cache expired", fixture.explainer.articleCalls)
	}
}

func TestExplainEventIsCachedSeparatelyFromArticles(t *testing.T) {
	fixture := newFixture(t)
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(server.Close)
	fixture.service.store = cache.NewJSONStoreWithTTL(
		redis.NewClient(&redis.Options{Addr: server.Addr()}),
		CacheTTL,
	)

	if _, err := fixture.service.ExplainArticle(t.Context(), fixture.articleID); err != nil {
		t.Fatalf("ExplainArticle: %v", err)
	}
	if _, err := fixture.service.ExplainEvent(t.Context(), fixture.eventID); err != nil {
		t.Fatalf("ExplainEvent: %v", err)
	}
	if fixture.explainer.articleCalls != 1 || fixture.explainer.eventCalls != 1 {
		t.Fatalf("calls = article %d event %d, want one each before caching", fixture.explainer.articleCalls, fixture.explainer.eventCalls)
	}
	if _, err := fixture.service.ExplainEvent(t.Context(), fixture.eventID); err != nil {
		t.Fatalf("cached ExplainEvent: %v", err)
	}
	if fixture.explainer.eventCalls != 1 {
		t.Fatalf("event calls = %d, want the event answer served from cache", fixture.explainer.eventCalls)
	}
}
