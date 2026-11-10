package mcpendpoints

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// publishForMCPMembership queues the existing project publisher when an
// endpoint mutation may change a package's selected address. A failed probe
// stays best-effort: the periodic publisher sweep remains the safety net.
func (s *Service) publishForMCPMembership(ctx context.Context, authCtx *contextvalues.AuthContext, serverIDs []uuid.NullUUID, gatewayIDs ...uuid.NullUUID) {
	connected, err := pluginsrepo.New(s.db).HasPluginGithubConnectionForProject(ctx, *authCtx.ProjectID)
	if err != nil {
		s.logger.WarnContext(ctx, "check marketplace connection after endpoint mutation", attr.SlogError(err))
		return
	}
	if !connected {
		return
	}
	attached, err := hasPluginMembershipForEndpoint(ctx, pluginsrepo.New(s.db), *authCtx.ProjectID, serverIDs, gatewayIDs)
	if err != nil {
		s.logger.ErrorContext(ctx, "check plugin membership after endpoint mutation", attr.SlogError(err))
		return
	}
	if attached {
		s.triggerPluginPublish(ctx, authCtx, true, false)
	}
}

func hasPluginMembershipForEndpoint(ctx context.Context, queries *pluginsrepo.Queries, projectID uuid.UUID, serverIDs, gatewayIDs []uuid.NullUUID) (bool, error) {
	for _, id := range gatewayIDs {
		if !id.Valid {
			continue
		}
		attached, err := queries.HasPluginMembershipForGateway(ctx, pluginsrepo.HasPluginMembershipForGatewayParams{ProjectID: projectID, GatewayID: id.UUID})
		if err != nil {
			return false, fmt.Errorf("check gateway plugin membership: %w", err)
		}
		if attached {
			return true, nil
		}
	}
	for _, id := range serverIDs {
		if !id.Valid {
			continue
		}
		attached, err := queries.HasPluginMembershipForMCPServer(ctx, pluginsrepo.HasPluginMembershipForMCPServerParams{ProjectID: projectID, McpServerID: id.UUID})
		if err != nil {
			return false, fmt.Errorf("check MCP plugin membership: %w", err)
		}
		if attached {
			return true, nil
		}
	}
	return false, nil
}
