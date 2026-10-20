package mcpskills

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills/skilllink"
)

// The link vocabulary lives in package skilllink so writers outside the
// resolver can use it without an import cycle. These aliases keep it
// addressable from mcpskills, where the resolver reads it.

// TargetKind names the kind of MCP surface a skill is linked to.
type TargetKind = skilllink.TargetKind

// Target is the MCP surface a skill is linked to and served by.
type Target = skilllink.Target

// NewLink describes a skill to link to the MCP surface that will serve it.
type NewLink = skilllink.NewLink

const (
	TargetMCPServer           = skilllink.TargetMCPServer
	TargetMetaMCPServer       = skilllink.TargetMetaMCPServer
	TargetOrganizationGateway = skilllink.TargetOrganizationGateway
)

// ErrInvalidTarget reports a target whose id does not fit its kind.
var ErrInvalidTarget = skilllink.ErrInvalidTarget

// MCPServerTarget names a stored MCP server.
func MCPServerTarget(id uuid.UUID) Target { return skilllink.MCPServerTarget(id) }

// MetaMCPServerTarget names a stored meta MCP gateway.
func MetaMCPServerTarget(id uuid.UUID) Target { return skilllink.MetaMCPServerTarget(id) }

// OrganizationGatewayTarget names the organization gateway.
func OrganizationGatewayTarget() Target { return skilllink.OrganizationGatewayTarget() }

// TargetOf reads the target back from a link's stored columns.
func TargetOf(kind string, mcpServerID, metaMCPServerID uuid.NullUUID) Target {
	return skilllink.TargetOf(kind, mcpServerID, metaMCPServerID)
}

// CreateLink links a skill to an MCP surface and returns the link's id.
func CreateLink(ctx context.Context, db database.DBTX, link NewLink) (uuid.UUID, error) {
	return skilllink.CreateLink(ctx, db, link) //nolint:wrapcheck // thin alias of the skilllink writer
}

// UpsertLink links a skill to an MCP surface, or updates the pin of the live
// link that already joins them.
func UpsertLink(ctx context.Context, db database.DBTX, link NewLink) (repo.McpSkillLink, error) {
	return skilllink.UpsertLink(ctx, db, link) //nolint:wrapcheck // thin alias of the skilllink writer
}

// DeleteLink unlinks a skill from its surface, scoped to the caller's
// organization and project.
func DeleteLink(ctx context.Context, db database.DBTX, organizationID string, projectID, linkID uuid.UUID) (bool, error) {
	return skilllink.DeleteLink(ctx, db, organizationID, projectID, linkID) //nolint:wrapcheck // thin alias of the skilllink writer
}
