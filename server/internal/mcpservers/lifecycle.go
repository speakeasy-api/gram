package mcpservers

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LifecycleUpdateInput describes the narrow MCP-server fields shared by
// dashboard management and Platform MCP lifecycle operations. Backend references
// are deliberately inherited from the locked server; callers cannot use this
// command to rewire a server.
type LifecycleUpdateInput struct {
	OrganizationID string
	ProjectID      uuid.UUID
	ActorUserID    string
	ActorEmail     *string
	ServerID       uuid.UUID
	Name           *string
	Visibility     string

	EnvironmentID         uuid.NullUUID
	UserSessionIssuerID   uuid.NullUUID
	RemoteMcpServerID     uuid.NullUUID
	TunneledMcpServerID   uuid.NullUUID
	ToolsetID             uuid.NullUUID
	UnproxiedMcpServerID  uuid.NullUUID
	ToolVariationsGroupID uuid.NullUUID
}

// UpdateMCPServerLifecycleInTransaction updates only the name-derived slug and
// visibility of a locked MCP server. It synchronizes auto-derived plugin display
// names and writes the normal MCP update audit event, but it never creates,
// removes, or selects a plugin attachment.
func UpdateMCPServerLifecycleInTransaction(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, existing repo.McpServer, input LifecycleUpdateInput) (repo.McpServer, error) {
	if tx == nil || auditLogger == nil || input.OrganizationID == "" || input.ProjectID == uuid.Nil || input.ActorUserID == "" || input.ServerID == uuid.Nil || existing.ID != input.ServerID || existing.ProjectID != input.ProjectID || input.Visibility == "" {
		return repo.McpServer{}, fmt.Errorf("invalid MCP server lifecycle update input")
	}

	name := existing.Name
	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		if trimmed == "" {
			return repo.McpServer{}, fmt.Errorf("MCP server name must be non-empty")
		}
		name = conv.ToPGText(trimmed)
	}
	slug, err := computeServerSlug(conv.FromPGTextOrEmpty[string](name), existing.ID)
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("compute MCP server slug: %w", err)
	}

	updated, err := repo.New(tx).UpdateMCPServer(ctx, repo.UpdateMCPServerParams{
		Name:                  name,
		Slug:                  conv.ToPGText(slug),
		EnvironmentID:         input.EnvironmentID,
		UserSessionIssuerID:   input.UserSessionIssuerID,
		RemoteMcpServerID:     input.RemoteMcpServerID,
		TunneledMcpServerID:   input.TunneledMcpServerID,
		ToolsetID:             input.ToolsetID,
		UnproxiedMcpServerID:  input.UnproxiedMcpServerID,
		ToolVariationsGroupID: input.ToolVariationsGroupID,
		Visibility:            input.Visibility,
		ID:                    existing.ID,
		ProjectID:             input.ProjectID,
	})
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("update MCP server lifecycle: %w", err)
	}

	oldDisplayName := ServerDisplayName(existing)
	newDisplayName := ServerDisplayName(updated)
	if oldDisplayName != newDisplayName {
		if _, err := pluginsrepo.New(tx).SyncMcpServerDisplayName(ctx, pluginsrepo.SyncMcpServerDisplayNameParams{
			NewDisplayName: newDisplayName,
			ProjectID:      input.ProjectID,
			McpServerID:    uuid.NullUUID{UUID: updated.ID, Valid: true},
			OldDisplayName: oldDisplayName,
		}); err != nil {
			return repo.McpServer{}, fmt.Errorf("sync MCP server plugin display name: %w", err)
		}
	}

	if err := auditLogger.LogMcpServerUpdate(ctx, tx, audit.LogMcpServerUpdateEvent{
		OrganizationID:          input.OrganizationID,
		ProjectID:               input.ProjectID,
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, input.ActorUserID),
		ActorDisplayName:        input.ActorEmail,
		ActorSlug:               nil,
		McpServerURN:            urn.NewMcpServer(updated.ID),
		McpServerName:           conv.FromPGTextOrEmpty[string](updated.Name),
		McpServerSlug:           conv.FromPGTextOrEmpty[string](updated.Slug),
		McpServerSnapshotBefore: mv.BuildMcpServerView(existing),
		McpServerSnapshotAfter:  mv.BuildMcpServerView(updated),
	}); err != nil {
		return repo.McpServer{}, fmt.Errorf("audit MCP server lifecycle update: %w", err)
	}
	return updated, nil
}
