package ai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBackupKeyRetriesProviderOverloadWithIndependentKey(t *testing.T) {
	var keys []string
	client := &http.Client{Transport: embeddingRoundTrip(func(request *http.Request) (*http.Response, error) {
		keys = append(keys, request.Header.Get("x-goog-api-key"))
		body, err := io.ReadAll(request.Body)
		_ = request.Body.Close()
		if err != nil || string(body) != `{"request":"same"}` {
			t.Errorf("request body = %q, error = %v", body, err)
		}
		status := http.StatusOK
		if request.Header.Get("x-goog-api-key") == "primary" {
			status = http.StatusServiceUnavailable
		}
		return &http.Response{StatusCode: status, Body: http.NoBody, Header: make(http.Header), Request: request}, nil
	})}
	client = WithBackupKey(client, "primary", "backup")
	for range 2 {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.com/model", bytes.NewBufferString(`{"request":"same"}`))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", response.StatusCode)
		}
	}
	if len(keys) != 4 || keys[0] != "primary" || keys[1] != "backup" || keys[2] != "primary" || keys[3] != "backup" {
		t.Fatalf("key order = %v, want primary/backup attempts without a local cooldown", keys)
	}
}

func TestBackupKeyDoesNotRetryClientErrors(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls++
		writer.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	client := WithBackupKey(server.Client(), "primary", "backup")
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if calls != 1 || response.StatusCode != http.StatusBadRequest {
		t.Fatalf("calls = %d, status = %d, want one 400", calls, response.StatusCode)
	}
}
