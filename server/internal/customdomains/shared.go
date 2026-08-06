package customdomains

import (
	"fmt"
	"net"
	"strings"
)

// validateIPAllowlist checks that every entry is a valid IPv4 address or IPv4 CIDR range.
// IPv6 is rejected — nginx whitelist-source-range only supports IPv4 for this use case.
func validateIPAllowlist(entries []string) error {
	for _, entry := range entries {
		// Reject any IPv6 notation (including IPv4-mapped ::ffff: addresses) before
		// parsing, since net.ParseIP considers ::ffff:x.x.x.x a valid IPv4 address.
		if strings.Contains(entry, ":") {
			return fmt.Errorf("IPv6 addresses and CIDR ranges are not supported: %q", entry)
		}
		if _, _, err := net.ParseCIDR(entry); err == nil {
			continue
		}
		if net.ParseIP(entry) != nil {
			continue
		}
		return fmt.Errorf("invalid IP address or CIDR range: %q", entry)
	}
	return nil
}
