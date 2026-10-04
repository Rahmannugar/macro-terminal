package ai

import (
	"context"
	"errors"
	"net/http"
	"time"
)

var ErrMissingKey = errors.New("no API key configured")

type Enrichment struct {
	Entities []string `json:"entities"`
	Topics   []string `json:"topics"`
	Concepts []string `json:"concepts"`
}

type EnrichInput struct {
	Title       string
	Content     string
	EntityCodes []string
}

type Enricher interface {
	Enrich(ctx context.Context, input EnrichInput) (Enrichment, error)
}

type ExplainArticleInput struct {
	Title          string
	Content        string
	URL            string
	SourceName     string
	PublishedAt    *time.Time
	Enrichment     Enrichment
	Entities       []string
	Pairs          []string
	ClusterTitle   string
	RelatedTitles  []string
	KnowledgeTerms []string
}

type ExplainEventInput struct {
	Indicator      string
	IndicatorType  string
	ScheduledAt    time.Time
	ReleasedAt     *time.Time
	Previous       *float64
	Consensus      *float64
	Actual         *float64
	Entities       []string
	Pairs          []string
	RecentArticles []string
}

type Explainer interface {
	ExplainArticle(ctx context.Context, input ExplainArticleInput) (string, error)
	ExplainEvent(ctx context.Context, input ExplainEventInput) (string, error)
}

func NewClient(httpClient *http.Client, apiKey, model string) Enricher {
	return newGeminiClient(httpClient, apiKey, model)
}

func NewExplainer(httpClient *http.Client, apiKey, model string) Explainer {
	return newGeminiClient(httpClient, apiKey, model)
}
