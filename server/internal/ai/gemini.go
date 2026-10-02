package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	geminiBaseURL   = "https://generativelanguage.googleapis.com"
	requestTimeout  = 20 * time.Second
	maxOutputTokens = 1024
)

type geminiClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

func newGeminiClient(httpClient *http.Client, apiKey, model string) *geminiClient {
	return &geminiClient{
		httpClient: httpClient,
		baseURL:    geminiBaseURL,
		apiKey:     strings.TrimSpace(apiKey),
		model:      strings.TrimSpace(model),
	}
}

func (client *geminiClient) Enrich(ctx context.Context, input EnrichInput) (Enrichment, error) {
	if client.apiKey == "" {
		return Enrichment{}, ErrMissingKey
	}
	if client.model == "" {
		return Enrichment{}, errors.New("enrichment model is not configured")
	}

	requestBody, err := json.Marshal(client.buildRequest(input))
	if err != nil {
		return Enrichment{}, fmt.Errorf("encode provider request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", client.baseURL, client.model)
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return Enrichment{}, fmt.Errorf("build provider request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-goog-api-key", client.apiKey)

	response, err := client.httpClient.Do(request)
	if err != nil {
		return Enrichment{}, fmt.Errorf("call AI provider: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Enrichment{}, fmt.Errorf("read provider response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return Enrichment{}, fmt.Errorf("provider returned %d: %s", response.StatusCode, errorMessage(body))
	}

	var decoded generateResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return Enrichment{}, fmt.Errorf("decode provider response: %w", err)
	}
	text := decoded.firstText()
	if text == "" {
		return Enrichment{}, errors.New("provider response carried no text part")
	}
	var enrichment Enrichment
	if err := json.Unmarshal([]byte(text), &enrichment); err != nil {
		return Enrichment{}, fmt.Errorf("decode enrichment JSON: %w", err)
	}
	return enrichment, nil
}

func (client *geminiClient) buildRequest(input EnrichInput) generateRequest {
	prompt := fmt.Sprintf(
		"Classify the following financial news article.\n\nTitle: %s\n\nContent: %s\n\n"+
			"Return entities only from this list of known asset and institution codes: %s\n"+
			"List the entity codes the article is actually about, its topics, and its key concepts.",
		input.Title, input.Content, strings.Join(input.EntityCodes, ", "),
	)
	return generateRequest{
		Contents: []content{{Parts: []part{{Text: prompt}}}},
		GenerationConfig: generationConfig{
			ResponseMimeType: "application/json",
			ResponseSchema:   enrichmentSchema(),
			MaxOutputTokens:  maxOutputTokens,
		},
	}
}

func enrichmentSchema() schema {
	stringArray := schema{Type: "array", Items: &schema{Type: "string"}}
	return schema{
		Type: "object",
		Properties: map[string]schema{
			"entities": stringArray,
			"topics":   stringArray,
			"concepts": stringArray,
		},
		Required: []string{"entities", "topics", "concepts"},
	}
}

type generateRequest struct {
	Contents         []content        `json:"contents"`
	GenerationConfig generationConfig `json:"generationConfig"`
}

type content struct {
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type generationConfig struct {
	ResponseMimeType string `json:"responseMimeType"`
	ResponseSchema   schema `json:"responseSchema"`
	MaxOutputTokens  int    `json:"maxOutputTokens"`
}

type schema struct {
	Type       string            `json:"type"`
	Properties map[string]schema `json:"properties,omitempty"`
	Required   []string          `json:"required,omitempty"`
	Items      *schema           `json:"items,omitempty"`
}

type generateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []part `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func (response generateResponse) firstText() string {
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				return part.Text
			}
		}
	}
	return ""
}

func errorMessage(body []byte) string {
	var providerError struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &providerError); err == nil && providerError.Error.Message != "" {
		return providerError.Error.Message
	}
	excerpt := strings.TrimSpace(string(body))
	if len(excerpt) > 200 {
		excerpt = excerpt[:200]
	}
	return excerpt
}
