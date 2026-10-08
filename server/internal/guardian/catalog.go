package guardian

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"unicode"
)

// catalogDomain is the development catalog's routing namespace. Customer
// labels select release channels, not authorization or tenant boundaries.
const catalogDomain = "catalog.dev.speakeasy.com"

// catalogHTTPSPort is the only socket port exposed by the catalog ILB.
const catalogHTTPSPort = 443

// catalogMaxLabelLength is the DNS limit for one customer routing label.
const catalogMaxLabelLength = 63

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
// or URL validation. Other hosts retain the policy's normal IP protections.
// URL validation only consumes this option; dialing options do not relax it.
func WithInternalCatalog() ClientOption {
	return func(o *httpClientOptions) { o.internalCatalog = true }
}

func isCatalogHost(host string) bool {
	// Match ASCII DNS names directly; Unicode case folding must not turn a
	// lookalike into a member of the infrastructure namespace.
	for _, c := range host {
		if c > unicode.MaxASCII {
			return false
		}
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	label, ok := strings.CutSuffix(host, "."+catalogDomain)
	if !ok || len(label) == 0 || len(label) > catalogMaxLabelLength || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, c := range label {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

func checkCatalogURL(u *url.URL) error {
	if !isCatalogHost(u.Hostname()) || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || u.User != nil || strings.HasSuffix(u.Host, ":") {
		return fmt.Errorf("internal catalog requires HTTPS on port 443 without userinfo: %w", ErrBadHost)
	}
	return nil
}

func (p *Policy) validateCatalogURL(ctx context.Context, u *url.URL) error {
	if err := checkCatalogURL(u); err != nil {
		return err
	}
	ips, err := p.resolver.LookupIP(ctx, "ip", u.Hostname())
	if err != nil {
		return fmt.Errorf("resolve internal catalog: %w: %w", ErrBadHost, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("internal catalog has no addresses: %w", ErrBadHost)
	}
	for _, ip := range ips {
		if err := p.internalCatalog.checkDestination(net.JoinHostPort(ip.String(), "443")); err != nil {
			return err
		}
	}
	return nil
}

func (r *catalogRule) checkDestination(address string) error {
	addr, err := netip.ParseAddrPort(address)
	if err != nil || netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port()) != r.destination {
		return fmt.Errorf("internal catalog destination rejected: %w", ErrBlockedIP)
	}
	return nil
}

func (p *Policy) catalogTransport(base *http.Transport, baseDialer *net.Dialer) *http.Transport {
	transport := base.Clone()
	// An environment proxy would resolve/connect on our behalf and defeat the
	// socket check. Catalog traffic must connect directly to the configured ILB.
	transport.Proxy = nil
	dialer := *baseDialer
	dialer.ControlContext = func(_ context.Context, _, address string, _ syscall.RawConn) error {
		return p.internalCatalog.checkDestination(address)
	}
	transport.DialContext = dialer.DialContext
	return transport
}

type catalogRoundTripper struct {
	next    http.RoundTripper
	catalog *http.Transport
}

func (t *catalogRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Select again for every redirect and retry, never carrying an exception
	// from a previous request to a different host, scheme or port.
	transport := t.next
	if isCatalogHost(req.URL.Hostname()) {
		if err := checkCatalogURL(req.URL); err != nil {
			return nil, err
		}
		transport = t.catalog
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return resp, fmt.Errorf("catalog-enabled request: %w", err)
	}
	return resp, nil
}
