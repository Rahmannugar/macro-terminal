package ingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/safehttp"
	"github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// instrumentationPath names the traces this package emits.
const instrumentationPath = "github.com/Rahmannugar/macro-terminal/server/internal/ingestion"

const (
	// maxAttempts is how many times one fetch is tried before giving up.
	maxAttempts = 3
	// baseRetryDelay is the wait before the first retry; it doubles after
	// each attempt.
	baseRetryDelay = 500 * time.Millisecond
	// maxRetryDelay caps every wait, including one the provider asks for
	// through Retry-After.
	maxRetryDelay = 30 * time.Second
	// maxResponseBytes caps how much of a response is read, so a runaway
	// provider cannot eat worker memory.
	maxResponseBytes = 16 << 20
	// userAgent identifies the terminal to providers.
	userAgent = "macro-terminal/1.0 (+https://macroterminal.consumel.com)"
)

// HTTPDoer is the part of http.Client the fetcher needs: just Do.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Fetcher performs provider requests: it builds the request, spaces out
// calls to the same host, retries with backoff, and records results against
// the circuit breaker.
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
	// checkDestination validates configuration URLs before the first
	// attempt: safehttp.CheckDestination in production, relaxed by tests
	// to reach loopback fixtures.
	checkDestination func(host, port string) error

	spacingMu       sync.Mutex
	lastHostRequest map[string]time.Time
}

// NewFetcher builds a fetcher on telemetry's traced HTTP client.
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

		checkDestination: safehttp.CheckDestination,

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

// Fetch retrieves and parses one source configuration. Errors that happen
// while building the request (bad configuration, missing secret) never
// reach the provider and do not count against the breaker.
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
	// A POST body is consumed by the previous attempt; rebuild it from the
	// saved snapshot so every retry sends the identical payload.
	if request.GetBody != nil {
		body, err := request.GetBody()
		if err != nil {
			return Result{}, fmt.Errorf("rebuild request body: %w", err)
		}
		attemptRequest.Body = body
	}
	response, err := fetcher.client.Do(attemptRequest)
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if errors.Is(err, safehttp.ErrBlockedAddress) {
			return Result{}, &fetchError{
				cause:     fmt.Errorf("request blocked: %w", err),
				retryable: false,
			}
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
		result.BaseURL = request.URL.String()
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

// buildRequest turns configuration JSON into the request to send: the URL,
// string parameters (provider keys like GDELT's query/language), and
// `*_env`/`*_param` pairs that fill in secrets from environment variables.
// Secrets are never stored in the configuration itself. The BLS source is
// the one provider that is not a plain GET: it refuses the bare collection
// URL and wants series selection in a JSON body.
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

	target, err := url.Parse(rawURL)
	if err != nil || target.Host == "" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidURL, rawURL)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("%w: %s", ErrInsecureURL, target.Scheme)
	}
	if fetcher.checkDestination != nil {
		if err := fetcher.checkDestination(target.Hostname(), target.Port()); err != nil {
			return nil, err
		}
	}

	var request *http.Request
	if configuration.SourceName == blsSourceName {
		request, err = buildBLSRequest(ctx, target, document)
	} else {
		request, err = buildGETRequest(ctx, target, document)
	}
	if err != nil {
		return nil, err
	}

	request.Header.Set("User-Agent", userAgent)
	if envName, ok := document["bearer_env"].(string); ok && strings.TrimSpace(envName) != "" {
		envName = strings.TrimSpace(envName)
		secret := os.Getenv(envName)
		if secret == "" {
			return nil, fmt.Errorf("%w: %s", ErrSecretMissing, envName)
		}
		request.Header.Set("Authorization", "Bearer "+secret)
	}
	// SDMX statistics gateways answer 500 when Accept includes
	// application/json, so a configuration can override this header.
	accept := acceptFor(configuration.Type)
	if value, ok := document["accept"].(string); ok && strings.TrimSpace(value) != "" {
		accept = value
	}
	request.Header.Set("Accept", accept)
	return request, nil
}

// buildGETRequest assembles the query string from configuration keys and
// sends a GET.
func buildGETRequest(ctx context.Context, target *url.URL, document map[string]any) (*http.Request, error) {
	query, err := configurationQuery(document)
	if err != nil {
		return nil, err
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
	return request, nil
}

// blsSourceName identifies the provider whose data endpoint refuses a bare
// GET: without a series ID in the path it answers 405, so multi-series
// requests travel as a JSON POST body instead of query parameters.
const blsSourceName = "BLS"

// buildBLSRequest turns the configuration's series selection into the JSON
// body BLS expects. The registration key is optional: BLS answers without
// one, just within tighter limits, so an unset environment variable is not
// an error here.
func buildBLSRequest(ctx context.Context, target *url.URL, document map[string]any) (*http.Request, error) {
	rawSeries, ok := document["seriesid"].([]any)
	if !ok || len(rawSeries) == 0 {
		return nil, fmt.Errorf(
			"%w: %s configuration needs a non-empty seriesid array", ErrInvalidConfiguration, blsSourceName,
		)
	}
	series := make([]string, 0, len(rawSeries))
	for _, raw := range rawSeries {
		seriesID, ok := raw.(string)
		if !ok || strings.TrimSpace(seriesID) == "" {
			return nil, fmt.Errorf(
				"%w: %s seriesid entries must be non-empty strings", ErrInvalidConfiguration, blsSourceName,
			)
		}
		series = append(series, strings.TrimSpace(seriesID))
	}

	body := map[string]any{"seriesid": series}
	for _, key := range []string{"startyear", "endyear"} {
		raw, present := document[key]
		if !present {
			continue
		}
		value, ok := raw.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf(
				"%w: %s %s must be a four-digit year string", ErrInvalidConfiguration, blsSourceName, key,
			)
		}
		body[key] = strings.TrimSpace(value)
	}
	if envName, ok := document["registrationkey_env"].(string); ok && strings.TrimSpace(envName) != "" {
		if key := os.Getenv(strings.TrimSpace(envName)); key != "" {
			body["registrationkey"] = key
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode %s request body: %w", blsSourceName, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	return request, nil
}

func configurationQuery(document map[string]any) (url.Values, error) {
	query := url.Values{}
	for key, raw := range document {
		switch {
		case key == "url" || key == "min_interval_s" || key == "accept" ||
			key == "candle" || key == "bearer_env":
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

// configurationInterval reads the optional min_interval_s (minimum seconds
// between requests to one host) from configuration; 0 when unset.
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

// waitHostSpacing keeps requests to the same host at least interval apart —
// GDELT allows one request per five seconds, for example. Each caller
// claims a timestamp slot, so concurrent fetches to one host queue up
// instead of colliding.
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
