package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

// BuildMcpServerView converts a repo mcp_servers row into the API response
// type.
func BuildMcpServerView(server repo.McpServer) *types.McpServer {
	return &types.McpServer{
		ID:                    server.ID.String(),
		ProjectID:             server.ProjectID.String(),
		Name:                  conv.FromPGText[string](server.Name),
		Slug:                  conv.FromPGText[string](server.Slug),
		PlatformEndpointSlug:  nil,
		LegacyPlatformSlug:    nil,
		EnvironmentID:         conv.FromNullableUUID(server.EnvironmentID),
		UserSessionIssuerID:   conv.FromNullableUUID(server.UserSessionIssuerID),
		RemoteMcpServerID:     conv.FromNullableUUID(server.RemoteMcpServerID),
		TunneledMcpServerID:   conv.FromNullableUUID(server.TunneledMcpServerID),
		ToolsetID:             conv.FromNullableUUID(server.ToolsetID),
		UnproxiedMcpServerID:  conv.FromNullableUUID(server.UnproxiedMcpServerID),
		ToolVariationsGroupID: conv.FromNullableUUID(server.ToolVariationsGroupID),
		BuiltinBackend:        conv.FromPGText[string](server.BuiltinBackend),
		Visibility:            types.McpServerVisibility(server.Visibility),
		NetworkAccessMode:     types.NetworkAccessMode(networkaccess.EffectiveForView(server.NetworkAccessMode)),
		Toolset:               nil,
		PrimaryEndpointID:     nil,
		PrimaryEndpointURL:    nil,
		HasEndpoint:           nil,
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

// BuildMcpServerToolsetSummaryView converts a toolset summary row into the
// API type.
func BuildMcpServerToolsetSummaryView(row repo.ListToolsetSummariesByIDsRow) *types.McpServerToolsetSummary {
	slugs := row.ExternalMcpSlugs
	if slugs == nil {
		slugs = []string{}
	}
	return &types.McpServerToolsetSummary{
		ID:                    row.ID.String(),
		Slug:                  row.Slug,
		Name:                  row.Name,
		Description:           conv.FromPGText[string](row.Description),
		ToolCount:             int(row.ToolCount) + platformtools.CountTools(row.PlatformToolUrns),
		ToolSelectionMode:     row.ToolSelectionMode,
		ExternalMcpSlugs:      slugs,
		ExternalMcpProxyOnly:  row.ExternalMcpProxyOnly,
		ExternalOauthServerID: conv.FromNullableUUID(row.ExternalOauthServerID),
		RegistrySpecifier:     conv.PtrEmpty(row.RegistrySpecifier),
		HostingMcpServerID:    nil,
	}
}
