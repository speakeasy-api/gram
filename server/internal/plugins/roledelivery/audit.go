package roledelivery

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/audit"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// memberRow is a plugin_servers row as the role delivery queries return it.
type memberRow struct {
	ID              uuid.UUID
	PluginID        uuid.UUID
	ProjectID       uuid.NullUUID
	McpServerID     uuid.NullUUID
	MetaMcpServerID uuid.NullUUID
	DisplayName     string
	Policy          string
	SortOrder       int32
	CreatedAt       pgtype.Timestamptz
	UpdatedAt       pgtype.Timestamptz
	DeletedAt       pgtype.Timestamptz
	Deleted         bool
}

// Content automation records the same content events as manual additions and
// removals. The originating grant/audience mutation separately records its actor.
func auditChange(ctx context.Context, tx pgx.Tx, org string, projectID, pluginID uuid.UUID, membership memberRow, added bool) error {
	plugin, err := pluginsrepo.New(tx).GetPlugin(ctx, pluginsrepo.GetPluginParams{ID: pluginID, OrganizationID: org, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("load role delivery audit plugin: %w", err)
	}
	var mcpURN *urn.McpServer
	if membership.McpServerID.Valid {
		value := urn.NewMcpServer(membership.McpServerID.UUID)
		mcpURN = &value
	}
	var gatewayURN *urn.MetaMcpServer
	if membership.MetaMcpServerID.Valid {
		value := urn.NewMetaMcpServer(membership.MetaMcpServerID.UUID)
		gatewayURN = &value
	}
	actor := urn.NewSystemPrincipal("automatic-role-distribution")
	actorDisplayName := "Speakeasy"
	logger := audit.NewLogger()
	if added {
		if err := logger.LogPluginServerAdd(ctx, tx, audit.LogPluginServerAddEvent{OrganizationID: org, ProjectID: projectID, Actor: actor, ActorDisplayName: &actorDisplayName, ActorSlug: nil, PluginID: pluginID, PluginName: plugin.Name, PluginSlug: plugin.Slug, ServerID: membership.ID, ServerDisplayName: membership.DisplayName, ServerPolicy: membership.Policy, ServerSortOrder: membership.SortOrder, ToolsetURN: nil, McpServerURN: mcpURN, MetaMcpServerURN: gatewayURN}); err != nil {
			return fmt.Errorf("audit role delivery membership: %w", err)
		}
		return nil
	}
	if err := logger.LogPluginServerRemove(ctx, tx, audit.LogPluginServerRemoveEvent{OrganizationID: org, ProjectID: projectID, Actor: actor, ActorDisplayName: &actorDisplayName, ActorSlug: nil, PluginID: pluginID, PluginName: plugin.Name, PluginSlug: plugin.Slug, ServerID: membership.ID, ToolsetURN: nil, McpServerURN: mcpURN, MetaMcpServerURN: gatewayURN}); err != nil {
		return fmt.Errorf("audit role delivery membership: %w", err)
	}
	return nil
}
