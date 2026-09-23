package mcpendpoints

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// publishForMCPMembership queues the existing project publisher when an
// endpoint mutation may change a package's selected address. A failed probe
// stays best-effort: the periodic publisher sweep remains the safety net.
func (s *Service) publishForMCPMembership(ctx context.Context, authCtx *contextvalues.AuthContext, serverIDs ...uuid.NullUUID) {
	connected, err := pluginsrepo.New(s.db).HasPluginGithubConnectionForProject(ctx, *authCtx.ProjectID)
	if err != nil {
		s.logger.WarnContext(ctx, "check marketplace connection after endpoint mutation", attr.SlogError(err))
		return
	}
	if !connected {
		return
	}
	for _, id := range serverIDs {
		if !id.Valid {
			continue
		}
		attached, err := pluginsrepo.New(s.db).HasPluginMembershipForMCPServer(ctx, pluginsrepo.HasPluginMembershipForMCPServerParams{
			ProjectID: *authCtx.ProjectID, McpServerID: id,
		})
		if err != nil {
			s.logger.ErrorContext(ctx, "check plugin membership after endpoint mutation", attr.SlogError(err))
			continue
		}
		if attached {
			s.triggerPluginPublish(ctx, authCtx, true, false)
			return
		}
	}
}
