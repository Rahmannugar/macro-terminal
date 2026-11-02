package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Reserve accounts for embedding work across processes before contacting the provider.
type EmbeddingBudget interface {
	Reserve(ctx context.Context, tokens, requests int) (time.Duration, error)
}

type embeddingBudgetTransport struct {
	next   http.RoundTripper
	budget EmbeddingBudget
}

// Install inside key failover so each outbound attempt reserves its own allowance.
func WithEmbeddingBudget(client *http.Client, budget EmbeddingBudget) *http.Client {
	copy := *client
	next := copy.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	copy.Transport = &embeddingBudgetTransport{next: next, budget: budget}
	return &copy
}

func (transport *embeddingBudgetTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.GetBody == nil {
		return nil, errors.New("embedding request body cannot be inspected")
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	var payload struct {
		Content  embeddingContent   `json:"content"`
		Requests []embeddingRequest `json:"requests"`
	}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("read embedding budget input: %w", err)
	}
	inputs := payload.Requests
	if len(inputs) == 0 {
		inputs = []embeddingRequest{{Content: payload.Content}}
	}
	tokens := 0
	for _, input := range inputs {
		for _, part := range input.Content.Parts {
			tokens += EmbeddingCost(part.Text)
		}
	}
	delay, err := transport.budget.Reserve(request.Context(), tokens, len(inputs))
	if err != nil {
		slog.ErrorContext(request.Context(), "Embedding allowance unavailable", "event", "embedding.budget_unavailable", "error", err)
		return nil, &DeferredError{Reason: "budget_unavailable", Delay: time.Minute}
	}
	if delay > 0 {
		slog.InfoContext(request.Context(), "Embedding request waiting for allowance", "event", "embedding.deferred",
			"reason", "local_budget", "estimated_tokens", tokens, "inputs", len(inputs), "retry_after_seconds", delay.Seconds())
		return nil, &DeferredError{Reason: "local_budget", Delay: delay}
	}
	return transport.next.RoundTrip(request)
}

const EmbeddingTokenBudget = 24000

// EmbeddingCost uses bytes as a conservative token allowance, including framing.
// This deliberately avoids the English-only assumption of four characters per token.
func EmbeddingCost(text string) int { return len(text) + 32 }

func EmbeddingBatchEnd(texts []string, start int) int {
	end, tokens := start, 0
	for end < len(texts) && end-start < maxBatchEmbeddingRequests {
		cost := EmbeddingCost(texts[end])
		if end > start && tokens+cost > EmbeddingTokenBudget {
			break
		}
		tokens += cost
		end++
	}
	return end
}

type DeferredError struct {
	Reason string
	Delay  time.Duration
	Detail string
}

func (failure *DeferredError) Error() string {
	if failure.Detail != "" {
		return fmt.Sprintf("embedding deferred: %s; retry after %s: %s", failure.Reason, failure.Delay.Round(time.Second), failure.Detail)
	}
	return fmt.Sprintf("embedding deferred: %s; retry after %s", failure.Reason, failure.Delay.Round(time.Second))
}

func EmbeddingDeferral(err error) (*DeferredError, bool) {
	var failure *DeferredError
	ok := errors.As(err, &failure)
	return failure, ok
}

func quotaFailure(status int, message string, now time.Time) error {
	if status != 429 {
		return fmt.Errorf("embedding provider returned %d: %s", status, message)
	}
	delay, reason := time.Minute, "provider_quota"
	lower := strings.ToLower(message)
	if strings.Contains(lower, "token") {
		reason = "provider_token_quota"
	}
	if strings.Contains(lower, "per day") || strings.Contains(lower, "per_day") {
		// Daily Gemini quotas reset at midnight in Pacific time.
		location, err := time.LoadLocation("America/Los_Angeles")
		if err == nil {
			local := now.In(location)
			delay = time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 1, 0, location).Sub(now)
		} else {
			delay = 24 * time.Hour
		}
		reason = "provider_daily_quota"
	}
	return &DeferredError{Reason: reason, Delay: delay, Detail: message}
}
