package customdomains

import (
	"fmt"
	"net"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains/repo"
)

func buildCustomDomainView(domain repo.CustomDomain, isUpdating bool) *gen.CustomDomain {
	ipAllowlist := domain.IpAllowlist
	if ipAllowlist == nil {
		ipAllowlist = []string{}
	}
	var consecutiveFailures *int32
	if domain.ConsecutiveFailures.Valid {
		consecutiveFailures = &domain.ConsecutiveFailures.Int32
	}
	return &gen.CustomDomain{
		ID:                   domain.ID.String(),
		OrganizationID:       domain.OrganizationID,
		Domain:               domain.Domain,
		Verified:             domain.Verified,
		Activated:            domain.Activated,
		CreatedAt:            domain.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:            domain.UpdatedAt.Time.Format(time.RFC3339),
		IsUpdating:           isUpdating,
		IPAllowlist:          ipAllowlist,
		HealthStatus:         conv.FromPGText[string](domain.HealthStatus),
		HealthIssue:          conv.FromPGText[string](domain.HealthIssue),
		HealthCheckedAt:      conv.PtrEmpty(conv.FromPGTimestamptz(domain.HealthCheckedAt)),
		UnhealthySince:       conv.PtrEmpty(conv.FromPGTimestamptz(domain.UnhealthySince)),
		CertificateExpiresAt: conv.PtrEmpty(conv.FromPGTimestamptz(domain.CertificateExpiresAt)),
		ConsecutiveFailures:  consecutiveFailures,
	}
}

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
