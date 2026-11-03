package ai

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/circuitbreaker"
)

type testProviderCircuit struct {
	acquireErr error
	blocked    string
	outcomes   []circuitbreaker.Outcome
	scopes     []string
}

func (circuit *testProviderCircuit) Acquire(_ context.Context, scope string, policy circuitbreaker.Policy) (circuitbreaker.Permit, error) {
	circuit.scopes = append(circuit.scopes, scope)
	if circuit.acquireErr != nil {
		return circuitbreaker.Permit{}, circuit.acquireErr
	}
	if scope == circuit.blocked {
		return circuitbreaker.Permit{}, &circuitbreaker.PauseError{Scope: scope, Reason: "provider_unavailable", After: time.Minute}
	}
	return circuitbreaker.Permit{Scope: scope, Policy: policy}, nil
}

func TestProviderCircuitStoreOutageDoesNotTryBackup(t *testing.T) {
	circuit := &testProviderCircuit{acquireErr: &circuitbreaker.PauseError{Scope: "test", Reason: "circuit_store_unavailable", After: time.Minute, Cause: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("unavailable")}}}
	client := &http.Client{Transport: embeddingRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("Redis outage reached external provider")
		return nil, errors.New("unexpected external call")
	})}
	client = WithBackupKey(WithProviderCircuit(client, circuit), "primary", "backup")
	_, err := NewEmbedder(client, "primary", "test").Embed(t.Context(), "input")
	deferred, ok := ProviderDeferral(err)
	if !ok || deferred.Reason != "circuit_store_unavailable" || len(circuit.scopes) != 1 {
		t.Fatalf("error=%v attempts=%d", err, len(circuit.scopes))
	}
}
func (circuit *testProviderCircuit) Finish(_ context.Context, _ circuitbreaker.Permit, outcome circuitbreaker.Outcome) error {
	circuit.outcomes = append(circuit.outcomes, outcome)
	return nil
}

func TestProviderCircuitSkipsPausedKeyBeforeReservingEmbeddingBudget(t *testing.T) {
	primary, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, geminiBaseURL+"/v1beta/models/test:embedContent", nil)
	primary.Header.Set("x-goog-api-key", "primary")
	circuit := &testProviderCircuit{blocked: providerCircuitScope(primary)}
	budget := &budgetStub{}
	calls := 0
	client := &http.Client{Transport: embeddingRoundTrip(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Header.Get("x-goog-api-key") != "backup" {
			t.Error("paused key reached provider")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"embedding":{"values":[0.3]}}`)), Request: request}, nil
	})}
	client = WithBackupKey(WithProviderCircuit(WithEmbeddingBudget(client, budget), circuit), "primary", "backup")
	_, err := NewEmbedder(client, "primary", "test").Embed(t.Context(), "test input")
	if err != nil || calls != 1 || budget.reservations != 1 || len(circuit.scopes) != 2 || len(circuit.outcomes) != 1 || circuit.outcomes[0].Failed {
		t.Fatalf("error=%v calls=%d reservations=%d outcomes=%v", err, calls, budget.reservations, circuit.outcomes)
	}
	if strings.Contains(circuit.blocked, "primary") {
		t.Fatal("credential leaked into circuit scope")
	}
}

type incompleteProviderBody struct{}

func (incompleteProviderBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (incompleteProviderBody) Close() error             { return nil }

func TestProviderCircuitWaitsForBodyAndIgnoresLocalBudgetDeferral(t *testing.T) {
	circuit := &testProviderCircuit{}
	client := &http.Client{Transport: embeddingRoundTrip(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: incompleteProviderBody{}, Request: request}, nil
	})}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com/model", nil)
	response, err := WithProviderCircuit(client, circuit).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(circuit.outcomes) != 0 {
		t.Fatal("headers alone marked provider healthy")
	}
	_, err = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if !errors.Is(err, io.ErrUnexpectedEOF) || len(circuit.outcomes) != 1 || !circuit.outcomes[0].Failed {
		t.Fatalf("read error=%v outcomes=%v", err, circuit.outcomes)
	}
	circuit.outcomes = nil
	client.Transport = embeddingRoundTrip(func(*http.Request) (*http.Response, error) {
		return nil, &DeferredError{Reason: "local_budget", Delay: time.Minute}
	})
	response, err = WithProviderCircuit(client, circuit).Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if _, ok := ProviderDeferral(err); !ok || len(circuit.outcomes) != 1 || !circuit.outcomes[0].Neutral {
		t.Fatalf("error=%v outcomes=%v", err, circuit.outcomes)
	}
}

func TestProviderQuotaOpensSharedPauseAndPreservesErrorBody(t *testing.T) {
	circuit := &testProviderCircuit{}
	body := `{"error":{"message":"tokens per minute quota","details":[{"retryDelay":"180s"}]}}`
	client := &http.Client{Transport: embeddingRoundTrip(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com/model", nil)
	response, err := WithProviderCircuit(client, circuit).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || string(content) != body || len(circuit.outcomes) != 1 || !circuit.outcomes[0].Immediate || circuit.outcomes[0].Pause != 3*time.Minute {
		t.Fatalf("body=%q error=%v outcomes=%v", content, err, circuit.outcomes)
	}
}
