// Package ingestion fetches source data on a schedule.
//
// The runner picks which sources are due, the fetcher performs the request
// (with retries, host spacing, and a circuit breaker), and an adapter turns
// the response body into a Result.
package ingestion

import (
	"errors"
	"time"
)

var (
	// ErrMalformed means the provider answered, but the body is not the format
	// we expect. Retrying cannot fix a wrong format, so malformed responses
	// are never retried (timeouts, rate limits, and 5xx errors are).
	ErrMalformed = errors.New("malformed provider response")
	// ErrSecretMissing means the configuration names an environment variable
	// that is not set. The fetch is skipped until the variable exists.
	ErrSecretMissing = errors.New("required environment variable is not set")
	// ErrCircuitOpen means the fetch was skipped because this source kept
	// failing and its breaker is cooling down.
	ErrCircuitOpen = errors.New("circuit breaker is open")
	// ErrUnsupportedAdapter means the configuration type has no adapter
	// (known types: api, rss, web).
	ErrUnsupportedAdapter = errors.New("unsupported source configuration type")
	// ErrInsecureURL means the URL uses a scheme other than http or https.
	ErrInsecureURL = errors.New("source URL must use http or https")
	// ErrInvalidURL means the URL is missing, unparsable, or has no host.
	ErrInvalidURL = errors.New("source URL is invalid")
	// ErrInvalidConfiguration means the configuration keys a provider
	// requires are missing or have the wrong shape.
	ErrInvalidConfiguration = errors.New("source configuration is invalid")
)

// Item is one entry from an RSS/Atom feed.
type Item struct {
	GUID      string
	Title     string
	URL       string
	Content   string
	Summary   string
	Published time.Time
}

// Result is a successful fetch of one configuration. Feed adapters fill
// Items; API and web adapters keep the raw bytes in Body. BaseURL is the
// request the response came from — the base for resolving relative links.
type Result struct {
	Items      []Item
	Body       []byte
	BaseURL    string
	StatusCode int
	Attempts   int
	FetchedAt  time.Time
}

// Failure classifies fetch errors so the fetcher can decide what to retry
// by asking Retryable() and RetryAfter() instead of matching error text.
// Errors that do not implement Failure are treated as permanent.
type Failure interface {
	error
	Retryable() bool
	RetryAfter() time.Duration
}

type fetchError struct {
	cause      error
	statusCode int
	retryable  bool
	retryAfter time.Duration
}

func (e *fetchError) Error() string             { return e.cause.Error() }
func (e *fetchError) Unwrap() error             { return e.cause }
func (e *fetchError) Retryable() bool           { return e.retryable }
func (e *fetchError) RetryAfter() time.Duration { return e.retryAfter }

func retryable(err error) bool {
	var failure Failure
	return errors.As(err, &failure) && failure.Retryable()
}

func retryAfter(err error) time.Duration {
	var failure Failure
	if errors.As(err, &failure) {
		return failure.RetryAfter()
	}
	return 0
}
