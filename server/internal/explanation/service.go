package explanation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
	articlemodels "github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	calendarmodels "github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	clusteringmodels "github.com/Rahmannugar/macro-terminal/server/internal/clustering/models"
	enrichmentmodels "github.com/Rahmannugar/macro-terminal/server/internal/enrichment/models"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/google/uuid"
)

// CacheTTL bounds how long a generated explanation may be reused before
// it is regenerated from current context.
const CacheTTL = 10 * time.Minute

const (
	relatedContextLimit = 5
	recentContextLimit  = 5
)

var (
	ErrNotFound    = errors.New("content not found")
	ErrUnavailable = errors.New("explanation is unavailable")
)

type ArticleReader interface {
	GetArticlesByIDs(ctx context.Context, ids []uuid.UUID) ([]articlemodels.StoredArticle, error)
	RecentArticlesByEntities(ctx context.Context, entityIDs []uuid.UUID, limit int) ([]articlemodels.StoredArticle, error)
}

type EnrichmentReader interface {
	ArticleEnrichment(ctx context.Context, articleID uuid.UUID) (enrichmentmodels.StoredEnrichment, bool, error)
}

type GraphReader interface {
	EntitiesForArticle(ctx context.Context, articleID uuid.UUID) ([]entitymodels.Entity, error)
	EntitiesForCalendarEvent(ctx context.Context, calendarEventID uuid.UUID) ([]entitymodels.Entity, error)
	EntityPairsContainingEntity(ctx context.Context, entityID uuid.UUID) ([]entitymodels.EntityPair, error)
	KnowledgeTermsForEntities(ctx context.Context, entityIDs []uuid.UUID) ([]entitymodels.KnowledgeTerm, error)
}

type EventReader interface {
	CalendarEvent(ctx context.Context, id uuid.UUID) (calendarmodels.EventContext, bool, error)
}

type ClusterReader interface {
	StoryClusterForArticle(ctx context.Context, articleID uuid.UUID) (clusteringmodels.StoryCluster, bool, error)
}

type RelatedReader interface {
	Related(ctx context.Context, articleID uuid.UUID, limit int) ([]articlemodels.StoredArticle, error)
}

type Explainer interface {
	ExplainArticle(ctx context.Context, input ai.ExplainArticleInput) (string, error)
	ExplainEvent(ctx context.Context, input ai.ExplainEventInput) (string, error)
}

type Service struct {
	articles   ArticleReader
	enrichment EnrichmentReader
	graph      GraphReader
	events     EventReader
	clusters   ClusterReader
	related    RelatedReader
	explainer  Explainer
	store      *cache.JSONStore
}

func NewService(
	articles ArticleReader,
	enrichment EnrichmentReader,
	graph GraphReader,
	events EventReader,
	clusters ClusterReader,
	related RelatedReader,
	explainer Explainer,
	store *cache.JSONStore,
) *Service {
	return &Service{
		articles:   articles,
		enrichment: enrichment,
		graph:      graph,
		events:     events,
		clusters:   clusters,
		related:    related,
		explainer:  explainer,
		store:      store,
	}
}

func (service *Service) ExplainArticle(ctx context.Context, id uuid.UUID) (explanationResponse, error) {
	key := cache.ExplainArticleKey(id)
	if response, ok := service.cachedResponse(ctx, key); ok {
		return response, nil
	}

	found, err := service.articles.GetArticlesByIDs(ctx, []uuid.UUID{id})
	if err != nil {
		return explanationResponse{}, fmt.Errorf("load article: %w", err)
	}
	if len(found) == 0 {
		return explanationResponse{}, ErrNotFound
	}
	article := found[0]

	entityIDs, entityNames, err := service.articleEntities(ctx, id)
	if err != nil {
		return explanationResponse{}, err
	}
	pairs, err := service.pairSymbols(ctx, entityIDs)
	if err != nil {
		return explanationResponse{}, err
	}
	terms, err := service.graph.KnowledgeTermsForEntities(ctx, entityIDs)
	if err != nil {
		return explanationResponse{}, fmt.Errorf("load knowledge terms: %w", err)
	}

	structured, err := service.storedEnrichment(ctx, id)
	if err != nil {
		return explanationResponse{}, err
	}

	cluster, _, err := service.clusters.StoryClusterForArticle(ctx, id)
	if err != nil {
		return explanationResponse{}, fmt.Errorf("load story cluster: %w", err)
	}

	related, err := service.related.Related(ctx, id, relatedContextLimit)
	if err != nil {
		slog.Default().WarnContext(ctx, "related articles unavailable for explanation", "error", err)
		related = nil
	}

	text, err := service.explainer.ExplainArticle(ctx, ai.ExplainArticleInput{
		Title:          article.Title,
		Content:        article.Content,
		URL:            article.URL,
		SourceName:     article.SourceName,
		PublishedAt:    article.PublishedAt,
		Enrichment:     structured,
		Entities:       entityNames,
		Pairs:          pairs,
		ClusterTitle:   cluster.Title,
		RelatedTitles:  articleTitles(related),
		KnowledgeTerms: termLabels(terms),
	})
	if err != nil {
		return explanationResponse{}, fmt.Errorf("%w: explain article: %w", ErrUnavailable, err)
	}
	return service.cacheResponse(ctx, key, text), nil
}

func (service *Service) ExplainEvent(ctx context.Context, id uuid.UUID) (explanationResponse, error) {
	key := cache.ExplainCalendarEventKey(id)
	if response, ok := service.cachedResponse(ctx, key); ok {
		return response, nil
	}

	event, ok, err := service.events.CalendarEvent(ctx, id)
	if err != nil {
		return explanationResponse{}, fmt.Errorf("load calendar event: %w", err)
	}
	if !ok {
		return explanationResponse{}, ErrNotFound
	}

	entityIDs, entityNames, err := service.eventEntities(ctx, id)
	if err != nil {
		return explanationResponse{}, err
	}
	pairs, err := service.pairSymbols(ctx, entityIDs)
	if err != nil {
		return explanationResponse{}, err
	}
	recent, err := service.articles.RecentArticlesByEntities(ctx, entityIDs, recentContextLimit)
	if err != nil {
		return explanationResponse{}, fmt.Errorf("load recent articles: %w", err)
	}

	text, err := service.explainer.ExplainEvent(ctx, ai.ExplainEventInput{
		Indicator:      event.Name,
		IndicatorType:  event.IndicatorType,
		ScheduledAt:    event.ScheduledAt,
		ReleasedAt:     event.ReleasedAt,
		Previous:       event.Previous,
		Consensus:      event.Consensus,
		Actual:         event.Actual,
		Entities:       entityNames,
		Pairs:          pairs,
		RecentArticles: articleTitles(recent),
	})
	if err != nil {
		return explanationResponse{}, fmt.Errorf("%w: explain event: %w", ErrUnavailable, err)
	}
	return service.cacheResponse(ctx, key, text), nil
}

func (service *Service) articleEntities(ctx context.Context, id uuid.UUID) ([]uuid.UUID, []string, error) {
	entities, err := service.graph.EntitiesForArticle(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("load article entities: %w", err)
	}
	ids, names := entityContext(entities)
	return ids, names, nil
}

func (service *Service) eventEntities(ctx context.Context, id uuid.UUID) ([]uuid.UUID, []string, error) {
	entities, err := service.graph.EntitiesForCalendarEvent(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("load calendar event entities: %w", err)
	}
	ids, names := entityContext(entities)
	return ids, names, nil
}

func entityContext(entities []entitymodels.Entity) ([]uuid.UUID, []string) {
	ids := make([]uuid.UUID, 0, len(entities))
	names := make([]string, 0, len(entities))
	for _, entity := range entities {
		ids = append(ids, entity.ID)
		names = append(names, entity.Code+" "+entity.Name)
	}
	return ids, names
}

func (service *Service) pairSymbols(ctx context.Context, entityIDs []uuid.UUID) ([]string, error) {
	seen := make(map[string]struct{})
	symbols := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		pairs, err := service.graph.EntityPairsContainingEntity(ctx, entityID)
		if err != nil {
			return nil, fmt.Errorf("load entity pairs: %w", err)
		}
		for _, pair := range pairs {
			if _, ok := seen[pair.Symbol]; ok {
				continue
			}
			seen[pair.Symbol] = struct{}{}
			symbols = append(symbols, pair.Symbol)
		}
	}
	return symbols, nil
}

func (service *Service) storedEnrichment(ctx context.Context, id uuid.UUID) (ai.Enrichment, error) {
	stored, ok, err := service.enrichment.ArticleEnrichment(ctx, id)
	if err != nil {
		return ai.Enrichment{}, fmt.Errorf("load article enrichment: %w", err)
	}
	if !ok {
		return ai.Enrichment{}, nil
	}
	var structured ai.Enrichment
	if err := json.Unmarshal(stored.Result, &structured); err != nil {
		return ai.Enrichment{}, fmt.Errorf("decode stored enrichment: %w", err)
	}
	return structured, nil
}

// cachedResponse treats every miss — absent, corrupt, or an unreachable
// Redis — the same way: regenerate.
func (service *Service) cachedResponse(ctx context.Context, key string) (explanationResponse, bool) {
	cached, err := service.store.GetMany(ctx, []string{key})
	if err != nil {
		return explanationResponse{}, false
	}
	raw, ok := cached[key]
	if !ok {
		return explanationResponse{}, false
	}
	var response explanationResponse
	if err := json.Unmarshal(raw, &response); err != nil || response.Explanation.Text == "" {
		return explanationResponse{}, false
	}
	return response, true
}

func (service *Service) cacheResponse(ctx context.Context, key, text string) explanationResponse {
	response := explanationResponse{Explanation: explanationJSON{Text: text}}
	_ = service.store.Set(ctx, key, response)
	return response
}

func articleTitles(articles []articlemodels.StoredArticle) []string {
	titles := make([]string, 0, len(articles))
	for _, article := range articles {
		titles = append(titles, article.Title)
	}
	return titles
}

func termLabels(terms []entitymodels.KnowledgeTerm) []string {
	labels := make([]string, 0, len(terms))
	for _, term := range terms {
		labels = append(labels, term.Name+" ("+term.Type+")")
	}
	return labels
}
