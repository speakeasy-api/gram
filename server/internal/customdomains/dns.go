package customdomains

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/dns"
)

// DNSConfigurationStatus describes public DNS evidence, never service availability.
type DNSConfigurationStatus string

const (
	// DNSConfigurationMatched means the published records match a configured target.
	DNSConfigurationMatched DNSConfigurationStatus = "matched"
	// DNSConfigurationWrongTarget identifies a different token under our routing suffix.
	DNSConfigurationWrongTarget DNSConfigurationStatus = "wrong_target"
	// DNSConfigurationUnconfirmed includes proxy and flattened targets we cannot identify.
	DNSConfigurationUnconfirmed DNSConfigurationStatus = "unconfirmed"
	// DNSConfigurationNotFound means the queried hostname does not exist in DNS.
	DNSConfigurationNotFound DNSConfigurationStatus = "not_found"
	// DNSConfigurationCheckFailed means the observer could not complete its lookup.
	DNSConfigurationCheckFailed DNSConfigurationStatus = "check_failed"
	// DNSConfigurationNotConfigured means no routing targets are configured for this deployment.
	DNSConfigurationNotConfigured DNSConfigurationStatus = "not_configured"
)

// DNSObservation is advisory configuration evidence. It must not revoke ownership
// or activation, and a match does not prove public endpoint availability.
type DNSObservation struct {
	// Status describes whether the public records identify a configured target.
	Status DNSConfigurationStatus
	// Method describes only what the observer could establish from public DNS.
	Method DNSRoutingMethod
}

const (
	// dnsAuditTimeout leaves time for infrastructure checks within a health activity.
	dnsAuditTimeout = 5 * time.Second
	// maxCNAMEHops bounds work for loops and unusually long customer alias chains.
	maxCNAMEHops = 8
)

// DNSRoutingTargets describes the public routing names accepted for one domain.
type DNSRoutingTargets struct {
	SharedCNAME  string
	DomainTarget string
	TargetSuffix string
	ARecords     []netip.Addr
}

// ObserveDNS checks routing configuration independently of TXT ownership and TLS.
// A proxy or unrecognized target is unconfirmed, not a proven service failure.
func ObserveDNS(ctx context.Context, resolver dns.Resolver, domain string, targets DNSRoutingTargets) (DNSObservation, error) {
	target := normalizeDNSName(targets.SharedCNAME)
	domainTarget := normalizeDNSName(targets.DomainTarget)
	staticAddresses := targets.ARecords
	result := DNSObservation{Status: DNSConfigurationUnconfirmed, Method: DNSRoutingMethodUnknown}
	if target == "" && domainTarget == "" && len(staticAddresses) == 0 {
		result.Status = DNSConfigurationNotConfigured
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, dnsAuditTimeout)
	defer cancel()
	fail := func(err error) (DNSObservation, error) {
		result.Status = DNSConfigurationCheckFailed
		return result, err
	}
	current := normalizeDNSName(domain)
	seen := make(map[string]bool)
	hadAlias := false
	for hop := 0; ; hop++ {
		if hop == maxCNAMEHops || seen[current] {
			return fail(errors.New("custom domain CNAME chain exceeds its limit or contains a loop"))
		}
		seen[current] = true
		next, err := resolver.LookupCNAMERecord(ctx, current+".")
		if err != nil {
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				if !hadAlias {
					result.Status = DNSConfigurationNotFound
				}
				return result, nil
			}
			return fail(fmt.Errorf("observe custom domain CNAME: %w", err))
		}
		if next == "" {
			break
		}
		hadAlias = true
		current = normalizeDNSName(next)
		if domainTarget != "" && current == domainTarget {
			return DNSObservation{Status: DNSConfigurationMatched, Method: DNSRoutingMethodPerDomainCNAME}, nil
		}
		if target != "" && current == target {
			// The alias establishes configuration. Separate address lookups can
			// legitimately differ for rotating or geographic DNS responses.
			result.Status = DNSConfigurationMatched
			result.Method = DNSRoutingMethodSharedCNAME
			return result, nil
		}
		if _, other := ParseDNSTarget(targets.TargetSuffix, current); other {
			return DNSObservation{Status: DNSConfigurationWrongTarget, Method: DNSRoutingMethodUnknown}, nil
		}
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", normalizeDNSName(domain)+".")
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return result, nil
		}
		return fail(fmt.Errorf("observe custom domain addresses: %w", err))
	}
	allowed := make(map[netip.Addr]bool)
	for _, address := range staticAddresses {
		allowed[address.Unmap()] = true
	}
	var targetErr error
	for _, target := range []string{target, domainTarget} {
		if target == "" {
			continue
		}
		targetAddresses, err := resolver.LookupNetIP(ctx, "ip", target+".")
		if err != nil {
			targetErr = errors.Join(targetErr, err)
			continue
		}
		for _, address := range targetAddresses {
			allowed[address.Unmap()] = true
		}
	}
	matched := len(addresses) > 0
	for _, address := range addresses {
		if !allowed[address.Unmap()] {
			matched = false
			break
		}
	}
	if !matched {
		// A partial target lookup cannot rule out the remaining addresses.
		// Known addresses can still independently establish a complete match.
		if targetErr != nil {
			return fail(fmt.Errorf("observe routing target addresses: %w", targetErr))
		}
		return result, nil
	}
	result.Status = DNSConfigurationMatched
	if !hadAlias {
		result.Method = DNSRoutingMethodARecord
	}
	return result, nil
}

func normalizeDNSName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}
