package admin

import (
	"context"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SetCustomDomainTargetSuffix sets the normalized parent DNS name of
// per-domain CNAME targets. Empty leaves targets out of listCustomDomains.
func (s *Service) SetCustomDomainTargetSuffix(suffix string) {
	s.customDomainTargetSuffix = suffix
}

func (s *Service) ListCustomDomains(ctx context.Context, payload *gen.ListCustomDomainsPayload) (*gen.AdminListCustomDomainsResult, error) {
	rows, err := repo.New(s.db).AdminListCustomDomains(ctx, repo.AdminListCustomDomainsParams{
		OrganizationID:   conv.PtrToPGText(payload.OrganizationID),
		DnsRoutingMethod: conv.PtrToPGText(payload.DNSRoutingMethod),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list custom domains").LogError(ctx, s.logger)
	}

	views := make([]*gen.AdminCustomDomain, len(rows))
	for i, row := range rows {
		var dnsTarget *string
		if target, ok := customdomains.DNSTarget(s.customDomainTargetSuffix, row.DnsTargetToken); ok {
			dnsTarget = &target
		}
		healthStatus := conv.FromPGText[string](row.HealthStatus)
		healthIssue := conv.FromPGText[string](row.HealthIssue)
		if !row.DnsCheckedAt.Valid || !row.HealthCheckedAt.Valid || !row.DnsCheckedAt.Time.Equal(row.HealthCheckedAt.Time) || customdomains.IsDNSHealthIssue(customdomains.HealthIssue(row.HealthIssue.String)) {
			healthStatus = new("unknown")
			healthIssue = nil
		}
		views[i] = &gen.AdminCustomDomain{
			ID:                     row.ID.String(),
			OrganizationID:         row.OrganizationID,
			OrganizationName:       row.OrganizationName,
			OrganizationSlug:       row.OrganizationSlug,
			Domain:                 row.Domain,
			Verified:               row.Verified,
			Activated:              row.Activated,
			DNSTargetToken:         conv.FromPGText[string](row.DnsTargetToken),
			DNSTarget:              dnsTarget,
			DNSRoutingMethod:       conv.FromPGText[string](row.DnsRoutingMethod),
			DNSConfigurationStatus: conv.FromPGText[string](row.DnsConfigurationStatus),
			DNSCheckedAt:           conv.PtrEmpty(conv.FromPGTimestamptz(row.DnsCheckedAt)),
			HealthStatus:           healthStatus,
			HealthIssue:            healthIssue,
			HealthCheckedAt:        conv.PtrEmpty(conv.FromPGTimestamptz(row.HealthCheckedAt)),
			CreatedAt:              row.CreatedAt.Time.Format(time.RFC3339),
		}
	}

	return &gen.AdminListCustomDomainsResult{CustomDomains: views}, nil
}
