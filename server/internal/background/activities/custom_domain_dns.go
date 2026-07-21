package activities

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/dns"
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
