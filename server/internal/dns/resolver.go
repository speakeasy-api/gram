package dns

import (
	"context"
	"net"
	"net/netip"
)

// Resolver abstracts DNS lookups for testability. The lookup method signatures
// match the corresponding methods on [net.Resolver]. Use [NewNetResolver] for
// production and [NewMockResolver] for tests.
type Resolver interface {
	LookupAddr(ctx context.Context, addr string) ([]string, error)
	LookupCNAME(ctx context.Context, host string) (string, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
	LookupNS(ctx context.Context, name string) ([]*net.NS, error)
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)

	// Resolver returns the underlying *[net.Resolver]. For [NetResolver] this
	// is the real resolver; for [MockResolver] it is a resolver whose Dial
	// function routes queries through the configured mock functions.
	Resolver() *net.Resolver
}
