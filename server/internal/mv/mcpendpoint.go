package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
)

// BuildMcpEndpointView converts a repo mcp_endpoints row into the API response
// type.
func BuildMcpEndpointView(endpoint repo.McpEndpoint) *types.McpEndpoint {
	return &types.McpEndpoint{
		ID:             endpoint.ID.String(),
		ProjectID:      endpoint.ProjectID.String(),
		CustomDomainID: conv.FromNullableUUID(endpoint.CustomDomainID),
		McpServerID:    endpoint.McpServerID.String(),
		Slug:           types.McpEndpointSlug(endpoint.Slug),
		CreatedAt:      endpoint.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      endpoint.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildMcpEndpointListView converts a slice of repo rows into API types.
func BuildMcpEndpointListView(endpoints []repo.McpEndpoint) []*types.McpEndpoint {
	result := make([]*types.McpEndpoint, len(endpoints))
	for i, e := range endpoints {
		result[i] = BuildMcpEndpointView(e)
	}
	return result
}
