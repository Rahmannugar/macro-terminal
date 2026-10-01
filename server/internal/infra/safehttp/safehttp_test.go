package safehttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1",        // loopback
		"10.0.0.7",         // RFC 1918
		"172.16.4.4",       // RFC 1918
		"192.168.1.1",      // RFC 1918
		"169.254.169.254",  // link-local: instance metadata
		"100.64.0.1",       // carrier-grade NAT
		"0.0.0.0",          // unspecified
		"224.0.0.1",        // multicast
		"::1",              // IPv6 loopback
		"fe80::1",          // IPv6 link-local
		"fc00::1",          // IPv6 unique local
		"::ffff:127.0.0.1", // IPv4-mapped loopback
	}
	for _, raw := range blocked {
		if !blockedIP(net.ParseIP(raw)) {
			t.Errorf("blockedIP(%s) = false, want true", raw)
		}
	}

	public := []string{"93.184.216.34", "1.1.1.1", "2606:4700::1111", "8.8.8.8"}
	for _, raw := range public {
		if blockedIP(net.ParseIP(raw)) {
			t.Errorf("blockedIP(%s) = true, want false", raw)
		}
	}
}

func TestCheckDestination(t *testing.T) {
	refused := []struct{ host, port string }{
		{"169.254.169.254", ""},   // metadata literal
		{"127.0.0.1", "80"},       // loopback literal, allowed port
		{"93.184.216.34", "6380"}, // public host, internal-service port
		{"example.com", "5433"},   // public host, database port
		{"[::1]", ""},             // IPv6 loopback literal
	}
	for _, destination := range refused {
		if err := CheckDestination(destination.host, destination.port); !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("CheckDestination(%q, %q) = %v, want ErrBlockedAddress", destination.host, destination.port, err)
		}
	}

	allowed := []struct{ host, port string }{
		{"example.com", ""}, // hostname, scheme default port
		{"example.com", "443"},
		{"93.184.216.34", "80"}, // public literal, web port
	}
	for _, destination := range allowed {
		if err := CheckDestination(destination.host, destination.port); err != nil {
			t.Errorf("CheckDestination(%q, %q) = %v, want nil", destination.host, destination.port, err)
		}
	}
}

func TestTransportBlocksLoopbackProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("loopback provider should never be reached")
	}))
	defer server.Close()

	client := &http.Client{Transport: Transport()}
	response, err := do(client, server.URL)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("request = %v, want ErrBlockedAddress", err)
	}
}

func TestTransportBlocksRedirectIntoInternalAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer server.Close()

	// The test fixture is on loopback, so this policy allows loopback
	// (and any fixture port) while still refusing everything else — the
	// redirect target must fail the same policy on its own connection.
	transport := newTransport(policy{
		lookup: func(_ context.Context, host string) ([]net.IP, error) {
			return []net.IP{net.ParseIP(host)}, nil
		},
		allow:     func(ip net.IP) bool { return ip.IsLoopback() },
		allowPort: func(string) bool { return true },
	})
	client := &http.Client{Transport: transport}
	response, err := do(client, server.URL)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("redirect into metadata address: request = %v, want ErrBlockedAddress", err)
	}
}

func TestTransportFetchesWhenPolicyAllows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	transport := newTransport(policy{
		lookup:    func(_ context.Context, host string) ([]net.IP, error) { return []net.IP{net.ParseIP(host)}, nil },
		allow:     func(net.IP) bool { return true },
		allowPort: func(string) bool { return true },
	})
	client := &http.Client{Transport: transport}
	response, err := do(client, server.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
}

// do issues a GET with an explicit context, as the repo lint rules require.
func do(client *http.Client, target string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(request)
}

func TestDialTriesNextAddressWhenFirstRefuses(t *testing.T) {
	var dialed []string
	testPolicy := policy{
		lookup: func(_ context.Context, _ string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("203.0.113.7"), net.ParseIP("203.0.113.8")}, nil
		},
		allowPort: func(string) bool { return true },
		dial: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			if strings.Contains(address, "203.0.113.7") {
				return nil, errors.New("connection refused")
			}
			connection, _ := net.Pipe()
			return connection, nil
		},
	}

	connection, err := testPolicy.dialChecked(context.Background(), "tcp", "example.test:8080")
	if err != nil {
		t.Fatalf("dialChecked: %v", err)
	}
	_ = connection.Close()
	if len(dialed) != 2 || !strings.Contains(dialed[0], "203.0.113.7") || !strings.Contains(dialed[1], "203.0.113.8") {
		t.Errorf("dialed = %v, want both addresses in order", dialed)
	}
}

func TestDialFailsWhenEveryAddressRefuses(t *testing.T) {
	testPolicy := policy{
		lookup: func(_ context.Context, _ string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("203.0.113.7"), net.ParseIP("203.0.113.8")}, nil
		},
		allowPort: func(string) bool { return true },
		dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("connection refused")
		},
	}

	if _, err := testPolicy.dialChecked(context.Background(), "tcp", "example.test:8080"); err == nil {
		t.Fatal("dialChecked = nil, want the refusal error")
	}
}
