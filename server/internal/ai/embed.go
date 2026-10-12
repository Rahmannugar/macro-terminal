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
)

// Embedder turns text into a dense vector for similarity search.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
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
	if embedder.apiKey == "" {
		return nil, ErrMissingKey
	}
	if embedder.model == "" {
		return nil, errors.New("embedding model is not configured")
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("embedding input is empty")
	}

	requestBody := embeddingRequest{
		Model: "models/" + embedder.model,
		Content: embeddingContent{
			Parts: []embeddingPart{{Text: text}},
		},
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:embedContent", embedder.baseURL, embedder.model)
	ctx, cancel := context.WithTimeout(ctx, embeddingTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build embedding request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-goog-api-key", embedder.apiKey)

	response, err := embedder.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call embedding provider: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding provider returned %d: %s", response.StatusCode, errorMessage(body))
	}

	var decoded embeddingResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(decoded.Embedding.Values) == 0 {
		return nil, errors.New("embedding provider returned no values")
	}
	return decoded.Embedding.Values, nil
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
