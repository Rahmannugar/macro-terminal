package ingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// instrumentationPath names this package's spans.
const instrumentationPath = "github.com/Rahmannugar/macro-terminal/server/internal/ingestion"

const (
	// maxAttempts is the total attempts per fetch, including the first one.
	maxAttempts = 3
	// baseRetryDelay starts the exponential backoff between attempts.
	baseRetryDelay = 500 * time.Millisecond
	// maxRetryDelay caps any single wait, including a provider's Retry-After.
	maxRetryDelay = 30 * time.Second
	// maxResponseBytes bounds provider reads so a runaway response cannot
	// exhaust worker memory.
	maxResponseBytes = 16 << 20
	// userAgent identifies the terminal to providers.
	userAgent = "macro-terminal/1.0 (+https://macroterminal.consumel.com)"
)

// HTTPDoer is the subset of http.Client the fetcher needs.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Fetcher retrieves provider responses with failure classification, bounded
// retries, and circuit breaking.
type Fetcher struct {
	client      HTTPDoer
	breaker     *Breaker
	logger      *slog.Logger
	tracer      trace.Tracer
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	sleep       func(context.Context, time.Duration) error
	now         func() time.Time

	spacingMu       sync.Mutex
	lastHostRequest map[string]time.Time
}

// NewFetcher builds a fetcher on the instrumented HTTP client from telemetry.
func NewFetcher(client HTTPDoer, breaker *Breaker, logger *slog.Logger) *Fetcher {
	return newFetcher(client, breaker, logger)
}

func newFetcher(client HTTPDoer, breaker *Breaker, logger *slog.Logger) *Fetcher {
	return &Fetcher{
		client:      client,
		breaker:     breaker,
		logger:      logger,
		tracer:      otel.Tracer(instrumentationPath),
		maxAttempts: maxAttempts,
		baseDelay:   baseRetryDelay,
		maxDelay:    maxRetryDelay,
		sleep:       sleepContext,
		now:         time.Now,

		lastHostRequest: map[string]time.Time{},
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Fetch retrieves and parses one source configuration. Build failures
// (invalid configuration, missing secret) never reach the provider and never
// count against the circuit breaker.
func (fetcher *Fetcher) Fetch(
	ctx context.Context,
	configuration models.SourceConfigurationWithSource,
) (Result, error) {
	fetchContext, span := fetcher.tracer.Start(ctx, "ingestion.fetch",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("source.name", configuration.SourceName),
			attribute.String("source.type", configuration.SourceType),
			attribute.String("configuration.type", configuration.Type),
		))
	defer span.End()

	request, err := fetcher.buildRequest(fetchContext, configuration)
	if err != nil {
		return Result{}, err
	}
	interval := configurationInterval(configuration.Config)

	key := configuration.ID.String()
	if !fetcher.breaker.Allow(key) {
		return Result{}, ErrCircuitOpen
	}

	var lastErr error
	for attempt := 1; attempt <= fetcher.maxAttempts; attempt++ {
		if spacingErr := fetcher.waitHostSpacing(fetchContext, request.URL.Host, interval); spacingErr != nil {
			return Result{}, spacingErr
		}
		result, attemptErr := fetcher.attempt(fetchContext, request, configuration)
		if attemptErr == nil {
			result.Attempts = attempt
			result.FetchedAt = fetcher.now()
			fetcher.breaker.RecordSuccess(key)
			return result, nil
		}
		lastErr = attemptErr
		if fetchContext.Err() != nil {
			return Result{}, fetchContext.Err()
		}
		if !retryable(attemptErr) || attempt == fetcher.maxAttempts {
			break
		}
		if sleepErr := fetcher.sleep(fetchContext, fetcher.retryDelay(attempt, attemptErr)); sleepErr != nil {
			return Result{}, sleepErr
		}
	}
	span.RecordError(lastErr)
	fetcher.breaker.RecordFailure(key)
	return Result{}, lastErr
}

func (fetcher *Fetcher) attempt(
	ctx context.Context,
	request *http.Request,
	configuration models.SourceConfigurationWithSource,
) (Result, error) {
	attemptRequest := request.Clone(ctx)
	response, err := fetcher.client.Do(attemptRequest)
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, &fetchError{
			cause:     fmt.Errorf("request failed: %w", err),
			retryable: true,
		}
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return Result{}, &fetchError{
			cause:     fmt.Errorf("read response: %w", err),
			retryable: true,
		}
	}

	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		adapter, err := AdapterFor(configuration.Type)
		if err != nil {
			return Result{}, err
		}
		result, err := adapter.Parse(body)
		if err != nil {
			return Result{}, &fetchError{
				cause:      err,
				statusCode: response.StatusCode,
				retryable:  false,
			}
		}
		result.StatusCode = response.StatusCode
		return result, nil
	case response.StatusCode == http.StatusRequestTimeout ||
		response.StatusCode == http.StatusTooEarly ||
		response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode >= http.StatusInternalServerError:
		return Result{}, &fetchError{
			cause:      fmt.Errorf("provider returned status %d", response.StatusCode),
			statusCode: response.StatusCode,
			retryable:  true,
			retryAfter: parseRetryAfter(response.Header.Get("Retry-After"), fetcher.now()),
		}
	default:
		return Result{}, &fetchError{
			cause:      fmt.Errorf("provider returned status %d", response.StatusCode),
			statusCode: response.StatusCode,
			retryable:  false,
		}
	}
}

func (fetcher *Fetcher) retryDelay(attempt int, err error) time.Duration {
	delay := fetcher.baseDelay << (attempt - 1)
	if providerWait := retryAfter(err); providerWait > delay {
		delay = providerWait
	}
	if delay > fetcher.maxDelay {
		delay = fetcher.maxDelay
	}
	return delay
}

// buildRequest turns configuration JSON into a request: the URL, string
// parameters (provider-specific keys such as GDELT's query/language), and
// `*_env`/`*_param` pairs that inject environment-held secrets — secrets
// never live in JSONB configuration.
func (fetcher *Fetcher) buildRequest(
	ctx context.Context,
	configuration models.SourceConfigurationWithSource,
) (*http.Request, error) {
	decoder := json.NewDecoder(bytes.NewReader(configuration.Config))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode configuration: %w", err)
	}

	rawURL, ok := document["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return nil, fmt.Errorf("%w: configuration has no url", ErrInvalidURL)
	}

	query, err := configurationQuery(document)
	if err != nil {
		return nil, err
	}

	target, err := url.Parse(rawURL)
	if err != nil || target.Host == "" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidURL, rawURL)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("%w: %s", ErrInsecureURL, target.Scheme)
	}
	existing := target.Query()
	for key, values := range query {
		existing.Del(key)
		for _, value := range values {
			existing.Add(key, value)
		}
	}
	target.RawQuery = existing.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("User-Agent", userAgent)
	// SDMX statistics gateways reject Accept values containing application/json.
	accept := acceptFor(configuration.Type)
	if value, ok := document["accept"].(string); ok && strings.TrimSpace(value) != "" {
		accept = value
	}
	request.Header.Set("Accept", accept)
	return request, nil
}

func configurationQuery(document map[string]any) (url.Values, error) {
	query := url.Values{}
	for key, raw := range document {
		switch {
		case key == "url" || key == "min_interval_s" || key == "accept":
			continue
		case strings.HasSuffix(key, "_env"):
			base := strings.TrimSuffix(key, "_env")
			envName, ok := raw.(string)
			if !ok || envName == "" {
				return nil, fmt.Errorf("configuration key %s must name an environment variable", key)
			}
			secret := os.Getenv(envName)
			if secret == "" {
				return nil, fmt.Errorf("%w: %s", ErrSecretMissing, envName)
			}
			paramName, _ := document[base+"_param"].(string)
			if paramName == "" {
				paramName = base
			}
			query.Set(paramName, secret)
		case strings.HasSuffix(key, "_param"):
			continue
		default:
			query.Set(key, fmt.Sprint(raw))
		}
	}
	return query, nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := when.Sub(now); delay > 0 {
			return delay
		}
	}
	return 0
}

// configurationInterval reads the optional per-host request spacing
// (min_interval_s) from configuration JSON; zero when unset.
func configurationInterval(config []byte) time.Duration {
	var limits struct {
		MinIntervalSeconds int `json:"min_interval_s"`
	}
	if err := json.Unmarshal(config, &limits); err != nil {
		return 0
	}
	if limits.MinIntervalSeconds <= 0 {
		return 0
	}
	return time.Duration(limits.MinIntervalSeconds) * time.Second
}

// waitHostSpacing keeps concurrent fetches of one provider host at least
// interval apart, honoring limits such as GDELT's one-request-per-five-seconds.
func (fetcher *Fetcher) waitHostSpacing(
	ctx context.Context,
	host string,
	interval time.Duration,
) error {
	if interval <= 0 {
		return nil
	}
	for {
		fetcher.spacingMu.Lock()
		last, seen := fetcher.lastHostRequest[host]
		now := fetcher.now()
		if !seen || now.Sub(last) >= interval {
			fetcher.lastHostRequest[host] = now
			fetcher.spacingMu.Unlock()
			return nil
		}
		wait := last.Add(interval).Sub(now)
		fetcher.spacingMu.Unlock()
		if err := fetcher.sleep(ctx, wait); err != nil {
			return err
		}
	}
}
