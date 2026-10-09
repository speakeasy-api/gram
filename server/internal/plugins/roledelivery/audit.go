package roledelivery

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/audit"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Content automation records the same content events as manual additions and
// removals. The originating grant/audience mutation separately records its actor.
func auditChange(ctx context.Context, tx pgx.Tx, org string, projectID, pluginID uuid.UUID, membership pluginsrepo.PluginServer, added bool) error {
	return auditChangeAs(ctx, tx, org, projectID, pluginID, membership, added, urn.NewSystemPrincipal("automatic-role-distribution"))
}

func auditChangeAs(ctx context.Context, tx pgx.Tx, org string, projectID, pluginID uuid.UUID, membership pluginsrepo.PluginServer, added bool, actor urn.Principal) error {
	plugin, err := pluginsrepo.New(tx).GetPlugin(ctx, pluginsrepo.GetPluginParams{ID: pluginID, OrganizationID: org, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("load role delivery audit plugin: %w", err)
	}
	var toolsetURN *urn.Toolset
	var mcpURN *urn.McpServer
	if membership.ToolsetID.Valid {
		value := urn.NewToolset(membership.ToolsetID.UUID)
		toolsetURN = &value
	}
	if membership.McpServerID.Valid {
		value := urn.NewMcpServer(membership.McpServerID.UUID)
		mcpURN = &value
	}
	var actorDisplayName *string
	if actor.Type == urn.PrincipalTypeSystem {
		name := "Speakeasy"
		actorDisplayName = &name
	}
	logger := audit.NewLogger()
	if added {
		if err := logger.LogPluginServerAdd(ctx, tx, audit.LogPluginServerAddEvent{OrganizationID: org, ProjectID: projectID, Actor: actor, ActorDisplayName: actorDisplayName, ActorSlug: nil, PluginID: pluginID, PluginName: plugin.Name, PluginSlug: plugin.Slug, ServerID: membership.ID, ServerDisplayName: membership.DisplayName, ServerPolicy: membership.Policy, ServerSortOrder: membership.SortOrder, ToolsetURN: toolsetURN, McpServerURN: mcpURN, MetaMcpServerURN: nil}); err != nil {
			return fmt.Errorf("audit role delivery membership: %w", err)
		}
		return nil
	}
	if err := logger.LogPluginServerRemove(ctx, tx, audit.LogPluginServerRemoveEvent{OrganizationID: org, ProjectID: projectID, Actor: actor, ActorDisplayName: actorDisplayName, ActorSlug: nil, PluginID: pluginID, PluginName: plugin.Name, PluginSlug: plugin.Slug, ServerID: membership.ID, ToolsetURN: toolsetURN, McpServerURN: mcpURN, MetaMcpServerURN: nil}); err != nil {
		return fmt.Errorf("audit role delivery membership: %w", err)
	}
	return nil
}
