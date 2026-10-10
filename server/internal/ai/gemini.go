package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	geminiBaseURL          = "https://generativelanguage.googleapis.com"
	requestTimeout         = 20 * time.Second
	explainTimeout         = 30 * time.Second
	maxOutputTokens        = 1024
	explainMaxOutputTokens = 4096
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

	decoded, err := client.generate(ctx, client.buildRequest(input), requestTimeout)
	if err != nil {
		return Enrichment{}, err
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

func (client *geminiClient) ExplainArticle(ctx context.Context, input ExplainArticleInput) (string, error) {
	return client.explain(ctx, articlePrompt(input))
}

func (client *geminiClient) ExplainEvent(ctx context.Context, input ExplainEventInput) (string, error) {
	return client.explain(ctx, eventPrompt(input))
}

func (client *geminiClient) explain(ctx context.Context, prompt string) (string, error) {
	if client.apiKey == "" {
		return "", ErrMissingKey
	}
	if client.model == "" {
		return "", errors.New("explanation model is not configured")
	}
	decoded, err := client.generate(ctx, generateRequest{
		Contents:         []content{{Parts: []part{{Text: prompt}}}},
		GenerationConfig: generationConfig{MaxOutputTokens: explainMaxOutputTokens},
	}, explainTimeout)
	if err != nil {
		return "", err
	}
	text := decoded.firstText()
	if text == "" {
		return "", errors.New("provider response carried no text part")
	}
	return text, nil
}

func (client *geminiClient) generate(ctx context.Context, requestBody generateRequest, timeout time.Duration) (generateResponse, error) {
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return generateResponse{}, fmt.Errorf("encode provider request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", client.baseURL, client.model)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return generateResponse{}, fmt.Errorf("build provider request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-goog-api-key", client.apiKey)

	response, err := client.httpClient.Do(request)
	if err != nil {
		return generateResponse{}, fmt.Errorf("call AI provider: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return generateResponse{}, fmt.Errorf("read provider response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return generateResponse{}, fmt.Errorf("provider returned %d: %s", response.StatusCode, errorMessage(body))
	}

	var decoded generateResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return generateResponse{}, fmt.Errorf("decode provider response: %w", err)
	}
	return decoded, nil
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

func enrichmentSchema() *schema {
	stringArray := schema{Type: "array", Items: &schema{Type: "string"}}
	return &schema{
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
	ResponseMimeType string  `json:"responseMimeType,omitempty"`
	ResponseSchema   *schema `json:"responseSchema,omitempty"`
	MaxOutputTokens  int     `json:"maxOutputTokens,omitempty"`
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

func articlePrompt(input ExplainArticleInput) string {
	var builder strings.Builder
	builder.WriteString("Explain this financial news article for a macro trading terminal reader.\n\n")
	fmt.Fprintf(&builder, "Title: %s\n", input.Title)
	if input.SourceName != "" {
		fmt.Fprintf(&builder, "Source: %s\n", input.SourceName)
	}
	if input.PublishedAt != nil {
		fmt.Fprintf(&builder, "Published: %s\n", input.PublishedAt.UTC().Format(time.RFC3339))
	}
	if input.URL != "" {
		fmt.Fprintf(&builder, "URL: %s\n", input.URL)
	}
	fmt.Fprintf(&builder, "\nArticle content:\n%s\n", input.Content)

	cluster := ""
	if input.ClusterTitle != "" {
		cluster = "Story cluster: " + input.ClusterTitle
	}
	for _, line := range []string{
		listLine("Stored enrichment entities", input.Enrichment.Entities),
		listLine("Stored enrichment topics", input.Enrichment.Topics),
		listLine("Stored enrichment concepts", input.Enrichment.Concepts),
		listLine("Graph entities", input.Entities),
		listLine("Affected asset pairs", input.Pairs),
		cluster,
		listLine("Related coverage headlines", input.RelatedTitles),
		listLine("Knowledge terms", input.KnowledgeTerms),
	} {
		if line != "" {
			builder.WriteString(line)
			builder.WriteByte('\n')
		}
	}
	builder.WriteString("\nWrite a 2 to 4 paragraph explanation of what the article says and why it matters " +
		"for the affected assets. Use only the article and the context above; do not invent facts, figures, " +
		"or dates. Plain text, no markdown headings.")
	return builder.String()
}

func eventPrompt(input ExplainEventInput) string {
	var builder strings.Builder
	builder.WriteString("Explain this economic calendar release for a macro trading terminal reader.\n\n")
	fmt.Fprintf(&builder, "Indicator: %s", input.Indicator)
	if input.IndicatorType != "" {
		fmt.Fprintf(&builder, " (%s)", input.IndicatorType)
	}
	builder.WriteByte('\n')
	fmt.Fprintf(&builder, "Scheduled (UTC): %s\n", input.ScheduledAt.UTC().Format(time.RFC3339))
	if input.ReleasedAt != nil {
		fmt.Fprintf(&builder, "Released (UTC): %s\n", input.ReleasedAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(&builder, "Previous: %s\n", numberOrNA(input.Previous))
	fmt.Fprintf(&builder, "Consensus: %s\n", numberOrNA(input.Consensus))
	fmt.Fprintf(&builder, "Actual: %s\n", numberOrNA(input.Actual))
	for _, line := range []string{
		listLine("Linked entities", input.Entities),
		listLine("Affected asset pairs", input.Pairs),
		listLine("Recent related headlines", input.RecentArticles),
	} {
		if line != "" {
			builder.WriteString(line)
			builder.WriteByte('\n')
		}
	}
	builder.WriteString("\nWrite a 2 to 4 paragraph explanation comparing the actual result with consensus and " +
		"the previous reading, and why markets watch this indicator. Use only the context above; do not invent " +
		"numbers. Quote times exactly as given, in UTC; never convert them to another time zone. Plain text, " +
		"no markdown headings.")
	return builder.String()
}

func listLine(label string, items []string) string {
	if len(items) == 0 {
		return ""
	}
	return label + ": " + strings.Join(items, ", ")
}

func numberOrNA(value *float64) string {
	if value == nil {
		return "n/a"
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}
