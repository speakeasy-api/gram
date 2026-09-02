package mcpservers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LockedMCPServer is a server pinned for deletion together with the root
// endpoints it held when locked, which the caller reconciles after commit.
type LockedMCPServer struct {
	Server        repo.McpServer
	RootEndpoints []mcpendpointsrepo.McpEndpoint
}

// LockMCPServerForDelete takes the domain -> endpoint -> server locks a tombstone needs.
func LockMCPServerForDelete(ctx context.Context, tx pgx.Tx, projectID, serverID uuid.UUID) (LockedMCPServer, error) {
	endpoints := mcpendpointsrepo.New(tx)

	affectedDomainIDs, err := endpoints.ListCustomDomainIDsByMCPServerID(ctx, mcpendpointsrepo.ListCustomDomainIDsByMCPServerIDParams{
		McpServerID: serverID,
		ProjectID:   projectID,
	})
	if err != nil {
		return LockedMCPServer{}, fmt.Errorf("list custom domains for mcp server: %w", err)
	}
	if err := lockMcpServerCustomDomains(ctx, tx, affectedDomainIDs); err != nil {
		return LockedMCPServer{}, fmt.Errorf("lock custom domains: %w", err)
	}
	if _, err := endpoints.LockMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.LockMCPEndpointsByMCPServerIDParams{
		McpServerID: serverID,
		ProjectID:   projectID,
	}); err != nil {
		return LockedMCPServer{}, fmt.Errorf("lock mcp endpoints: %w", err)
	}
	locked, err := repo.New(tx).LockMCPServerByIDAndProjectID(ctx, repo.LockMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: projectID,
	})
	if err != nil {
		return LockedMCPServer{}, fmt.Errorf("lock mcp server: %w", err)
	}

	// Read after the server lock: no new root can commit past this point, and
	// the rows carry their pre-delete is_domain_root.
	rootEndpoints, err := endpoints.LockMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.LockMCPEndpointsByMCPServerIDParams{
		McpServerID: serverID,
		ProjectID:   projectID,
	})
	if err != nil {
		return LockedMCPServer{}, fmt.Errorf("lock root mcp endpoints: %w", err)
	}
	rootEndpoints = slices.DeleteFunc(rootEndpoints, func(endpoint mcpendpointsrepo.McpEndpoint) bool {
		return !endpoint.IsDomainRoot.Valid || !endpoint.IsDomainRoot.Bool
	})

	return LockedMCPServer{Server: locked, RootEndpoints: rootEndpoints}, nil
}

// TombstoneInput identifies the tenant and actor a tombstone is audited under.
type TombstoneInput struct {
	OrganizationID string
	ProjectID      uuid.UUID
	ActorUserID    string
	ActorEmail     *string
}

// TombstoneMCPServerInTransaction soft-deletes a locked server and its child rows; the caller writes the server delete event.
func TombstoneMCPServerInTransaction(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, locked LockedMCPServer, input TombstoneInput) (repo.McpServer, error) {
	if tx == nil || auditLogger == nil || input.OrganizationID == "" || input.ProjectID == uuid.Nil || input.ActorUserID == "" {
		return repo.McpServer{}, errors.New("invalid MCP server tombstone input")
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, input.ActorUserID)

	// Children first: the endpoint FK only cascades on hard deletes, and
	// nothing may resolve to a deleted server after commit.
	deletedEndpoints, err := mcpendpointsrepo.New(tx).SoftDeleteMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.SoftDeleteMCPEndpointsByMCPServerIDParams{
		McpServerID: locked.Server.ID,
		ProjectID:   input.ProjectID,
	})
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("delete child mcp endpoints: %w", err)
	}

	deleted, err := repo.New(tx).DeleteMCPServer(ctx, repo.DeleteMCPServerParams{
		ID:        locked.Server.ID,
		ProjectID: input.ProjectID,
	})
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("delete mcp server: %w", err)
	}
	if err := LogMCPServerRootAutoClears(ctx, tx, auditLogger, input.OrganizationID, actor, input.ActorEmail, locked.RootEndpoints); err != nil {
		return repo.McpServer{}, fmt.Errorf("log automatic root endpoint cleanup: %w", err)
	}
	for _, endpoint := range deletedEndpoints {
		if err := auditLogger.LogMcpEndpointDelete(ctx, tx, audit.LogMcpEndpointDeleteEvent{
			OrganizationID:   input.OrganizationID,
			ProjectID:        input.ProjectID,
			Actor:            actor,
			ActorDisplayName: input.ActorEmail,
			ActorSlug:        nil,
			McpEndpointURN:   urn.NewMcpEndpoint(endpoint.ID),
			Slug:             endpoint.Slug,
		}); err != nil {
			return repo.McpServer{}, fmt.Errorf("log mcp endpoint deletion: %w", err)
		}
	}

	// A live plugin attachment would keep holding the display name and block
	// a later same-named server from attaching.
	detachedPluginServers, err := pluginsrepo.New(tx).SoftDeletePluginServersByMCPServerID(ctx, pluginsrepo.SoftDeletePluginServersByMCPServerIDParams{
		ProjectID:   input.ProjectID,
		McpServerID: uuid.NullUUID{UUID: deleted.ID, Valid: true},
	})
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("detach mcp server from plugins: %w", err)
	}

	deletedMemberships, err := metamcprepo.New(tx).DeleteMetaMCPMembersByMCPServerID(ctx, metamcprepo.DeleteMetaMCPMembersByMCPServerIDParams{
		McpServerID: deleted.ID,
		ProjectID:   input.ProjectID,
	})
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("delete meta mcp memberships: %w", err)
	}
	for _, membership := range deletedMemberships {
		if err := auditLogger.LogMetaMcpMemberRemove(ctx, tx, audit.LogMetaMcpMemberEvent{
			OrganizationID:   input.OrganizationID,
			ProjectID:        input.ProjectID,
			Actor:            actor,
			ActorDisplayName: input.ActorEmail,
			ActorSlug:        nil,
			MetaMcpServerURN: urn.NewMetaMcpServer(membership.MetaMcpServerID),
			Name:             membership.MetaMcpServerName,
			MembershipURN:    urn.NewMetaMcpServerMember(membership.ID),
			McpServerURN:     urn.NewMcpServer(membership.McpServerID),
			SortOrder:        membership.SortOrder,
		}); err != nil {
			return repo.McpServer{}, fmt.Errorf("log meta mcp membership removal: %w", err)
		}
	}

	deletedServerURN := urn.NewMcpServer(deleted.ID)
	for _, pluginServer := range detachedPluginServers {
		if err := auditLogger.LogPluginServerRemove(ctx, tx, audit.LogPluginServerRemoveEvent{
			OrganizationID:   input.OrganizationID,
			ProjectID:        input.ProjectID,
			Actor:            actor,
			ActorDisplayName: input.ActorEmail,
			ActorSlug:        nil,
			PluginID:         pluginServer.PluginID,
			PluginName:       pluginServer.PluginName,
			PluginSlug:       pluginServer.PluginSlug,
			ServerID:         pluginServer.ID,
			ToolsetURN:       nil,
			McpServerURN:     &deletedServerURN,
		}); err != nil {
			return repo.McpServer{}, fmt.Errorf("log mcp server plugin detachment: %w", err)
		}
	}

	return deleted, nil
}
