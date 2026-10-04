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

// requestPluginPublicationForToolset records a durable package refresh in the
// mutation's transaction when a plugin carries the toolset, directly or through
// an MCP server it backs.
func (s *Service) requestPluginPublicationForToolset(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, toolsetID uuid.UUID) error {
	if !s.publicationRequests.Enabled {
		return nil
	}
	attached, err := pluginsrepo.New(tx).HasPluginMembershipForToolset(ctx, pluginsrepo.HasPluginMembershipForToolsetParams{ProjectID: *authCtx.ProjectID, ToolsetID: toolsetID})
	if err != nil {
		return fmt.Errorf("check toolset plugin membership: %w", err)
	}
	if !attached {
		return nil
	}
	if err := s.publicationRequests.Project(ctx, tx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID); err != nil {
		return fmt.Errorf("request plugin publication: %w", err)
	}
	return nil
}

// publishPluginsForToolset queues the existing project publisher after a
// committed toolset mutation that changes what a package carrying it renders.
// Projects without a marketplace and toolsets no plugin carries enqueue
// nothing. A failed probe stays best-effort: the periodic publisher sweep
// remains the safety net.
func (s *Service) publishPluginsForToolset(ctx context.Context, authCtx *contextvalues.AuthContext, toolsetID uuid.UUID) {
	if !s.pluginsGitHubEnabled {
		return
	}
	queries := pluginsrepo.New(s.db)
	connected, err := queries.HasPluginGithubConnectionForProject(ctx, *authCtx.ProjectID)
	if err != nil {
		s.logger.WarnContext(ctx, "check marketplace connection after toolset mutation", attr.SlogError(err))
		return
	}
	if !connected {
		return
	}
	attached, err := queries.HasPluginMembershipForToolset(ctx, pluginsrepo.HasPluginMembershipForToolsetParams{ProjectID: *authCtx.ProjectID, ToolsetID: toolsetID})
	if err != nil {
		s.logger.WarnContext(ctx, "check plugin membership after toolset mutation", attr.SlogError(err))
		return
	}
	s.triggerPluginPublish(ctx, authCtx, attached, false)
}
