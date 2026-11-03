package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/circuitbreaker"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/safehttp"
)

type providerCircuitTransport struct {
	next    http.RoundTripper
	circuit circuitbreaker.Controller
}

func WithProviderCircuit(client *http.Client, circuit circuitbreaker.Controller) *http.Client {
	copy := *client
	next := copy.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	copy.Transport = &providerCircuitTransport{next: next, circuit: circuit}
	return &copy
}

type replayBody struct {
	io.Reader
	io.Closer
}

func providerCircuitScope(request *http.Request) string {
	credential := sha256.Sum256([]byte(request.Header.Get("x-goog-api-key")))
	return fmt.Sprintf("gemini:%s:%s:%x", request.URL.Hostname(), request.URL.EscapedPath(), credential)
}

func (transport *providerCircuitTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	policy := circuitbreaker.DefaultPolicy()
	policy.ProbeLease = batchEmbeddingTimeout + 5*time.Second
	permit, err := transport.circuit.Acquire(request.Context(), providerCircuitScope(request), policy)
	if err != nil {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, err
	}
	outcome := circuitbreaker.Outcome{Neutral: true}
	report := func(outcome circuitbreaker.Outcome) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), 2*time.Second)
		defer cancel()
		if err := transport.circuit.Finish(ctx, permit, outcome); err != nil {
			slog.ErrorContext(ctx, "Could not record AI circuit outcome", "event", "circuit.state_unavailable", "dependency_scope", permit.Scope, "error", err)
		}
	}
	response, err := transport.next.RoundTrip(request)
	if err != nil {
		if !errors.Is(request.Context().Err(), context.Canceled) && !errors.Is(err, safehttp.ErrBlockedAddress) {
			if _, deferred := ProviderDeferral(err); !deferred {
				outcome = circuitbreaker.Outcome{Failed: true, Reason: "provider_transport_error"}
			}
		}
		report(outcome)
		return response, err
	}
	if response.StatusCode == 429 {
		prefix, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body = &replayBody{Reader: io.MultiReader(bytes.NewReader(prefix), response.Body), Closer: response.Body}
		if readErr != nil {
			outcome = circuitbreaker.Outcome{Failed: true, Immediate: true, Pause: time.Minute, Reason: "provider_quota"}
			report(outcome)
			_ = response.Body.Close()
			return nil, &circuitbreaker.PauseError{Scope: permit.Scope, Reason: outcome.Reason, After: outcome.Pause, Cause: readErr}
		}
		failure := quotaFailure(429, errorMessage(prefix), time.Now())
		deferred, _ := ProviderDeferral(failure)
		outcome = circuitbreaker.Outcome{Failed: true, Immediate: true, Pause: max(deferred.Delay, embeddingRetryAfter(response.Header.Get("Retry-After"), prefix)), Reason: deferred.Reason}
	} else if response.StatusCode >= 500 || response.StatusCode == 408 {
		outcome = circuitbreaker.Outcome{Failed: true, Reason: "provider_unavailable"}
	} else if response.StatusCode >= 200 && response.StatusCode < 400 {
		// Recovery is proven only after the response body completes, not at headers.
		response.Body = &circuitResponseBody{ReadCloser: response.Body, ctx: request.Context(), report: report}
		return response, nil
	}
	report(outcome)
	return response, nil
}

type circuitResponseBody struct {
	io.ReadCloser
	ctx    context.Context
	report func(circuitbreaker.Outcome)
	once   sync.Once
}

func (body *circuitResponseBody) Read(buffer []byte) (int, error) {
	count, err := body.ReadCloser.Read(buffer)
	if err != nil {
		outcome := circuitbreaker.Outcome{}
		if !errors.Is(err, io.EOF) {
			outcome = circuitbreaker.Outcome{Failed: true, Reason: "provider_response_error"}
			if errors.Is(body.ctx.Err(), context.Canceled) {
				outcome = circuitbreaker.Outcome{Neutral: true}
			}
		}
		body.once.Do(func() { body.report(outcome) })
	}
	return count, err
}

func (body *circuitResponseBody) Close() error {
	err := body.ReadCloser.Close()
	body.once.Do(func() { body.report(circuitbreaker.Outcome{Neutral: true}) })
	return err
}
