package ai

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/circuitbreaker"
)

type failoverTransport struct {
	next http.RoundTripper
	keys [2]string
}

func WithBackupKey(client *http.Client, primary, backup string) *http.Client {
	primary = strings.TrimSpace(primary)
	backup = strings.TrimSpace(backup)
	if client == nil || primary == "" || backup == "" || primary == backup {
		return client
	}
	copy := *client
	// Each operation owns the deadline shared by both provider attempts.
	copy.Timeout = 0
	next := copy.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	copy.Transport = &failoverTransport{next: next, keys: [2]string{primary, backup}}
	return &copy
}

func (transport *failoverTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.send(request, 0, false)
	retry := err == nil && retryableStatus(response.StatusCode)
	if err != nil {
		pause, paused := circuitbreaker.Paused(err)
		var network net.Error
		if paused {
			retry = pause.Reason != "circuit_store_unavailable"
		} else {
			retry = errors.As(err, &network)
		}
	}
	if !retry || (request.Body != nil && request.GetBody == nil) {
		return response, err
	}
	if contextErr := request.Context().Err(); contextErr != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		return nil, contextErr
	}
	if response != nil {
		_ = response.Body.Close()
	}
	return transport.send(request, 1, true)
}

func (transport *failoverTransport) send(request *http.Request, index int, replay bool) (*http.Response, error) {
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
	copy.Header.Set("x-goog-api-key", transport.keys[index])
	return transport.next.RoundTrip(copy)
}

func retryableStatus(status int) bool { return status == 429 || status == 408 || status >= 500 }
