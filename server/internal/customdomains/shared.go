package customdomains

import (
	"fmt"
	"net"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/customdomains/repo"
)

func buildCustomDomainView(domain repo.CustomDomain, isUpdating bool) *gen.CustomDomain {
	ipAllowlist := domain.IpAllowlist
	if ipAllowlist == nil {
		ipAllowlist = []string{}
	}
	return &gen.CustomDomain{
		ID:             domain.ID.String(),
		OrganizationID: domain.OrganizationID,
		Domain:         domain.Domain,
		Verified:       domain.Verified,
		Activated:      domain.Activated,
		CreatedAt:      domain.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      domain.UpdatedAt.Time.Format(time.RFC3339),
		IsUpdating:     isUpdating,
		IPAllowlist:    ipAllowlist,
	}
}

// validateIPAllowlist checks that every entry is a valid IPv4 address or IPv4 CIDR range.
// IPv6 is rejected — nginx whitelist-source-range only supports IPv4 for this use case.
func validateIPAllowlist(entries []string) error {
	for _, entry := range entries {
		if ip, network, err := net.ParseCIDR(entry); err == nil {
			if ip.To4() == nil || network.IP.To4() == nil {
				return fmt.Errorf("IPv6 CIDR ranges are not supported: %q", entry)
			}
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			if ip.To4() == nil {
				return fmt.Errorf("IPv6 addresses are not supported: %q", entry)
			}
			continue
		}
		return fmt.Errorf("invalid IP address or CIDR range: %q", entry)
	}
	return nil
}
