package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpfrontends/repo"
)

// BuildMcpFrontendView converts a repo mcp_frontends row into the API response
// type.
func BuildMcpFrontendView(frontend repo.McpFrontend) *types.McpFrontend {
	return &types.McpFrontend{
		ID:                    frontend.ID.String(),
		ProjectID:             frontend.ProjectID.String(),
		EnvironmentID:         conv.FromNullableUUID(frontend.EnvironmentID),
		ExternalOauthServerID: conv.FromNullableUUID(frontend.ExternalOauthServerID),
		OauthProxyServerID:    conv.FromNullableUUID(frontend.OauthProxyServerID),
		RemoteMcpServerID:     conv.FromNullableUUID(frontend.RemoteMcpServerID),
		ToolsetID:             conv.FromNullableUUID(frontend.ToolsetID),
		Visibility:            types.McpFrontendVisibility(frontend.Visibility),
		CreatedAt:             frontend.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:             frontend.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildMcpFrontendListView converts a slice of repo rows into API types.
func BuildMcpFrontendListView(frontends []repo.McpFrontend) []*types.McpFrontend {
	result := make([]*types.McpFrontend, len(frontends))
	for i, f := range frontends {
		result[i] = BuildMcpFrontendView(f)
	}
	return result
}
