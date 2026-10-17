package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func batchTestEmbedder(server *httptest.Server) *geminiEmbedder {
	return &geminiEmbedder{
		httpClient: server.Client(),
		baseURL:    server.URL,
		apiKey:     "test-key",
		model:      "gemini-embedding-001",
	}
}

func batchResponse(count int) string {
	entries := make([]string, 0, count)
	for index := 0; index < count; index++ {
		entries = append(entries, fmt.Sprintf(`{"values":[%d,%d]}`, index, -index))
	}
	return `{"embeddings":[` + strings.Join(entries, ",") + `]}`
}

func TestEmbedBatchSendsOneBatchedCallPreservingOrder(t *testing.T) {
	var path string
	var apiKey string
	var received batchEmbeddingRequest
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
		_, _ = writer.Write([]byte(batchResponse(3)))
	}))
	defer server.Close()

	texts := []string{"first article", "second article", "third article"}
	vectors, err := batchTestEmbedder(server).EmbedBatch(t.Context(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}

	if !strings.HasSuffix(path, "/v1beta/models/gemini-embedding-001:batchEmbedContents") {
		t.Errorf("path = %q, want the batch endpoint", path)
	}
	if apiKey != "test-key" {
		t.Errorf("api key = %q, want the configured key", apiKey)
	}
	if len(received.Requests) != len(texts) {
		t.Fatalf("requests = %d, want %d", len(received.Requests), len(texts))
	}
	for index, item := range received.Requests {
		if item.Model != "models/gemini-embedding-001" {
			t.Errorf("request %d model = %q, want the configured model", index, item.Model)
		}
		if item.Content.Parts[0].Text != texts[index] {
			t.Errorf("request %d text = %q, want %q", index, item.Content.Parts[0].Text, texts[index])
		}
	}
	if len(vectors) != len(texts) {
		t.Fatalf("vectors = %d, want %d", len(vectors), len(texts))
	}
	for index, vector := range vectors {
		if len(vector) != 2 || vector[0] != float32(index) {
			t.Errorf("vector %d = %v, want the provider's value for that input", index, vector)
		}
	}
}

func TestEmbedBatchSplitsInputsAboveTheBatchLimit(t *testing.T) {
	calls := 0
	textsSeen := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		var received batchEmbeddingRequest
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		textsSeen += len(received.Requests)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(batchResponse(len(received.Requests))))
	}))
	defer server.Close()

	texts := make([]string, maxBatchEmbeddingRequests+1)
	for index := range texts {
		texts[index] = fmt.Sprintf("article %d", index)
	}
	vectors, err := batchTestEmbedder(server).EmbedBatch(t.Context(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}

	if calls != 2 {
		t.Errorf("provider calls = %d, want 2 (batch limit chunks)", calls)
	}
	if textsSeen != len(texts) {
		t.Errorf("texts sent = %d, want %d", textsSeen, len(texts))
	}
	if len(vectors) != len(texts) {
		t.Errorf("vectors = %d, want %d", len(vectors), len(texts))
	}
}

func TestEmbedBatchRejectsProviderCountMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(batchResponse(1)))
	}))
	defer server.Close()

	_, err := batchTestEmbedder(server).EmbedBatch(t.Context(), []string{"one", "two"})
	if err == nil || !strings.Contains(err.Error(), "1 vectors for 2 inputs") {
		t.Fatalf("error = %v, want the count mismatch reported", err)
	}
}

func TestEmbedBatchRejectsEmptyInputBeforeCallingTheProvider(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls++
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(batchResponse(1)))
	}))
	defer server.Close()

	_, err := batchTestEmbedder(server).EmbedBatch(t.Context(), []string{"ok", "   "})
	if err == nil || !strings.Contains(err.Error(), "embedding input 1 is empty") {
		t.Fatalf("error = %v, want the empty input rejected", err)
	}
	if calls != 0 {
		t.Errorf("provider calls = %d, want 0", calls)
	}
}

func TestEmbedBatchRequiresKey(t *testing.T) {
	embedder := &geminiEmbedder{model: "gemini-embedding-001"}
	_, err := embedder.EmbedBatch(t.Context(), []string{"text"})
	if !errors.Is(err, ErrMissingKey) {
		t.Fatalf("error = %v, want ErrMissingKey", err)
	}
}

func TestEmbedStillUsesTheSingleContentEndpoint(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path = request.URL.Path
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"embedding":{"values":[0.5,0.25]}}`))
	}))
	defer server.Close()

	vector, err := batchTestEmbedder(server).Embed(t.Context(), "a query")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if !strings.HasSuffix(path, "/v1beta/models/gemini-embedding-001:embedContent") {
		t.Errorf("path = %q, want the single-content endpoint", path)
	}
	if len(vector) != 2 || vector[0] != 0.5 {
		t.Errorf("vector = %v, want the provider's values", vector)
	}
}
