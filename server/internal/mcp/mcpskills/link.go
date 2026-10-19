package mcpskills

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills/repo"
)

// NewLink describes a skill to link to the MCP surface that will serve it.
type NewLink struct {
	OrganizationID string
	ProjectID      uuid.UUID
	SkillID        uuid.UUID

	// PinnedVersionID pins the served version; nil serves the skill's latest
	// valid version.
	PinnedVersionID *uuid.UUID

	Target          Target
	CreatedByUserID string
}

// CreateLink links a skill to an MCP surface and returns the link's id. The
// target must be valid for its kind; a stored target must belong to the
// link's project, which the foreign keys enforce.
func CreateLink(ctx context.Context, db database.DBTX, link NewLink) (uuid.UUID, error) {
	if err := link.Target.Validate(); err != nil {
		return uuid.Nil, err
	}

	pinned := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if link.PinnedVersionID != nil {
		pinned = uuid.NullUUID{UUID: *link.PinnedVersionID, Valid: true}
	}
	mcpServerID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	metaMCPServerID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	switch link.Target.Kind {
	case TargetMCPServer:
		mcpServerID = uuid.NullUUID{UUID: link.Target.ID, Valid: true}
	case TargetMetaMCPServer:
		metaMCPServerID = uuid.NullUUID{UUID: link.Target.ID, Valid: true}
	case TargetOrganizationGateway:
	}

	row, err := repo.New(db).CreateMCPSkillLink(ctx, repo.CreateMCPSkillLinkParams{
		OrganizationID:  link.OrganizationID,
		ProjectID:       link.ProjectID,
		SkillID:         link.SkillID,
		PinnedVersionID: pinned,
		TargetKind:      string(link.Target.Kind),
		McpServerID:     mcpServerID,
		MetaMcpServerID: metaMCPServerID,
		CreatedByUserID: link.CreatedByUserID,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create skill link: %w", err)
	}
	return row.ID, nil
}

// DeleteLink unlinks a skill from its surface, scoped to the caller's
// organization and project. It reports whether a live link was removed.
func DeleteLink(ctx context.Context, db database.DBTX, organizationID string, projectID, linkID uuid.UUID) (bool, error) {
	removed, err := repo.New(db).DeleteMCPSkillLink(ctx, repo.DeleteMCPSkillLinkParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ID:             linkID,
	})
	if err != nil {
		return false, fmt.Errorf("delete skill link: %w", err)
	}
	return removed > 0, nil
}
