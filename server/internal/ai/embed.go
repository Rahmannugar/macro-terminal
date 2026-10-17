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
	embeddingTimeout = 30 * time.Second
	// batchEmbeddingTimeout gives a full batch payload room to finish.
	batchEmbeddingTimeout = 60 * time.Second
	// maxBatchEmbeddingRequests keeps one call inside the provider's batch
	// size; larger inputs split into sequential calls.
	maxBatchEmbeddingRequests = 100
	// maxEmbeddingResponseBytes covers the largest batch response
	// (100 inputs x 3072-dimension values as JSON).
	maxEmbeddingResponseBytes = 8 << 20
)

// Embedder turns text into a dense vector for similarity search.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	// EmbedBatch embeds many texts in as few provider calls as the batch
	// size allows, preserving input order.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

type geminiEmbedder struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

// NewEmbedder builds a Gemini-backed text embedder. model should be a
// Gemini embedding model such as text-embedding-004.
func NewEmbedder(httpClient *http.Client, apiKey, model string) Embedder {
	return &geminiEmbedder{
		httpClient: httpClient,
		baseURL:    geminiBaseURL,
		apiKey:     strings.TrimSpace(apiKey),
		model:      strings.TrimSpace(model),
	}
}

func (embedder *geminiEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := embedder.validate(text); err != nil {
		return nil, err
	}

	requestBody := embeddingRequest{
		Model: "models/" + embedder.model,
		Content: embeddingContent{
			Parts: []embeddingPart{{Text: text}},
		},
	}
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:embedContent", embedder.baseURL, embedder.model)

	var decoded embeddingResponse
	if err := embedder.postJSON(ctx, endpoint, requestBody, embeddingTimeout, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Embedding.Values) == 0 {
		return nil, errors.New("embedding provider returned no values")
	}
	return decoded.Embedding.Values, nil
}

func (embedder *geminiEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if err := embedder.validate(texts...); err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:batchEmbedContents", embedder.baseURL, embedder.model)
	vectors := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += maxBatchEmbeddingRequests {
		end := min(start+maxBatchEmbeddingRequests, len(texts))
		chunk, err := embedder.embedChunk(ctx, endpoint, texts[start:end])
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, chunk...)
	}
	return vectors, nil
}

func (embedder *geminiEmbedder) embedChunk(ctx context.Context, endpoint string, texts []string) ([][]float32, error) {
	requests := make([]embeddingRequest, 0, len(texts))
	for _, text := range texts {
		requests = append(requests, embeddingRequest{
			Model: "models/" + embedder.model,
			Content: embeddingContent{
				Parts: []embeddingPart{{Text: text}},
			},
		})
	}

	var decoded batchEmbeddingResponse
	if err := embedder.postJSON(ctx, endpoint, batchEmbeddingRequest{Requests: requests}, batchEmbeddingTimeout, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embedding provider returned %d vectors for %d inputs", len(decoded.Embeddings), len(texts))
	}
	vectors := make([][]float32, 0, len(texts))
	for index, embedding := range decoded.Embeddings {
		if len(embedding.Values) == 0 {
			return nil, fmt.Errorf("embedding provider returned no values for input %d", index)
		}
		vectors = append(vectors, embedding.Values)
	}
	return vectors, nil
}

func (embedder *geminiEmbedder) validate(texts ...string) error {
	if embedder.apiKey == "" {
		return ErrMissingKey
	}
	if embedder.model == "" {
		return errors.New("embedding model is not configured")
	}
	if len(texts) == 0 {
		return errors.New("embedding input is empty")
	}
	for index, text := range texts {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("embedding input %d is empty", index)
		}
	}
	return nil
}

func (embedder *geminiEmbedder) postJSON(
	ctx context.Context,
	endpoint string,
	requestBody any,
	timeout time.Duration,
	decoded any,
) error {
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("encode embedding request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build embedding request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-goog-api-key", embedder.apiKey)

	response, err := embedder.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call embedding provider: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxEmbeddingResponseBytes))
	if err != nil {
		return fmt.Errorf("read embedding response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("embedding provider returned %d: %s", response.StatusCode, errorMessage(body))
	}
	if err := json.Unmarshal(body, decoded); err != nil {
		return fmt.Errorf("decode embedding response: %w", err)
	}
	return nil
}

type embeddingRequest struct {
	Model   string           `json:"model"`
	Content embeddingContent `json:"content"`
}

type embeddingContent struct {
	Parts []embeddingPart `json:"parts"`
}

type embeddingPart struct {
	Text string `json:"text"`
}

type embeddingResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

type batchEmbeddingRequest struct {
	Requests []embeddingRequest `json:"requests"`
}

type batchEmbeddingResponse struct {
	Embeddings []struct {
		Values []float32 `json:"values"`
	} `json:"embeddings"`
}
