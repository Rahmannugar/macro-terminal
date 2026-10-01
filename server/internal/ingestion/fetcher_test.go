package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/safehttp"
	"github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"github.com/google/uuid"
)

func asFailure(err error, target *Failure) bool { return errors.As(err, target) }

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(request *http.Request) (*http.Response, error) { return f(request) }
func isMalformed(err error) bool                                    { return errors.Is(err, ErrMalformed) }
func isSecretMissing(err error) bool                                { return errors.Is(err, ErrSecretMissing) }
func isInsecureURL(err error) bool                                  { return errors.Is(err, ErrInsecureURL) }
func isCircuitOpen(err error) bool                                  { return errors.Is(err, ErrCircuitOpen) }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfiguration(configType, configJSON string) models.SourceConfigurationWithSource {
	return models.SourceConfigurationWithSource{
		SourceConfiguration: models.SourceConfiguration{
			ID:       uuid.New(),
			SourceID: uuid.New(),
			Type:     configType,
			Config:   json.RawMessage(configJSON),
		},
		SourceName: "Test Source",
		SourceType: "news",
	}
}

// newTestFetcher returns a fetcher wired to the handler, a recorder of sleep
// delays, and a counter of provider requests.
func newTestFetcher(t *testing.T, handler http.HandlerFunc) (*Fetcher, *httptest.Server, *atomic.Int64, *[]time.Duration) {
	t.Helper()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	fetcher := newFetcher(server.Client(), NewBreaker(5, time.Minute), discardLogger())
	// The address policy refuses loopback fixtures; tests reach them
	// through the injected client instead.
	fetcher.checkDestination = nil
	fetcher.baseDelay = 10 * time.Millisecond
	fetcher.maxDelay = time.Minute
	var delays []time.Duration
	fetcher.sleep = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}
	return fetcher, server, &requests, &delays
}

func TestFetcherBlocksRefusedConfigurationURLs(t *testing.T) {
	fetcher, _, requests, _ := newTestFetcher(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("refused destination should never be requested")
	})
	// This test verifies the production policy, so put it back after the
	// helper relaxed it for loopback fixtures.
	fetcher.checkDestination = safehttp.CheckDestination

	refused := []string{
		`{"url":"http://169.254.169.254/latest/meta-data/"}`,
		`{"url":"http://127.0.0.1:6380/"}`,
		`{"url":"http://[::1]/"}`,
		`{"url":"http://example.com:5433/"}`,
	}
	for _, config := range refused {
		_, err := fetcher.Fetch(context.Background(), testConfiguration("rss", config))
		if !errors.Is(err, safehttp.ErrBlockedAddress) {
			t.Errorf("config %s: err = %v, want ErrBlockedAddress", config, err)
		}
	}
	if requests.Load() != 0 {
		t.Errorf("provider requests = %d, want 0", requests.Load())
	}
}

func TestFetcherDoesNotRetryBlockedDestinations(t *testing.T) {
	var attempts atomic.Int64
	blocked := doerFunc(func(*http.Request) (*http.Response, error) {
		attempts.Add(1)
		return nil, fmt.Errorf("dial tcp: %w", safehttp.ErrBlockedAddress)
	})
	fetcher := newFetcher(blocked, NewBreaker(5, time.Minute), discardLogger())
	fetcher.checkDestination = nil

	_, err := fetcher.Fetch(context.Background(), testConfiguration("rss", `{"url":"https://example.com/feed"}`))
	if !errors.Is(err, safehttp.ErrBlockedAddress) {
		t.Fatalf("Fetch = %v, want ErrBlockedAddress", err)
	}
	var failure Failure
	if !asFailure(err, &failure) || failure.Retryable() {
		t.Fatalf("blocked destination must fail permanently, got %+v", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1 (no retries)", attempts.Load())
	}
}

func TestFetcherSuccessJSON(t *testing.T) {
	fetcher, server, requests, delays := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Errorf("User-Agent = %q, want %q", got, userAgent)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	configuration := testConfiguration("api", `{"url":"`+server.URL+`/data"}`)
	result, err := fetcher.Fetch(context.Background(), configuration)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.StatusCode != http.StatusOK || result.Attempts != 1 {
		t.Fatalf("status=%d attempts=%d, want 200/1", result.StatusCode, result.Attempts)
	}
	if string(result.Body) != `{"status":"ok"}` {
		t.Fatalf("Body = %q", result.Body)
	}
	if requests.Load() != 1 || len(*delays) != 0 {
		t.Fatalf("requests=%d delays=%d, want 1/0", requests.Load(), len(*delays))
	}
}

func TestFetcherRetriesTemporaryFailures(t *testing.T) {
	var failures atomic.Int64
	fetcher, server, requests, delays := newTestFetcher(t, func(w http.ResponseWriter, _ *http.Request) {
		if failures.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	configuration := testConfiguration("api", `{"url":"`+server.URL+`/api"}`)
	result, err := fetcher.Fetch(context.Background(), configuration)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", result.Attempts)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
	wantDelays := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}
	if len(*delays) != len(wantDelays) {
		t.Fatalf("delays = %v, want %v", *delays, wantDelays)
	}
	for i, want := range wantDelays {
		if (*delays)[i] != want {
			t.Fatalf("delay[%d] = %v, want %v", i, (*delays)[i], want)
		}
	}
}

func TestFetcherHonorsRetryAfter(t *testing.T) {
	var failures atomic.Int64
	fetcher, server, _, delays := newTestFetcher(t, func(w http.ResponseWriter, _ *http.Request) {
		if failures.Add(1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	configuration := testConfiguration("api", `{"url":"`+server.URL+`/api"}`)
	if _, err := fetcher.Fetch(context.Background(), configuration); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(*delays) != 1 || (*delays)[0] != 7*time.Second {
		t.Fatalf("delays = %v, want [7s] (provider Retry-After beats backoff)", *delays)
	}
}

func TestFetcherGivesUpAfterMaxAttempts(t *testing.T) {
	fetcher, server, requests, _ := newTestFetcher(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	fetcher.breaker = NewBreaker(1, time.Minute)

	configuration := testConfiguration("api", `{"url":"`+server.URL+`/api"}`)
	_, err := fetcher.Fetch(context.Background(), configuration)
	if err == nil {
		t.Fatalf("Fetch should fail after exhausting attempts")
	}
	var failure Failure
	if !asFailure(err, &failure) || !failure.Retryable() {
		t.Fatalf("exhausted error should stay classified retryable: %v", err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
	if fetcher.breaker.Allow(configuration.ID.String()) {
		t.Fatalf("exhausted retries must record a breaker failure")
	}
}

func TestFetcherDoesNotRetryMalformed(t *testing.T) {
	fetcher, server, requests, delays := newTestFetcher(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json at all"))
	})

	configuration := testConfiguration("api", `{"url":"`+server.URL+`/api"}`)
	_, err := fetcher.Fetch(context.Background(), configuration)
	if err == nil {
		t.Fatalf("malformed response should fail")
	}
	if !isMalformed(err) {
		t.Fatalf("error = %v, want ErrMalformed", err)
	}
	if requests.Load() != 1 || len(*delays) != 0 {
		t.Fatalf("malformed responses must not retry: requests=%d delays=%d", requests.Load(), len(*delays))
	}
}

func TestFetcherDoesNotRetryClientError(t *testing.T) {
	fetcher, server, requests, delays := newTestFetcher(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	configuration := testConfiguration("api", `{"url":"`+server.URL+`/api"}`)
	_, err := fetcher.Fetch(context.Background(), configuration)
	if err == nil {
		t.Fatalf("404 should fail")
	}
	var failure Failure
	if asFailure(err, &failure) && failure.Retryable() {
		t.Fatalf("404 must not be retryable: %v", err)
	}
	if requests.Load() != 1 || len(*delays) != 0 {
		t.Fatalf("client errors must not retry: requests=%d delays=%d", requests.Load(), len(*delays))
	}
}

func TestFetcherInjectsConfigurationParameters(t *testing.T) {
	t.Setenv("TEST_FETCHER_SOURCE_KEY", "s3cret")
	handler := func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("query") != "(financial OR economic)" {
			t.Errorf("query param = %q", query.Get("query"))
		}
		if query.Get("language") != "english" {
			t.Errorf("language param = %q", query.Get("language"))
		}
		if query.Get("apikey") != "s3cret" {
			t.Errorf("apikey param = %q", query.Get("apikey"))
		}
		if query.Has("api_key_param") || query.Has("api_key_env") {
			t.Errorf("meta keys leaked into the query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
	fetcher, server, _, _ := newTestFetcher(t, handler)

	configuration := testConfiguration("api", `{"url":"`+server.URL+`/doc","query":"(financial OR economic)","language":"english","api_key_param":"apikey","api_key_env":"TEST_FETCHER_SOURCE_KEY"}`)
	if _, err := fetcher.Fetch(context.Background(), configuration); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestFetcherAcceptOverride(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/xml" {
			t.Errorf("Accept = %q, want application/xml", got)
		}
		if r.URL.Query().Has("accept") {
			t.Errorf("accept leaked into query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`<?xml version="1.0"?><data/>`))
	}
	fetcher, server, _, _ := newTestFetcher(t, handler)

	configuration := testConfiguration("api", `{"url":"`+server.URL+`/data","accept":"application/xml"}`)
	if _, err := fetcher.Fetch(context.Background(), configuration); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestFetcherMissingSecretSkipsBeforeRequest(t *testing.T) {
	t.Setenv("TEST_FETCHER_ABSENT_KEY", "")
	fetcher, server, requests, _ := newTestFetcher(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	configuration := testConfiguration("api", `{"url":"`+server.URL+`/api","api_key_param":"apikey","api_key_env":"TEST_FETCHER_ABSENT_KEY"}`)
	_, err := fetcher.Fetch(context.Background(), configuration)
	if !isSecretMissing(err) {
		t.Fatalf("error = %v, want ErrSecretMissing", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
}

func TestFetcherRejectsInsecureURL(t *testing.T) {
	fetcher, _, requests, _ := newTestFetcher(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	configuration := testConfiguration("api", `{"url":"ftp://example.com/api"}`)
	_, err := fetcher.Fetch(context.Background(), configuration)
	if !isInsecureURL(err) {
		t.Fatalf("error = %v, want ErrInsecureURL", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
}

func TestFetcherOpenCircuitSkipsProvider(t *testing.T) {
	fetcher, server, requests, _ := newTestFetcher(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	configuration := testConfiguration("api", `{"url":"`+server.URL+`/api"}`)
	fetcher.breaker = NewBreaker(1, time.Minute)
	fetcher.breaker.RecordFailure(configuration.ID.String())

	_, err := fetcher.Fetch(context.Background(), configuration)
	if !isCircuitOpen(err) {
		t.Fatalf("error = %v, want ErrCircuitOpen", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	if got := parseRetryAfter("12", now); got != 12*time.Second {
		t.Fatalf("seconds form = %v, want 12s", got)
	}
	if got := parseRetryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now); got != 90*time.Second {
		t.Fatalf("http-date form = %v, want 90s", got)
	}
	if got := parseRetryAfter("nonsense", now); got != 0 {
		t.Fatalf("invalid form = %v, want 0", got)
	}
	if got := parseRetryAfter("", now); got != 0 {
		t.Fatalf("empty form = %v, want 0", got)
	}
}

func TestFetcherSpacesRequestsPerHost(t *testing.T) {
	fetcher, server, requests, delays := newTestFetcher(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("min_interval_s") != "" {
			t.Errorf("min_interval_s must not be sent to the provider")
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	current := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	fetcher.now = func() time.Time { return current }
	fetcher.sleep = func(_ context.Context, delay time.Duration) error {
		*delays = append(*delays, delay)
		current = current.Add(delay)
		return nil
	}

	first := testConfiguration("api", `{"url":"`+server.URL+`/data","min_interval_s":5}`)
	second := testConfiguration("api", `{"url":"`+server.URL+`/data","min_interval_s":5}`)

	if _, err := fetcher.Fetch(context.Background(), first); err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if _, err := fetcher.Fetch(context.Background(), second); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}

	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
	if len(*delays) != 1 || (*delays)[0] != 5*time.Second {
		t.Fatalf("delays = %v, want a single 5s spacing wait", *delays)
	}
}
