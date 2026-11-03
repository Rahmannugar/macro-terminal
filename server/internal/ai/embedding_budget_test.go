package ai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type budgetStub struct {
	reservations int
	tokens       int
}

func (budget *budgetStub) Reserve(_ context.Context, tokens, _ int) (time.Duration, error) {
	budget.reservations++
	budget.tokens += tokens
	if budget.reservations > 1 {
		return time.Minute, nil
	}
	return 0, nil
}

type embeddingRoundTrip func(*http.Request) (*http.Response, error)

func (roundTrip embeddingRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestEmbeddingFailoverReservesEachAttempt(t *testing.T) {
	budget := &budgetStub{}
	calls := 0
	client := &http.Client{Transport: embeddingRoundTrip(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header), Request: request}, nil
	})}
	client = WithBackupKey(WithEmbeddingBudget(client, budget), "primary", "backup")
	embedder := NewEmbedder(client, "primary", "test")
	_, err := embedder.Embed(t.Context(), "test input")
	deferred, ok := ProviderDeferral(err)
	if !ok || deferred.Reason != "local_budget" || calls != 1 || budget.reservations != 2 {
		t.Fatalf("error=%v calls=%d reservations=%d", err, calls, budget.reservations)
	}
	if budget.tokens != 2*EmbeddingCost("test input") {
		t.Fatalf("reserved tokens=%d", budget.tokens)
	}
}

func TestEmbeddingBatchesRespectInputCost(t *testing.T) {
	texts := []string{strings.Repeat("x", 6000), strings.Repeat("€", 2000), strings.Repeat("x", 6000), strings.Repeat("x", 6000)}
	if end := EmbeddingBatchEnd(texts, 0); end != 3 {
		t.Fatalf("batch end=%d, want 3", end)
	}
}

func TestQuotaDeferralDistinguishesDailyReset(t *testing.T) {
	now := time.Date(2026, 11, 2, 8, 10, 0, 0, time.UTC)
	for _, test := range []struct {
		message string
		reason  string
		delay   time.Duration
	}{
		{"Embed content tokens per minute limit exceeded", "provider_token_quota", time.Minute},
		{"Embed content requests per day limit exceeded", "provider_daily_quota", 23*time.Hour + 50*time.Minute + time.Second},
	} {
		err := quotaFailure(429, test.message, now)
		deferred, ok := ProviderDeferral(err)
		if !ok || deferred.Reason != test.reason || deferred.Delay != test.delay {
			t.Fatalf("message=%s error=%v", test.message, err)
		}
	}
	if delay := embeddingRetryAfter("120", []byte(`{"error":{"details":[{"retryDelay":"180s"}]}}`)); delay != 3*time.Minute {
		t.Fatalf("retry delay=%s", delay)
	}
}
