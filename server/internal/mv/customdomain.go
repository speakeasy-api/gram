package mv

import (
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
)

func BuildCustomDomainView(domain customdomainsrepo.CustomDomain, isUpdating bool, rootMcpEndpointID uuid.UUID) *domains.CustomDomain {
	ipAllowlist := domain.IpAllowlist
	if ipAllowlist == nil {
		ipAllowlist = []string{}
	}
	var consecutiveFailures *int32
	if domain.ConsecutiveFailures.Valid {
		consecutiveFailures = &domain.ConsecutiveFailures.Int32
	}
	var rootMcpEndpointIDString *string
	if rootMcpEndpointID != uuid.Nil {
		value := rootMcpEndpointID.String()
		rootMcpEndpointIDString = &value
	}
	healthStatus := conv.FromPGText[string](domain.HealthStatus)
	healthIssue := conv.FromPGText[string](domain.HealthIssue)
	// Compare database timestamps before formatting drops subsecond precision.
	// An older worker can overwrite health without refreshing DNS.
	if domain.DnsCheckedAt.Valid && (!domain.HealthCheckedAt.Valid || !domain.DnsCheckedAt.Time.Equal(domain.HealthCheckedAt.Time)) {
		healthStatus = new("unknown")
	}
	// These persisted routing findings do not establish infrastructure health.
	switch domain.HealthIssue.String {
	case "dns_not_found", "dns_target_mismatch":
		healthStatus = new("unknown")
		healthIssue = nil
	}
	return &domains.CustomDomain{
		ID:                       domain.ID.String(),
		OrganizationID:           domain.OrganizationID,
		Domain:                   domain.Domain,
		Verified:                 domain.Verified,
		Activated:                domain.Activated,
		CreatedAt:                domain.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                domain.UpdatedAt.Time.Format(time.RFC3339),
		IsUpdating:               isUpdating,
		IPAllowlist:              ipAllowlist,
		HealthStatus:             healthStatus,
		HealthIssue:              healthIssue,
		HealthCheckedAt:          conv.PtrEmpty(conv.FromPGTimestamptz(domain.HealthCheckedAt)),
		UnhealthySince:           conv.PtrEmpty(conv.FromPGTimestamptz(domain.UnhealthySince)),
		CertificateExpiresAt:     conv.PtrEmpty(conv.FromPGTimestamptz(domain.CertificateExpiresAt)),
		ConsecutiveFailures:      consecutiveFailures,
		RootMcpEndpointID:        rootMcpEndpointIDString,
		OpenaiAppsChallengeToken: conv.FromPGText[string](domain.OpenaiAppsChallengeToken),
		SuggestedRecordType:      "cname",
		DNSTargetToken:           conv.FromPGText[string](domain.DnsTargetToken),
		// The target needs the deployment's suffix, which the customdomains
		// service owns and fills in.
		DNSTarget:              nil,
		DNSRoutingMethod:       conv.FromPGText[string](domain.DnsRoutingMethod),
		DNSConfigurationStatus: conv.FromPGText[string](domain.DnsConfigurationStatus),
		DNSCheckedAt:           conv.PtrEmpty(conv.FromPGTimestamptz(domain.DnsCheckedAt)),
	}
}

// BuildCustomDomainMcpEndpointListView converts a slice of joined endpoint rows
// into the API response type for domains.listMcpEndpoints.
func BuildCustomDomainMcpEndpointListView(rows []mcpendpointsrepo.ListMCPEndpointsByCustomDomainIDRow) []*domains.CustomDomainMcpEndpoint {
	result := make([]*domains.CustomDomainMcpEndpoint, len(rows))
	for i, r := range rows {
		result[i] = &domains.CustomDomainMcpEndpoint{
			ID:              r.ID.String(),
			Slug:            r.Slug,
			ProjectID:       r.ProjectID.String(),
			ProjectName:     r.ProjectName,
			ProjectSlug:     r.ProjectSlug,
			McpServerID:     conv.FromNullableUUID(r.McpServerID),
			MetaMcpServerID: conv.FromNullableUUID(r.MetaMcpServerID),
			McpServerName:   conv.PtrEmpty(r.McpServerName),
			McpServerSlug:   conv.FromPGText[string](r.McpServerSlug),
			IsDomainRoot:    r.IsDomainRoot.Valid && r.IsDomainRoot.Bool,
		}
	}
	return result
}
