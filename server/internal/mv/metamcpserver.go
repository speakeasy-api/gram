package mv

import (
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/repo"
)

// BuildMetaMcpServerView converts a repo meta_mcp_servers row into the API
// response type.
func BuildMetaMcpServerView(server repo.MetaMcpServer) *types.MetaMcpServer {
	return &types.MetaMcpServer{
		ID:                  server.ID.String(),
		OrganizationID:      server.OrganizationID,
		ProjectID:           server.ProjectID.String(),
		Name:                server.Name,
		UserSessionIssuerID: conv.FromNullableUUID(server.UserSessionIssuerID),
		Visibility:          types.MetaMcpServerVisibility(server.Visibility),
		CreatedAt:           conv.FromPGTimestamptz(server.CreatedAt),
		UpdatedAt:           conv.FromPGTimestamptz(server.UpdatedAt),
		MemberCount:         nil,
	}
}

// BuildMetaMcpServerListView converts a slice of repo listing rows into API
// types, carrying each server's live member count.
func BuildMetaMcpServerListView(servers []repo.ListMetaMCPServersRow) []*types.MetaMcpServer {
	result := make([]*types.MetaMcpServer, len(servers))
	for i, s := range servers {
		view := BuildMetaMcpServerView(s.MetaMcpServer)
		count := int(s.MemberCount)
		view.MemberCount = &count
		result[i] = view
	}
	return result
}

// BuildMetaMcpMemberView converts a joined member listing row into the API
// response type.
func BuildMetaMcpMemberView(member repo.ListMetaMCPMembersRow) *types.MetaMcpMember {
	return &types.MetaMcpMember{
		ID:            member.ID.String(),
		McpServerID:   member.McpServerID.String(),
		McpServerName: conv.FromPGText[string](member.McpServerName),
		McpServerSlug: conv.FromPGText[string](member.McpServerSlug),
		SortOrder:     int(member.SortOrder),
	}
}

// BuildMetaMcpMemberListView converts a slice of joined member listing rows
// into API types.
func BuildMetaMcpMemberListView(members []repo.ListMetaMCPMembersRow) []*types.MetaMcpMember {
	result := make([]*types.MetaMcpMember, len(members))
	for i, m := range members {
		result[i] = BuildMetaMcpMemberView(m)
	}
	return result
}

// BuildMetaMcpMemberViewFromParts builds the API response type from a bare
// membership row plus its member server row. Used by mutation handlers, which
// hold both rows and don't need the listing join.
func BuildMetaMcpMemberViewFromParts(member repo.MetaMcpServerMember, server mcpserversrepo.McpServer) *types.MetaMcpMember {
	return &types.MetaMcpMember{
		ID:            member.ID.String(),
		McpServerID:   member.McpServerID.String(),
		McpServerName: conv.FromPGText[string](server.Name),
		McpServerSlug: conv.FromPGText[string](server.Slug),
		SortOrder:     int(member.SortOrder),
	}
}
