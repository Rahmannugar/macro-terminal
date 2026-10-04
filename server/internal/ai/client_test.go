package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEnrichSendsStructuredRequestAndParsesResult(t *testing.T) {
	var received generateRequest
	var apiKey string
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path = request.URL.Path
		apiKey = request.Header.Get("x-goog-api-key")
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"entities\":[\"USD\"],\"topics\":[\"monetary_policy\"],\"concepts\":[\"rate decision\"]}"}]}}]}`))
	}))
	defer server.Close()

	client := newGeminiClient(server.Client(), "test-key", "gemini-3.1-flash-lite")
	client.baseURL = server.URL
	got, err := client.Enrich(context.Background(), EnrichInput{
		Title:       "Fed holds rates steady",
		Content:     "The Federal Reserve kept rates unchanged.",
		EntityCodes: []string{"USD", "EUR", "GBP"},
	})
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	if path != "/v1beta/models/gemini-3.1-flash-lite:generateContent" {
		t.Errorf("path = %q, want the generateContent endpoint", path)
	}
	if apiKey != "test-key" {
		t.Errorf("API key header = %q, want test-key", apiKey)
	}
	if len(got.Entities) != 1 || got.Entities[0] != "USD" ||
		len(got.Topics) != 1 || got.Topics[0] != "monetary_policy" ||
		len(got.Concepts) != 1 || got.Concepts[0] != "rate decision" {
		t.Errorf("Enrichment = %+v, want the parsed result", got)
	}

	if received.GenerationConfig.ResponseMimeType != "application/json" {
		t.Errorf("response mime type = %q, want application/json", received.GenerationConfig.ResponseMimeType)
	}
	if received.GenerationConfig.ResponseSchema.Type != "object" ||
		len(received.GenerationConfig.ResponseSchema.Required) != 3 {
		t.Errorf("response schema = %+v, want an object requiring the three fields", received.GenerationConfig.ResponseSchema)
	}
	prompt := received.Contents[0].Parts[0].Text
	if !strings.Contains(prompt, "Fed holds rates steady") ||
		!strings.Contains(prompt, "USD, EUR, GBP") {
		t.Errorf("prompt missing article or entity universe: %q", prompt)
	}
}

func TestEnrichWithoutKeyFailsWithoutCallingProvider(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
	}))
	defer server.Close()

	client := newGeminiClient(server.Client(), "", "gemini-3.1-flash-lite")
	client.baseURL = server.URL
	if _, err := client.Enrich(context.Background(), EnrichInput{Title: "x"}); !errors.Is(err, ErrMissingKey) {
		t.Fatalf("error = %v, want ErrMissingKey", err)
	}
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestEnrichSurfacesProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"code":429,"message":"quota exceeded"}}`))
	}))
	defer server.Close()

	client := newGeminiClient(server.Client(), "test-key", "gemini-3.1-flash-lite")
	client.baseURL = server.URL
	_, err := client.Enrich(context.Background(), EnrichInput{Title: "x"})
	if err == nil {
		t.Fatal("Enrich succeeded, want a provider error")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("error = %v, want the status and provider message", err)
	}
}

func TestEnrichRejectsUnusableResponses(t *testing.T) {
	for name, body := range map[string]string{
		"no candidates": `{}`,
		"garbage text":  `{"candidates":[{"content":{"parts":[{"text":"not json"}]}}]}`,
		"empty text":    `{"candidates":[{"content":{"parts":[{"text":""}]}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, _ = writer.Write([]byte(body))
			}))
			defer server.Close()

			client := newGeminiClient(server.Client(), "test-key", "gemini-3.1-flash-lite")
			client.baseURL = server.URL
			if _, err := client.Enrich(context.Background(), EnrichInput{Title: "x"}); err == nil {
				t.Fatal("Enrich succeeded, want an error")
			}
		})
	}
}

func TestNewClientBuildsAGeminiClient(t *testing.T) {
	enricher := NewClient(http.DefaultClient, "key", "gemini-3.1-flash-lite")
	if _, ok := enricher.(*geminiClient); !ok {
		t.Fatalf("NewClient built %T, want the gemini client", enricher)
	}
}

func TestExplainArticleSendsPromptAndReturnsText(t *testing.T) {
	var received generateRequest
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path = request.URL.Path
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"Rates stayed on hold while inflation eases slowly."}]}}]}`))
	}))
	defer server.Close()

	client := newGeminiClient(server.Client(), "test-key", "gemini-3.5-flash-lite")
	client.baseURL = server.URL
	got, err := client.ExplainArticle(context.Background(), ExplainArticleInput{
		Title:        "Fed holds rates steady",
		Content:      "The Federal Reserve kept rates unchanged.",
		Pairs:        []string{"EUR/USD"},
		Entities:     []string{"USD US Dollar"},
		ClusterTitle: "Fed policy outlook",
	})
	if err != nil {
		t.Fatalf("ExplainArticle: %v", err)
	}
	if got != "Rates stayed on hold while inflation eases slowly." {
		t.Errorf("text = %q, want the provider answer", got)
	}
	if path != "/v1beta/models/gemini-3.5-flash-lite:generateContent" {
		t.Errorf("path = %q, want the explanation model endpoint", path)
	}
	if received.GenerationConfig.ResponseMimeType != "" || received.GenerationConfig.ResponseSchema != nil {
		t.Errorf("generation config = %+v, want plain prose output", received.GenerationConfig)
	}
	if received.GenerationConfig.MaxOutputTokens != explainMaxOutputTokens {
		t.Errorf("max output tokens = %d, want %d", received.GenerationConfig.MaxOutputTokens, explainMaxOutputTokens)
	}
	prompt := received.Contents[0].Parts[0].Text
	for _, wanted := range []string{"Fed holds rates steady", "EUR/USD", "Fed policy outlook", "USD US Dollar"} {
		if !strings.Contains(prompt, wanted) {
			t.Errorf("prompt missing %q: %q", wanted, prompt)
		}
	}
}

func TestExplainEventSendsValuesAndReturnsText(t *testing.T) {
	var received generateRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_, _ = writer.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"Inflation cooled below consensus."}]}}]}`))
	}))
	defer server.Close()

	client := newGeminiClient(server.Client(), "test-key", "gemini-3.5-flash-lite")
	client.baseURL = server.URL
	consensus := 3.1
	actual := 2.7
	got, err := client.ExplainEvent(context.Background(), ExplainEventInput{
		Indicator:      "Consumer Price Index",
		IndicatorType:  "inflation",
		ScheduledAt:    time.Date(2026, 10, 3, 12, 30, 0, 0, time.UTC),
		Consensus:      &consensus,
		Actual:         &actual,
		RecentArticles: []string{"Fed holds rates steady"},
	})
	if err != nil {
		t.Fatalf("ExplainEvent: %v", err)
	}
	if got != "Inflation cooled below consensus." {
		t.Errorf("text = %q, want the provider answer", got)
	}
	prompt := received.Contents[0].Parts[0].Text
	for _, wanted := range []string{"Consumer Price Index (inflation)", "Previous: n/a", "Consensus: 3.1", "Actual: 2.7", "2026-10-03T12:30:00Z"} {
		if !strings.Contains(prompt, wanted) {
			t.Errorf("prompt missing %q: %q", wanted, prompt)
		}
	}
}

func TestExplainWithoutKeyFailsWithoutCallingProvider(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
	}))
	defer server.Close()

	client := newGeminiClient(server.Client(), "", "gemini-3.5-flash-lite")
	client.baseURL = server.URL
	if _, err := client.ExplainArticle(context.Background(), ExplainArticleInput{Title: "x"}); !errors.Is(err, ErrMissingKey) {
		t.Errorf("ExplainArticle error = %v, want ErrMissingKey", err)
	}
	if _, err := client.ExplainEvent(context.Background(), ExplainEventInput{Indicator: "x"}); !errors.Is(err, ErrMissingKey) {
		t.Errorf("ExplainEvent error = %v, want ErrMissingKey", err)
	}
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestExplainSurfacesProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"code":429,"message":"quota exceeded"}}`))
	}))
	defer server.Close()

	client := newGeminiClient(server.Client(), "test-key", "gemini-3.5-flash-lite")
	client.baseURL = server.URL
	_, err := client.ExplainArticle(context.Background(), ExplainArticleInput{Title: "x"})
	if err == nil {
		t.Fatal("ExplainArticle succeeded, want a provider error")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("error = %v, want the status and provider message", err)
	}
}
