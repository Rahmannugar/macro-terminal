package emaildelivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	resendEndpoint   = "https://api.resend.com/emails"
	maximumErrorBody = 64 << 10
	// resendRequestSpacing paces request starts below Resend's account-wide
	// rate limit; a single worker instance relies on this local spacing.
	resendRequestSpacing = 125 * time.Millisecond
)

type ResendSender struct {
	httpClient *http.Client
	apiKey     string
	from       string
	endpoint   string
	rateMu     sync.Mutex
	nextSendAt time.Time
}

func NewResendSender(httpClient *http.Client, apiKey string, from string) (*ResendSender, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("HTTP client is required for Resend")
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required for Resend")
	}
	from = strings.TrimSpace(from)
	if _, err := mail.ParseAddress(from); err != nil {
		return nil, fmt.Errorf("parse Resend sender: %w", err)
	}
	return &ResendSender{
		httpClient: httpClient, apiKey: apiKey, from: from, endpoint: resendEndpoint,
	}, nil
}

func (sender *ResendSender) Send(
	ctx context.Context,
	recipient string,
	subject string,
	text string,
	idempotencyKey string,
) (string, error) {
	if err := sender.waitForSendSlot(ctx); err != nil {
		return "", newSendFailure(err, true, 0)
	}
	payload, err := json.Marshal(map[string]any{
		"from": sender.from, "to": []string{recipient}, "subject": subject, "text": text,
	})
	if err != nil {
		return "", newSendFailure(fmt.Errorf("encode Resend email: %w", err), false, 0)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, sender.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", newSendFailure(fmt.Errorf("create Resend request: %w", err), false, 0)
	}
	request.Header.Set("Authorization", "Bearer "+sender.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey)

	response, err := sender.httpClient.Do(request)
	if err != nil {
		return "", newSendFailure(fmt.Errorf("send email through Resend: %w", err), true, 0)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maximumErrorBody))
	if readErr != nil {
		return "", newSendFailure(fmt.Errorf("read Resend response: %w", readErr), true, 0)
	}
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		var sent struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(body, &sent); err != nil || strings.TrimSpace(sent.ID) == "" {
			return "", newSendFailure(fmt.Errorf("decode successful Resend response"), true, 0)
		}
		return sent.ID, nil
	}

	var providerError struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &providerError)
	retryable := response.StatusCode == http.StatusRequestTimeout ||
		response.StatusCode == http.StatusTooEarly ||
		response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode >= http.StatusInternalServerError ||
		(response.StatusCode == http.StatusConflict && providerError.Name == "concurrent_idempotent_requests")
	retryAfter := parseRetryAfter(response.Header.Get("Retry-After"), time.Now().UTC())
	return "", newSendFailure(
		fmt.Errorf("email rejected by Resend: status=%d code=%s", response.StatusCode, boundedCode(providerError.Name)),
		retryable,
		retryAfter,
	)
}

func (sender *ResendSender) waitForSendSlot(ctx context.Context) error {
	sender.rateMu.Lock()
	now := time.Now()
	reservedAt := sender.nextSendAt
	if reservedAt.Before(now) {
		reservedAt = now
	}
	sender.nextSendAt = reservedAt.Add(resendRequestSpacing)
	sender.rateMu.Unlock()
	if delay := time.Until(reservedAt); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

type SendFailure interface {
	error
	Retryable() bool
	RetryAfter() time.Duration
}

type sendFailure struct {
	err        error
	retryable  bool
	retryAfter time.Duration
}

func newSendFailure(err error, retryable bool, retryAfter time.Duration) *sendFailure {
	return &sendFailure{err: err, retryable: retryable, retryAfter: retryAfter}
}

func (failure *sendFailure) Error() string             { return failure.err.Error() }
func (failure *sendFailure) Unwrap() error             { return failure.err }
func (failure *sendFailure) Retryable() bool           { return failure.retryable }
func (failure *sendFailure) RetryAfter() time.Duration { return failure.retryAfter }

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

func boundedCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if len(value) > 100 {
		return value[:100]
	}
	return value
}
