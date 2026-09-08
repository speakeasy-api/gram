package customdomains

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// ParseExpectedARecords validates the configured set of static ingress IPs
// that customers may point apex-domain A records at. Only IPv4 addresses are
// accepted: the ingress has no IPv6 front door, so advertising or matching
// anything else would be wrong. Returns a deduplicated, sorted list.
func ParseExpectedARecords(values []string) ([]netip.Addr, error) {
	addrs := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		addr, err := netip.ParseAddr(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid custom domain A record %q: %w", trimmed, err)
		}
		if !addr.Is4() {
			return nil, fmt.Errorf("invalid custom domain A record %q: only IPv4 addresses are supported", trimmed)
		}
		addrs = append(addrs, addr)
	}
	slices.SortFunc(addrs, func(a, b netip.Addr) int { return a.Compare(b) })
	return slices.Compact(addrs), nil
}

// FormatARecords renders parsed A records back to strings for display.
func FormatARecords(addrs []netip.Addr) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.String())
	}
	return out
}
