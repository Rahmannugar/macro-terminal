package ai

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	quotaCooldown   = time.Minute
	serviceCooldown = 30 * time.Second
)

type failoverTransport struct {
	next    http.RoundTripper
	keys    [2]string
	mu      sync.Mutex
	blocked [2]time.Time
	now     func() time.Time
}

// WithBackupKey gives Gemini requests one attempt with an independent key when the first project is throttled or unavailable.
func WithBackupKey(client *http.Client, primary, backup string) *http.Client {
	primary = strings.TrimSpace(primary)
	backup = strings.TrimSpace(backup)
	if client == nil || primary == "" || backup == "" || primary == backup {
		return client
	}
	copy := *client
	// Gemini operations set their own request deadlines; the client timeout must not cut off the backup attempt.
	copy.Timeout = 0
	next := copy.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	copy.Transport = &failoverTransport{
		next: next,
		keys: [2]string{primary, backup},
		now:  time.Now,
	}
	return &copy
}

func (transport *failoverTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	first := transport.available(-1)
	if first < 0 {
		return nil, errors.New("gemini projects are cooling down")
	}
	response, err := transport.send(request, first, false)
	if err != nil {
		return response, err
	}
	if !retryableStatus(response.StatusCode) {
		return response, nil
	}
	transport.cooldown(first, response.StatusCode)
	second := transport.available(first)
	if err := request.Context().Err(); err != nil {
		_ = response.Body.Close()
		return nil, err
	}
	if second < 0 || (request.Body != nil && request.GetBody == nil) {
		return response, nil
	}
	_ = response.Body.Close()
	response, err = transport.send(request, second, true)
	if err == nil && retryableStatus(response.StatusCode) {
		transport.cooldown(second, response.StatusCode)
	}
	return response, err
}

func (transport *failoverTransport) send(request *http.Request, keyIndex int, replay bool) (*http.Response, error) {
	copy := request.Clone(request.Context())
	if replay && request.Body != nil {
		body, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		copy.Body = body
	}
	if copy.Header == nil {
		copy.Header = make(http.Header)
	}
	copy.Header.Set("x-goog-api-key", transport.keys[keyIndex])
	return transport.next.RoundTrip(copy)
}

func (transport *failoverTransport) available(exclude int) int {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	now := transport.now()
	for index := range transport.keys {
		if index != exclude && !now.Before(transport.blocked[index]) {
			return index
		}
	}
	return -1
}

func (transport *failoverTransport) cooldown(index, status int) {
	duration := serviceCooldown
	if status == http.StatusTooManyRequests {
		duration = quotaCooldown
	}
	transport.mu.Lock()
	transport.blocked[index] = transport.now().Add(duration)
	transport.mu.Unlock()
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable
}
