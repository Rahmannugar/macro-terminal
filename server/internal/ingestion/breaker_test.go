package ingestion

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/circuitbreaker"
)

type fakeCircuit struct {
	acquireErr error
	outcomes   []circuitbreaker.Outcome
}

func TestProviderScopesAndFailureKinds(t *testing.T) {
	configuration := testConfiguration("web", `{"url":"https://first.example/feed"}`)
	first, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://first.example/article", nil)
	second, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://second.example/article", nil)
	if providerScope(first, configuration, "article_content") == providerScope(second, configuration, "article_content") {
		t.Fatal("unrelated publishers share circuit")
	}
	if providerScope(first, configuration, "article_content") == providerScope(first, configuration, "source_fetch") {
		t.Fatal("article refusal can block feed")
	}
	if !providerOutcome(404, 0, "article_content").Neutral {
		t.Fatal("missing article poisons publisher circuit")
	}
	if !providerOutcome(403, 0, "source_fetch").Neutral {
		t.Fatal("configuration access error poisons availability circuit")
	}
	response := &http.Response{StatusCode: 401, Header: make(http.Header)}
	if reason := providerFailureReason(response, []byte(`<script src="https://ct.captcha-delivery.com/c.js"></script>`)); reason != "publisher_browser_challenge" {
		t.Fatalf("reason=%s", reason)
	}
}

func TestCancelledProviderCallReleasesProbe(t *testing.T) {
	circuit := &fakeCircuit{}
	fetcher := newFetcher(doerFunc(func(*http.Request) (*http.Response, error) { return nil, context.Canceled }), circuit, discardLogger())
	fetcher.checkDestination = nil
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := fetcher.Fetch(ctx, testConfiguration("api", `{"url":"https://example.com"}`))
	if !errors.Is(err, context.Canceled) || len(circuit.outcomes) != 1 || !circuit.outcomes[0].Neutral {
		t.Fatalf("error=%v outcomes=%v", err, circuit.outcomes)
	}
}

func TestPublisherChallengeIsClassifiedWithoutAListener(t *testing.T) {
	circuit := &fakeCircuit{}
	fetcher := newFetcher(doerFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<script src="https://ct.captcha-delivery.com/c.js"></script>`)), Request: request}, nil
	}), circuit, discardLogger())
	fetcher.checkDestination = nil
	_, err := fetcher.FetchArticle(t.Context(), testConfiguration("web", `{"url":"https://example.com/article"}`))
	var failure interface{ FailureReason() string }
	if !errors.As(err, &failure) || failure.FailureReason() != "publisher_browser_challenge" || len(circuit.outcomes) != 1 || !circuit.outcomes[0].Failed {
		t.Fatalf("error=%v outcomes=%v", err, circuit.outcomes)
	}
}

func (circuit *fakeCircuit) Acquire(_ context.Context, scope string, policy circuitbreaker.Policy) (circuitbreaker.Permit, error) {
	return circuitbreaker.Permit{Scope: scope, Policy: policy}, circuit.acquireErr
}
func (circuit *fakeCircuit) Finish(_ context.Context, _ circuitbreaker.Permit, outcome circuitbreaker.Outcome) error {
	circuit.outcomes = append(circuit.outcomes, outcome)
	return nil
}
