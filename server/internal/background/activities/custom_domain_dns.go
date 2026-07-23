package activities

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

func checkCustomDomainRouting(ctx context.Context, resolver dns.Resolver, domain, expectedTarget string) (customdomains.HealthIssue, error) {
	normalizedExpectedTarget := normalizeDNSName(expectedTarget)
	cname, cnameErr := resolver.LookupCNAME(ctx, domain)
	if cnameErr == nil {
		normalizedCNAME := normalizeDNSName(cname)
		if normalizedCNAME == normalizedExpectedTarget {
			return "", nil
		}
		if normalizedCNAME != normalizeDNSName(domain) {
			return customdomains.HealthIssueDNSTargetMismatch, nil
		}
	}

	domainAddresses, domainErr := resolver.LookupHost(ctx, domain)
	if domainErr != nil {
		var dnsErr *net.DNSError
		if errors.As(domainErr, &dnsErr) && dnsErr.IsNotFound {
			return customdomains.HealthIssueDNSNotFound, nil
		}
		return "", fmt.Errorf("resolve custom domain addresses: %w", domainErr)
	}

	expectedAddresses, err := resolver.LookupHost(ctx, normalizedExpectedTarget)
	if err != nil {
		return "", fmt.Errorf("resolve expected custom domain target: %w", err)
	}
	addressSet := make(map[string]struct{}, len(expectedAddresses))
	for _, address := range expectedAddresses {
		addressSet[address] = struct{}{}
	}
	for _, address := range domainAddresses {
		if _, ok := addressSet[address]; ok {
			return "", nil
		}
	}

	return customdomains.HealthIssueDNSTargetMismatch, nil
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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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
