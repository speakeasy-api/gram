package activities

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// customDomainProbeTimeout bounds the HTTPS probe so a black-holed domain
// cannot consume the health check activity's full start-to-close timeout.
const customDomainProbeTimeout = 10 * time.Second

func checkCustomDomainRouting(ctx context.Context, resolver dns.Resolver, domain, expectedTarget string, expectedARecords []netip.Addr) (customdomains.HealthIssue, error) {
	normalizedExpectedTarget := normalizeDNSName(expectedTarget)
	cnameMatched := false
	cname, cnameErr := resolver.LookupCNAME(ctx, domain)
	if cnameErr == nil {
		normalizedCNAME := normalizeDNSName(cname)
		switch normalizedCNAME {
		case normalizedExpectedTarget:
			// Correct shape — but still judge the served addresses below: a
			// provider serving an (RFC-illegal) A/AAAA alongside the CNAME
			// diverts resolvers that prefer the address records.
			cnameMatched = true
		case normalizeDNSName(domain):
			// Flattened/apex: no real CNAME; judge the address records.
		default:
			return customdomains.HealthIssueDNSTargetMismatch, nil
		}
	}

	domainAddrs, domainErr := resolver.LookupNetIP(ctx, "ip", domain)
	if domainErr != nil {
		if cnameMatched {
			// The routing shape is proven by the CNAME; an address resolution
			// hiccup here is transient or on Gram's side of the delegation.
			return "", nil
		}
		var dnsErr *net.DNSError
		if errors.As(domainErr, &dnsErr) && dnsErr.IsNotFound {
			return customdomains.HealthIssueDNSNotFound, nil
		}
		return "", fmt.Errorf("resolve custom domain addresses: %w", domainErr)
	}

	var domainV4 []netip.Addr
	for _, addr := range domainAddrs {
		addr = addr.Unmap()
		if !addr.Is4() {
			// An AAAA record diverts IPv6-preferring clients somewhere Gram
			// cannot serve, even when every A record is correct.
			return customdomains.HealthIssueDNSTargetMismatch, nil
		}
		domainV4 = append(domainV4, addr)
	}
	if len(domainV4) == 0 {
		if cnameMatched {
			return "", nil
		}
		return customdomains.HealthIssueDNSNotFound, nil
	}

	allowed := make(map[netip.Addr]struct{}, len(expectedARecords))
	for _, addr := range expectedARecords {
		allowed[addr.Unmap()] = struct{}{}
	}
	expectedAddrs, err := resolver.LookupNetIP(ctx, "ip", normalizedExpectedTarget)
	if err != nil {
		// The configured static IPs stand on their own; live resolution of the
		// CNAME target is only a supplement when they are absent.
		if len(allowed) == 0 {
			if cnameMatched {
				return "", nil
			}
			return "", fmt.Errorf("resolve expected custom domain target: %w", err)
		}
	}
	for _, addr := range expectedAddrs {
		allowed[addr.Unmap()] = struct{}{}
	}

	// Every published A record must point at Gram: a partially wrong RRset
	// sends a share of traffic elsewhere and must not read as healthy.
	for _, addr := range domainV4 {
		if _, ok := allowed[addr]; !ok {
			return customdomains.HealthIssueDNSTargetMismatch, nil
		}
	}
	return "", nil
}

func checkCustomDomainCAA(ctx context.Context, resolver dns.Resolver, domain string) (customdomains.HealthIssue, error) {
	restriction, err := dns.FindIssueRestriction(ctx, resolver, domain)
	if err != nil {
		return "", fmt.Errorf("resolve custom domain CAA: %w", err)
	}
	if restriction.Name == "" || dns.IssueAllows(restriction.Records, dns.LetsEncryptIssueDomain) {
		return "", nil
	}
	return customdomains.HealthIssueCAAForbidden, nil
}

func normalizeDNSName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

// probeCustomDomainHTTPS reports whether the domain answers HTTPS with a
// certificate valid for its hostname. DNS shape alone cannot distinguish a
// proxied/CDN domain that still routes traffic (e.g. a flattened CNAME serving
// proxy IPs) from one that is genuinely misconfigured, so a successful probe
// overrides a dns_target_mismatch observation. Any HTTP status counts as
// success: the signal is that something terminated TLS for this hostname, not
// what it said.
func probeCustomDomainHTTPS(ctx context.Context, client *guardian.HTTPClient, domain string) error {
	ctx, cancel := context.WithTimeout(ctx, customDomainProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/mcp", nil)
	if err != nil {
		return fmt.Errorf("build custom domain probe request: %w", err)
	}

	// Do not follow redirects: the response must come from the domain itself.
	probeClient := *client
	probeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := probeClient.Do(req)
	if err != nil {
		return fmt.Errorf("probe custom domain over https: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	return nil
}
