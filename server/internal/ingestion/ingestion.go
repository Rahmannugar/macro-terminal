// Package ingestion implements the scheduled fetch layer of the pipeline:
// adapters turn provider payloads into raw results, the fetcher applies
// timeouts, retries, and circuit breaking, and the runner schedules each
// source configuration at its cadence.
package ingestion

import (
	"errors"
	"time"
)

var (
	// ErrMalformed marks a provider response that does not match the expected
	// format. Malformed responses are not retried, unlike temporary provider
	// failures (timeouts, rate limits, 5xx), which are.
	ErrMalformed = errors.New("malformed provider response")
	// ErrSecretMissing marks configuration referencing an environment variable
	// that is not set; the source is skipped until the deployment provides it.
	ErrSecretMissing = errors.New("required environment variable is not set")
	// ErrCircuitOpen marks a fetch skipped because the provider's circuit
	// breaker is open.
	ErrCircuitOpen = errors.New("circuit breaker is open")
	// ErrUnsupportedAdapter marks a configuration type without an adapter.
	ErrUnsupportedAdapter = errors.New("unsupported source configuration type")
	// ErrInsecureURL rejects configuration URLs outside http/https.
	ErrInsecureURL = errors.New("source URL must use http or https")
	// ErrInvalidURL marks configuration whose URL cannot be parsed or has no host.
	ErrInvalidURL = errors.New("source URL is invalid")
)

// Item is one entry from a feed-shaped source (RSS/Atom).
type Item struct {
	GUID      string
	Title     string
	URL       string
	Summary   string
	Published time.Time
}

// Result is a successful fetch of one source configuration.
type Result struct {
	Items      []Item
	Body       []byte
	StatusCode int
	Attempts   int
	FetchedAt  time.Time
}

// Failure is the provider-failure classification carried by fetch errors.
// The fetcher decides retries from these methods instead of matching on
// strings; unknown (non-Failure) errors are treated as terminal.
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
