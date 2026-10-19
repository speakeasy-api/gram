package guardian

import (
	"fmt"
	"net"
)

// WithHostedMCPFrontEndCIDR configures the catalog destination without granting
// access to ordinary clients. Empty disables it; only a private IPv4 /32 is
// accepted. MCP clients must also opt in with WithInternalCatalog.
func WithHostedMCPFrontEndCIDR(cidr string) (func(*Policy), error) {
	var block *net.IPNet
	if cidr != "" {
		ip, parsed, err := net.ParseCIDR(cidr)
		if err != nil || ip.To4() == nil || !ip.IsPrivate() || parsed.String() != ip.String()+"/32" {
			return nil, fmt.Errorf("internal catalog destination must be a private IPv4 /32")
		}
		block = parsed
	}
	return func(p *Policy) { p.internalCatalog = block }, nil
}

// WithInternalCatalog enables the configured catalog rule for an MCP client
// or URL validation. Other destinations retain the policy's normal IP protections.
func WithInternalCatalog() ClientOption {
	return func(o *httpClientOptions) { o.internalCatalog = true }
}
