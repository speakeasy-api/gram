package toolsets

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// toolsetCarriedByPlugin reports, inside the mutation's transaction, whether a
// plugin carries the toolset directly or through an enabled MCP server it
// backs. Callers probe before any write in the same transaction that could
// detach the toolset from a plugin, such as deleting its hosted wrapper.
func toolsetCarriedByPlugin(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, toolsetID uuid.UUID) (bool, error) {
	attached, err := pluginsrepo.New(tx).HasPluginMembershipForToolset(ctx, pluginsrepo.HasPluginMembershipForToolsetParams{ProjectID: *authCtx.ProjectID, ToolsetID: toolsetID})
	if err != nil {
		return false, fmt.Errorf("check toolset plugin membership: %w", err)
	}
	return attached, nil
}

// detachToolsetPluginServers removes a deleted toolset's toolset-keyed plugin
// entries so they stop holding their display names, auditing each removal.
func detachToolsetPluginServers(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, authCtx *contextvalues.AuthContext, toolsetID uuid.UUID) (int, error) {
	detached, err := pluginsrepo.New(tx).SoftDeletePluginServersByToolsetID(ctx, pluginsrepo.SoftDeletePluginServersByToolsetIDParams{
		ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("detach toolset plugin servers: %w", err)
	}
	toolsetURN := urn.NewToolset(toolsetID)
	for _, server := range detached {
		if err := auditLogger.LogPluginServerRemove(ctx, tx, audit.LogPluginServerRemoveEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        *authCtx.ProjectID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			PluginID:         server.PluginID,
			PluginName:       server.PluginName,
			PluginSlug:       server.PluginSlug,
			ServerID:         server.ID,
			ToolsetURN:       &toolsetURN,
			McpServerURN:     nil,
			MetaMcpServerURN: nil,
		}); err != nil {
			return 0, fmt.Errorf("audit toolset plugin server removal: %w", err)
		}
	}
	return len(detached), nil
}

// requestPluginPublication records a durable package refresh in the
// mutation's transaction for a toolset change a plugin carries.
func (s *Service) requestPluginPublication(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext) error {
	if !s.publicationRequests.Enabled {
		return nil
	}
	if err := s.publicationRequests.Project(ctx, tx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID); err != nil {
		return fmt.Errorf("request plugin publication: %w", err)
	}
	return nil
}

// publishPluginsAfterToolsetChange queues the existing project publisher after
// a committed toolset change a plugin carries. Projects without a marketplace
// enqueue nothing. A failed probe stays best-effort: the periodic publisher
// sweep remains the safety net.
func (s *Service) publishPluginsAfterToolsetChange(ctx context.Context, authCtx *contextvalues.AuthContext) {
	if !s.pluginsGitHubEnabled {
		return
	}
	connected, err := pluginsrepo.New(s.db).HasPluginGithubConnectionForProject(context.WithoutCancel(ctx), *authCtx.ProjectID)
	if err != nil {
		s.logger.WarnContext(ctx, "check marketplace connection after toolset mutation", attr.SlogError(err))
		return
	}
	s.triggerPluginPublish(ctx, authCtx, connected, false)
}
