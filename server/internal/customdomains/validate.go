package customdomains

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/publicsuffix"
)

var prohibitedDomainRoots = []string{"getgram.ai", "speakeasy.com", "speakeasyapi.dev"}
var specialTestDomains = []string{"chat.speakeasy.com", "chat.dev.speakeasy.com"}

// Every label follows the DNS LDH rule (1-63 chars, no leading/trailing
// hyphen); the final label is an alphabetic TLD.
var domainRegex = regexp.MustCompile(`^(?i)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// NormalizeDomainName canonicalizes user input for storage and comparison:
// DNS is case-insensitive but Host-header matching and the unique index are
// exact strings, so mixed case must never be persisted.
func NormalizeDomainName(domain string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
}

// ValidateDomainName rejects syntactically invalid and prohibited domains. It
// is shared by the registration API (synchronous row creation) and the
// verification activity so both fail identically. Callers pass the
// NormalizeDomainName form; validation normalizes again so a raw value cannot
// dodge the prohibited-root check via case or a trailing dot.
func ValidateDomainName(domain string) error {
	normalized := NormalizeDomainName(domain)
	if len(normalized) > 253 || !domainRegex.MatchString(normalized) {
		return fmt.Errorf("domain is invalid: %s", domain)
	}
	if slices.Contains(specialTestDomains, normalized) { // Temporarily allowed test domains
		return nil
	}
	for _, root := range prohibitedDomainRoots {
		if normalized == root || strings.HasSuffix(normalized, "."+root) {
			return fmt.Errorf("domain %s is prohibited", domain)
		}
	}
	return nil
}

// IsProbablyApexDomain reports whether the domain looks like a zone apex
// (registrable domain == the domain itself), where DNS forbids a CNAME and A
// records are required. eTLD+1 is a registrable-domain heuristic, not
// authoritative zone knowledge (delegated subzones exist), so callers must
// treat this as a suggestion the user can override, never a hard rule.
func IsProbablyApexDomain(domain string) bool {
	normalized := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if normalized == "" {
		return false
	}
	registrable, err := publicsuffix.EffectiveTLDPlusOne(normalized)
	if err != nil {
		return false
	}
	return registrable == normalized
}
