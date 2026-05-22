package mv

import (
	"github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
)

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
		}
	}
	return result
}
