package toolsets

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
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
