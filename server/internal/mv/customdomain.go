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
		HealthStatus:             conv.FromPGText[string](domain.HealthStatus),
		HealthIssue:              conv.FromPGText[string](domain.HealthIssue),
		HealthCheckedAt:          conv.PtrEmpty(conv.FromPGTimestamptz(domain.HealthCheckedAt)),
		UnhealthySince:           conv.PtrEmpty(conv.FromPGTimestamptz(domain.UnhealthySince)),
		CertificateExpiresAt:     conv.PtrEmpty(conv.FromPGTimestamptz(domain.CertificateExpiresAt)),
		ConsecutiveFailures:      consecutiveFailures,
		RootMcpEndpointID:        rootMcpEndpointIDString,
		OpenaiAppsChallengeToken: conv.FromPGText[string](domain.OpenaiAppsChallengeToken),
	}
}

// BuildCustomDomainMcpEndpointListView converts a slice of joined endpoint rows
// into the API response type for domains.listMcpEndpoints.
func BuildCustomDomainMcpEndpointListView(rows []mcpendpointsrepo.ListMCPEndpointsByCustomDomainIDRow) []*domains.CustomDomainMcpEndpoint {
	result := make([]*domains.CustomDomainMcpEndpoint, len(rows))
	for i, r := range rows {
		result[i] = &domains.CustomDomainMcpEndpoint{
			ID:            r.ID.String(),
			Slug:          r.Slug,
			ProjectID:     r.ProjectID.String(),
			ProjectName:   r.ProjectName,
			ProjectSlug:   r.ProjectSlug,
			McpServerID:   r.McpServerID.String(),
			McpServerName: conv.FromPGText[string](r.McpServerName),
			McpServerSlug: conv.FromPGText[string](r.McpServerSlug),
			IsDomainRoot:  r.IsDomainRoot.Valid && r.IsDomainRoot.Bool,
		}
	}
	return result
}
