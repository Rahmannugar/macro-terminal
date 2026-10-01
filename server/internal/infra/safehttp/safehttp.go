// Package safehttp builds the worker's outbound HTTP transport: provider
// fetches may only reach public addresses on the standard web ports, so a
// configuration URL or a redirect cannot point the worker at internal
// services or the instance metadata endpoint.
package safehttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// ErrBlockedAddress marks a destination the policy refuses: a non-web port,
// or an address in a loopback, private, link-local, carrier-grade NAT,
// multicast, or unspecified range. A fetch that hits it fails permanently —
// retrying cannot change where a destination points.
var ErrBlockedAddress = errors.New("destination address is not allowed")

// CheckDestination applies the address policy without dialing. The port must
// be a standard web port and a literal IP address must be public; hostnames
// are resolved and checked at connection time.
func CheckDestination(host, port string) error {
	if port != "" && !standardWebPort(port) {
		return fmt.Errorf("%w: port %s", ErrBlockedAddress, port)
	}
	if literal := net.ParseIP(strings.Trim(host, "[]")); literal != nil && blockedIP(literal) {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
	}
	return nil
}

// Transport returns the transport for provider fetches: direct connections,
// no proxy. Every connection, redirects included, resolves the host and
// checks every address before dialing checked addresses only, in order until
// one connects. A fresh DNS answer cannot swap addresses after the check
// (DNS rebinding), and one refusing record does not sink the host.
func Transport() *http.Transport {
	return newTransport(policy{})
}

func newTransport(p policy) *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           p.dialChecked,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// policy holds the outbound rules; zero fields mean the real defaults.
// Tests replace them to reach loopback fixtures.
type policy struct {
	lookup    func(ctx context.Context, host string) ([]net.IP, error)
	allow     func(net.IP) bool
	allowPort func(port string) bool
	dial      func(ctx context.Context, network, address string) (net.Conn, error)
}

func (p policy) dialChecked(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBlockedAddress, address)
	}
	if !p.portAllowed(port) {
		return nil, fmt.Errorf("%w: port %s", ErrBlockedAddress, port)
	}
	ips, err := p.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	for _, ip := range ips {
		if !p.ipAllowed(ip) {
			return nil, fmt.Errorf("%w: %s resolves to %s", ErrBlockedAddress, host, ip)
		}
	}
	// Every address passed the policy; connect to the first that answers.
	var lastErr error
	for _, ip := range ips {
		connection, err := p.dialNow(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func (p policy) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if p.lookup != nil {
		return p.lookup(ctx, host)
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	ips := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		ips = append(ips, address.IP)
	}
	return ips, nil
}

func (p policy) ipAllowed(ip net.IP) bool {
	if p.allow != nil {
		return p.allow(ip)
	}
	return !blockedIP(ip)
}

func (p policy) portAllowed(port string) bool {
	if p.allowPort != nil {
		return p.allowPort(port)
	}
	return standardWebPort(port)
}

func (p policy) dialNow(ctx context.Context, network, address string) (net.Conn, error) {
	if p.dial != nil {
		return p.dial(ctx, network, address)
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

func standardWebPort(port string) bool {
	return port == "80" || port == "443"
}

// blockedIP reports whether an address is internal — not somewhere a public
// provider lives. IPv4-mapped IPv6 forms are checked as their IPv4 address.
func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() ||
		inCarrierGradeNAT(ip)
}

// inCarrierGradeNAT covers 100.64.0.0/10, which net.IP does not classify.
func inCarrierGradeNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
}
