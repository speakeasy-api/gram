package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// BuildMcpServerView converts a repo mcp_servers row into the API response
// type.
func BuildMcpServerView(server repo.McpServer) *types.McpServer {
	return &types.McpServer{
		ID:                    server.ID.String(),
		ProjectID:             server.ProjectID.String(),
		EnvironmentID:         conv.FromNullableUUID(server.EnvironmentID),
		ExternalOauthServerID: conv.FromNullableUUID(server.ExternalOauthServerID),
		OauthProxyServerID:    conv.FromNullableUUID(server.OauthProxyServerID),
		RemoteMcpServerID:     conv.FromNullableUUID(server.RemoteMcpServerID),
		ToolsetID:             conv.FromNullableUUID(server.ToolsetID),
		Visibility:            types.McpServerVisibility(server.Visibility),
		CreatedAt:             server.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:             server.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildMcpServerListView converts a slice of repo rows into API types.
func BuildMcpServerListView(servers []repo.McpServer) []*types.McpServer {
	result := make([]*types.McpServer, len(servers))
	for i, s := range servers {
		result[i] = BuildMcpServerView(s)
	}
	return result
}
