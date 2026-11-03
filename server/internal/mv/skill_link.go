package mv

import (
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpskillsrepo "github.com/speakeasy-api/gram/server/internal/mcp/mcpskills/repo"
)

// BuildSkillLinkView renders a live skill link. targetName is the MCP server
// or gateway name, nil for the organization gateway, which has no row.
func BuildSkillLinkView(link mcpskillsrepo.McpSkillLink, skillName, skillDisplayName string, targetName *string) *types.SkillLink {
	return &types.SkillLink{
		ID:               link.ID.String(),
		SkillID:          link.SkillID.String(),
		SkillName:        skillName,
		SkillDisplayName: skillDisplayName,
		TargetKind:       link.TargetKind,
		McpServerID:      conv.FromNullableUUID(link.McpServerID),
		MetaMcpServerID:  conv.FromNullableUUID(link.MetaMcpServerID),
		TargetName:       targetName,
		PinnedVersionID:  conv.FromNullableUUID(link.PinnedVersionID),
		CreatedAt:        conv.FromPGTimestamptz(link.CreatedAt),
	}
}

// SkillLinkTargetName picks the display name of a link's target from the
// joined columns: a gateway's name, or an MCP server's name, falling back to
// its toolset's name and then its slug. It is nil when none is known, as for
// the organization gateway.
func SkillLinkTargetName(metaMCPServerName, mcpServerName, toolsetName, mcpServerSlug pgtype.Text) *string {
	for _, name := range []pgtype.Text{metaMCPServerName, mcpServerName, toolsetName, mcpServerSlug} {
		if name.Valid && name.String != "" {
			value := name.String
			return &value
		}
	}
	return nil
}
