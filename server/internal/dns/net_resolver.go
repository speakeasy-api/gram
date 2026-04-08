package dns

import (
	"context"
	"net"
	"net/netip"
)

// NetResolver implements [Resolver] by delegating to a [net.Resolver].
type NetResolver struct {
	resolver *net.Resolver
}

// NewNetResolver returns a [NetResolver] backed by [net.DefaultResolver].
func NewNetResolver() *NetResolver {
	return &NetResolver{resolver: net.DefaultResolver}
}

func (n *NetResolver) Resolver() *net.Resolver { return n.resolver }

func (n *NetResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	return n.resolver.LookupAddr(ctx, addr)
}

func (n *NetResolver) LookupCNAME(ctx context.Context, host string) (string, error) {
	return n.resolver.LookupCNAME(ctx, host)
}

func (n *NetResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return n.resolver.LookupHost(ctx, host)
}

func (n *NetResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	return n.resolver.LookupIP(ctx, network, host)
}

func (n *NetResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return n.resolver.LookupIPAddr(ctx, host)
}

func (n *NetResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	return n.resolver.LookupMX(ctx, name)
}

func (n *NetResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return n.resolver.LookupNetIP(ctx, network, host)
}

func (n *NetResolver) LookupNS(ctx context.Context, name string) ([]*net.NS, error) {
	return n.resolver.LookupNS(ctx, name)
}

func (n *NetResolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	return n.resolver.LookupSRV(ctx, service, proto, name)
}

func (n *NetResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return n.resolver.LookupTXT(ctx, name)
}
