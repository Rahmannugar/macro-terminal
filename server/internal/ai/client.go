package ai

import (
	"context"
	"errors"
	"net/http"
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

func NewClient(httpClient *http.Client, apiKey, model string) Enricher {
	return newGeminiClient(httpClient, apiKey, model)
}
