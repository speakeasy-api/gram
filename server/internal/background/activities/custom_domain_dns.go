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

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// customDomainProbeTimeout bounds the HTTPS probe so a black-holed domain
// cannot consume the health check activity's full start-to-close timeout.
const customDomainProbeTimeout = 10 * time.Second

// customDomainRoutingTargets are the DNS targets a custom domain may route
// through.
type customDomainRoutingTargets struct {
	// SharedCNAME is the normalized CNAME target every custom domain may use.
	// Empty when unconfigured.
	SharedCNAME string

	// ARecords are the static ingress IPs apex domains may point A records at.
	ARecords []netip.Addr

	// TargetSuffix is the normalized parent DNS name of per-domain targets.
	// Empty disables per-domain targets.
	TargetSuffix string

	// DomainTarget is the domain's own per-domain target (<token>.<suffix>).
	// Empty when per-domain targets are disabled or the domain has no token.
	DomainTarget string
}

func newCustomDomainRoutingTargets(sharedCNAME string, aRecords []netip.Addr, targetSuffix string, token pgtype.Text) customDomainRoutingTargets {
	domainTarget, _ := customdomains.DNSTarget(targetSuffix, token)
	return customDomainRoutingTargets{
		SharedCNAME:  normalizeDNSName(sharedCNAME),
		ARecords:     aRecords,
		TargetSuffix: targetSuffix,
		DomainTarget: domainTarget,
	}
}

// customDomainRouting is the outcome of one routing check.
type customDomainRouting struct {
	// Issue is empty when the domain's DNS routes to Speakeasy.
	Issue customdomains.HealthIssue

	// Method is the routing method the domain's DNS uses. A CNAME to a known
	// target keeps its method even when stray address records make it a
	// mismatch, so the method shows what the customer configured.
	Method customdomains.DNSRoutingMethod

	// OtherDNSTarget reports a CNAME to a per-domain target that is not this
	// domain's (another domain's, or one released when a domain was deleted).
	// Traffic follows that other target wherever it is moved, so the HTTPS
	// probe must not clear the mismatch.
	OtherDNSTarget bool
}

func checkCustomDomainRouting(ctx context.Context, resolver dns.Resolver, domain string, targets customDomainRoutingTargets) (customDomainRouting, error) {
	var noRouting customDomainRouting

	method := customdomains.DNSRoutingMethodUnknown
	cname, cnameErr := resolver.LookupCNAME(ctx, domain)
	if cnameErr == nil {
		normalizedCNAME := normalizeDNSName(cname)
		// The shared target is matched first so a suffix that also parents the
		// shared target cannot read it as another domain's per-domain target.
		// A matching CNAME is the correct shape, but the served addresses are
		// still judged below: a provider serving an (RFC-illegal) A/AAAA
		// alongside the CNAME diverts resolvers that prefer the address records.
		switch {
		case targets.SharedCNAME != "" && normalizedCNAME == targets.SharedCNAME:
			method = customdomains.DNSRoutingMethodSharedCNAME
		case targets.DomainTarget != "" && normalizedCNAME == targets.DomainTarget:
			method = customdomains.DNSRoutingMethodPerDomainCNAME
		case normalizedCNAME == "" || normalizedCNAME == normalizeDNSName(domain):
			// Flattened/apex: no real CNAME; judge the address records.
		default:
			_, other := customdomains.ParseDNSTarget(targets.TargetSuffix, normalizedCNAME)
			return customDomainRouting{
				Issue:          customdomains.HealthIssueDNSTargetMismatch,
				Method:         customdomains.DNSRoutingMethodUnknown,
				OtherDNSTarget: other,
			}, nil
		}
	}
	cnameMatched := method != customdomains.DNSRoutingMethodUnknown
	routed := customDomainRouting{Issue: "", Method: method, OtherDNSTarget: false}
	mismatch := customDomainRouting{Issue: customdomains.HealthIssueDNSTargetMismatch, Method: method, OtherDNSTarget: false}

	domainAddrs, domainErr := resolver.LookupNetIP(ctx, "ip", domain)
	if domainErr != nil {
		if cnameMatched {
			// The routing shape is proven by the CNAME; an address resolution
			// hiccup here is transient or on Speakeasy's side of the delegation.
			return routed, nil
		}
		var dnsErr *net.DNSError
		if errors.As(domainErr, &dnsErr) && dnsErr.IsNotFound {
			return customDomainRouting{Issue: customdomains.HealthIssueDNSNotFound, Method: method, OtherDNSTarget: false}, nil
		}
		return noRouting, fmt.Errorf("resolve custom domain addresses: %w", domainErr)
	}

	var domainV4 []netip.Addr
	for _, addr := range domainAddrs {
		addr = addr.Unmap()
		if !addr.Is4() {
			// An AAAA record diverts IPv6-preferring clients somewhere Speakeasy
			// cannot serve, even when every A record is correct.
			return mismatch, nil
		}
		domainV4 = append(domainV4, addr)
	}
	if len(domainV4) == 0 {
		if cnameMatched {
			return routed, nil
		}
		return customDomainRouting{Issue: customdomains.HealthIssueDNSNotFound, Method: method, OtherDNSTarget: false}, nil
	}

	allowed := make(map[netip.Addr]struct{}, len(targets.ARecords))
	for _, addr := range targets.ARecords {
		allowed[addr.Unmap()] = struct{}{}
	}

	// The configured static IPs stand on their own; live resolution of the
	// targets is a supplement that also covers a per-domain target moved to
	// another data plane, whose addresses are not in the static list.
	var resolveErr error
	if targets.SharedCNAME != "" {
		sharedAddrs, err := resolver.LookupNetIP(ctx, "ip", targets.SharedCNAME)
		if err != nil {
			resolveErr = fmt.Errorf("resolve shared custom domain target: %w", err)
		}
		for _, addr := range sharedAddrs {
			allowed[addr.Unmap()] = struct{}{}
		}
	}
	if targets.DomainTarget != "" {
		domainTargetAddrs, err := resolver.LookupNetIP(ctx, "ip", targets.DomainTarget)
		if err != nil {
			if method == customdomains.DNSRoutingMethodPerDomainCNAME {
				// The domain points at its own target, which may now serve
				// addresses outside the static list; without them the addresses
				// cannot be judged, and the CNAME already proves the shape.
				return routed, nil
			}
			resolveErr = errors.Join(resolveErr, fmt.Errorf("resolve per-domain custom domain target: %w", err))
		}
		for _, addr := range domainTargetAddrs {
			allowed[addr.Unmap()] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		if cnameMatched {
			return routed, nil
		}
		if resolveErr == nil {
			resolveErr = errors.New("no custom domain targets configured")
		}
		return noRouting, fmt.Errorf("resolve expected custom domain targets: %w", resolveErr)
	}

	// Every published A record must point at Speakeasy: a partially wrong RRset
	// sends a share of traffic elsewhere and must not read as healthy.
	for _, addr := range domainV4 {
		if _, ok := allowed[addr]; !ok {
			return mismatch, nil
		}
	}
	// A failed CNAME lookup leaves the record type unobserved: addresses that
	// arrive through a CNAME to a known target pass the same check.
	if !cnameMatched && cnameErr == nil {
		routed.Method = customdomains.DNSRoutingMethodARecord
	}
	return routed, nil
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
