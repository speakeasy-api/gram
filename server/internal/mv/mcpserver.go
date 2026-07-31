package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// mcpServerBackendKind derives the backend kind from whichever backend
// reference is set on the row (the mcp_servers backend-exclusivity check
// guarantees exactly one).
func mcpServerBackendKind(server repo.McpServer) *types.McpServerBackendKind {
	var kind types.McpServerBackendKind
	switch {
	case server.ToolsetID.Valid:
		kind = "toolset"
	case server.RemoteMcpServerID.Valid:
		kind = "remote"
	case server.TunneledMcpServerID.Valid:
		kind = "tunneled"
	default:
		return nil
	}
	return &kind
}

// BuildMcpServerView converts a repo mcp_servers row into the API response
// type. The toolset summary is left nil; read paths that need it attach one
// via BuildMcpServerToolsetSummary.
func BuildMcpServerView(server repo.McpServer) *types.McpServer {
	return &types.McpServer{
		ID:                    server.ID.String(),
		ProjectID:             server.ProjectID.String(),
		Name:                  conv.FromPGText[string](server.Name),
		Slug:                  conv.FromPGText[string](server.Slug),
		EnvironmentID:         conv.FromNullableUUID(server.EnvironmentID),
		UserSessionIssuerID:   conv.FromNullableUUID(server.UserSessionIssuerID),
		RemoteMcpServerID:     conv.FromNullableUUID(server.RemoteMcpServerID),
		TunneledMcpServerID:   conv.FromNullableUUID(server.TunneledMcpServerID),
		ToolsetID:             conv.FromNullableUUID(server.ToolsetID),
		ToolVariationsGroupID: conv.FromNullableUUID(server.ToolVariationsGroupID),
		Visibility:            types.McpServerVisibility(server.Visibility),
		BackendKind:           mcpServerBackendKind(server),
		ToolsetSummary:        nil,
		CreatedAt:             server.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:             server.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildMcpServerToolsetSummary converts a toolset summary row into the API
// type carried by toolset-backed McpServer views.
func BuildMcpServerToolsetSummary(id, slug, name string, toolUrns []string, originRegistrySpecifier *string) *types.McpServerToolsetSummary {
	if toolUrns == nil {
		toolUrns = []string{}
	}
	return &types.McpServerToolsetSummary{
		ID:                      id,
		Slug:                    slug,
		Name:                    name,
		ToolCount:               len(toolUrns),
		ToolUrns:                toolUrns,
		OriginRegistrySpecifier: originRegistrySpecifier,
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
