// Package requestip resolves public client addresses at the trusted edge boundary.
package requestip

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Source identifies which trusted input supplied an address.
type Source string

const (
	// SourceUnknown means neither the edge header nor transport was usable.
	SourceUnknown Source = ""

	// SourceHeader means the edge supplied one valid IP address.
	SourceHeader Source = "header"

	// SourceTransport means resolution fell back to the connection peer.
	SourceTransport Source = "transport"

	// maxAddressBytes bounds parsing, including whitespace around an IPv6 address.
	maxAddressBytes = 64
)

// Result is a resolved address, including its provenance. An invalid Addr is
// unresolved; it must not trigger a fallback to caller-supplied forwarding headers.
type Result struct {
	// Addr is the normalized address, or the zero value when unresolved.
	Addr netip.Addr

	// Source distinguishes edge attribution from transport fallback.
	Source Source
}

// String returns the address without a port, or empty when unresolved.
func (r Result) String() string {
	if !r.Addr.IsValid() {
		return ""
	}
	return r.Addr.String()
}

// Resolve accepts exactly one IP, never a list or an address with a port or zone.
// Every public ingress must overwrite X-Client-IP before this application
// deploys, including nginx during a mixed ingress rollout (AGE-3644).
// Private ingress must not use this resolver.
// Missing, malformed, and repeated headers fall back to RemoteAddr. Allowlisting
// the transport proxy's address therefore also permits fallback requests.
func Resolve(req *http.Request) Result {
	values := req.Header.Values("X-Client-IP")
	if len(values) == 1 {
		if addr := parseAddress(values[0]); addr.IsValid() {
			return Result{Addr: addr, Source: SourceHeader}
		}
	}
	host := req.RemoteAddr
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	if addr := parseAddress(host); addr.IsValid() {
		return Result{Addr: addr, Source: SourceTransport}
	}
	return Result{Addr: netip.Addr{}, Source: SourceUnknown}
}

func parseAddress(value string) netip.Addr {
	if len(value) > maxAddressBytes {
		return netip.Addr{}
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || addr.Zone() != "" {
		return netip.Addr{}
	}
	return addr.Unmap()
}

type contextKey struct{}

// WithContext stores the public listener's resolution result, even if unresolved.
func WithContext(ctx context.Context, result Result) context.Context {
	return context.WithValue(ctx, contextKey{}, result)
}

// FromContext returns the public listener's result and whether resolution ran.
func FromContext(ctx context.Context) (Result, bool) {
	result, ok := ctx.Value(contextKey{}).(Result)
	return result, ok
}
