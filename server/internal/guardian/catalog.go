package guardian

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
)

// catalogHTTPSPort is the only socket port exposed by the catalog ILB.
const catalogHTTPSPort = 443

type catalogRule struct {
	destination netip.AddrPort
}

// WithInternalCatalogCIDR configures the catalog destination without granting
// access to ordinary clients. Empty disables it; only a private IPv4 /32 is
// accepted. MCP clients must also opt in with WithInternalCatalog.
func WithInternalCatalogCIDR(cidr string) (func(*Policy), error) {
	var rule *catalogRule
	if cidr != "" {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || !prefix.Addr().Is4() || !prefix.Addr().IsPrivate() || !prefix.IsSingleIP() {
			return nil, fmt.Errorf("internal catalog destination must be a private IPv4 /32")
		}
		rule = &catalogRule{destination: netip.AddrPortFrom(prefix.Addr(), catalogHTTPSPort)}
	}
	return func(p *Policy) { p.internalCatalog = rule }, nil
}

// WithInternalCatalog enables the configured catalog rule for an MCP client
// or URL validation. Other destinations retain the policy's normal IP protections.
// URL validation only consumes this option; dialing options do not relax it.
func WithInternalCatalog() ClientOption {
	return func(o *httpClientOptions) { o.internalCatalog = true }
}

func catalogHTTPSURL(u *url.URL) bool {
	return u.Scheme == "https" && (u.Port() == "" || u.Port() == "443") && !strings.HasSuffix(u.Host, ":")
}

func (p *Policy) checkCatalogIP(ip net.IP) error {
	if p.internalCatalog.permits(net.JoinHostPort(ip.String(), "443")) {
		return nil
	}
	return p.checkIP(ip)
}

func (r *catalogRule) permits(address string) bool {
	addr, err := netip.ParseAddrPort(address)
	return err == nil && netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port()) == r.destination
}

// catalogTLSDialer permits only the configured socket in addition to the base
// policy. HTTP uses the ordinary dialer; tls.Dialer verifies the certificate
// against the requested hostname, including on redirected connections.
func (p *Policy) catalogTLSDialer(base *net.Dialer, config *tls.Config) *tls.Dialer {
	dialer := *base
	dialer.ControlContext = func(ctx context.Context, network, address string, conn syscall.RawConn) error {
		if p.internalCatalog.permits(address) {
			return nil
		}
		return base.ControlContext(ctx, network, address, conn)
	}
	return &tls.Dialer{NetDialer: &dialer, Config: config}
}
