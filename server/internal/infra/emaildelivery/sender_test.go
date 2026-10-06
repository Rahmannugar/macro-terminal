package emaildelivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResendSendPostsEmailWithIdempotencyKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/emails" {
			t.Errorf("path = %q, want /emails", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer re_test_key" {
			t.Errorf("authorization = %q, want the API key", authorization)
		}
		if key := request.Header.Get("Idempotency-Key"); key != "email-delivery/abc" {
			t.Errorf("idempotency key = %q, want the delivery key", key)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var payload struct {
			From    string   `json:"from"`
			To      []string `json:"to"`
			Subject string   `json:"subject"`
			Text    string   `json:"text"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.From != "Macro Terminal <noreply@example.com>" {
			t.Errorf("from = %q", payload.From)
		}
		if len(payload.To) != 1 || payload.To[0] != "reader@example.com" {
			t.Errorf("to = %v", payload.To)
		}
		if payload.Subject != "Your Macro Terminal sign-up code" {
			t.Errorf("subject = %q", payload.Subject)
		}
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusCreated)
		_, _ = response.Write([]byte(`{"id":"email-provider-1"}`))
	}))
	defer server.Close()

	sender := newTestSender(t, server)
	providerID, err := sender.Send(
		context.Background(),
		"reader@example.com",
		"Your Macro Terminal sign-up code",
		"code 123456",
		"email-delivery/abc",
	)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if providerID != "email-provider-1" {
		t.Errorf("provider id = %q", providerID)
	}
}

func TestResendSendClassifiesFailures(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		retryAfter    string
		wantRetryable bool
		wantDelay     time.Duration
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"name":"validation_error"}`, wantRetryable: false},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"name":"rate_limit_exceeded"}`, retryAfter: "30", wantRetryable: true, wantDelay: 30 * time.Second},
		{name: "server error", status: http.StatusInternalServerError, body: `{}`, wantRetryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()

			sender := newTestSender(t, server)
			_, err := sender.Send(context.Background(), "reader@example.com", "subject", "text", "key")
			if err == nil {
				t.Fatal("Send: expected an error")
			}
			var failure SendFailure
			if !errors.As(err, &failure) {
				t.Fatalf("error %v does not carry a send failure policy", err)
			}
			if failure.Retryable() != test.wantRetryable {
				t.Errorf("retryable = %v, want %v", failure.Retryable(), test.wantRetryable)
			}
			if failure.RetryAfter() != test.wantDelay {
				t.Errorf("retry after = %s, want %s", failure.RetryAfter(), test.wantDelay)
			}
		})
	}
}

func newTestSender(t *testing.T, server *httptest.Server) *ResendSender {
	t.Helper()
	sender, err := NewResendSender(server.Client(), "re_test_key", "Macro Terminal <noreply@example.com>")
	if err != nil {
		t.Fatalf("NewResendSender: %v", err)
	}
	sender.endpoint = server.URL + "/emails"
	return sender
}
