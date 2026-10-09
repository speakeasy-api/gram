// Package tombstone is the MCP server deletion shared by the server API and hosted wrappers.
package tombstone

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policylifecycle"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Locked is a server pinned for deletion with the root endpoints it held.
type Locked struct {
	Server        repo.McpServer
	RootEndpoints []mcpendpointsrepo.McpEndpoint
}

// Lock takes the domain -> endpoint -> server locks; pgx.ErrNoRows means the server is gone.
// Callers must take the project's Shadow MCP admission lock first, because
// Tombstone's risk policy cleanup needs it and other writers take it before
// the server row lock.
func Lock(ctx context.Context, tx pgx.Tx, organizationID string, projectID, serverID uuid.UUID) (Locked, error) {
	endpoints := mcpendpointsrepo.New(tx)
	domainIDs, err := endpoints.ListCustomDomainIDsByMCPServerID(ctx, mcpendpointsrepo.ListCustomDomainIDsByMCPServerIDParams{McpServerID: serverID, ProjectID: projectID})
	if err != nil {
		return Locked{}, fmt.Errorf("list custom domains for mcp server: %w", err)
	}
	if _, err := LockCustomDomains(ctx, tx, organizationID, domainIDs); err != nil {
		return Locked{}, fmt.Errorf("lock custom domains: %w", err)
	}
	if _, err := endpoints.LockMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.LockMCPEndpointsByMCPServerIDParams{McpServerID: serverID, ProjectID: projectID}); err != nil {
		return Locked{}, fmt.Errorf("lock mcp endpoints: %w", err)
	}
	server, err := repo.New(tx).LockMCPServerByIDAndProjectID(ctx, repo.LockMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	if err != nil {
		return Locked{}, fmt.Errorf("lock mcp server: %w", err)
	}
	// Authoritative root set: root selection holds the server FOR SHARE.
	roots, err := endpoints.LockMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.LockMCPEndpointsByMCPServerIDParams{McpServerID: serverID, ProjectID: projectID})
	if err != nil {
		return Locked{}, fmt.Errorf("lock root mcp endpoints: %w", err)
	}
	roots = slices.DeleteFunc(roots, func(endpoint mcpendpointsrepo.McpEndpoint) bool {
		return !endpoint.IsDomainRoot.Valid || !endpoint.IsDomainRoot.Bool
	})
	return Locked{Server: server, RootEndpoints: roots}, nil
}

// Input identifies the tenant and actor a tombstone is audited under.
type Input struct {
	OrganizationID string
	ProjectID      uuid.UUID
	ActorUserID    string
	ActorEmail     *string

	// TracerProvider traces the risk policy cleanup the delete runs.
	TracerProvider trace.TracerProvider
}

// Result is the tombstoned server, how many plugin attachments it lost, and
// the risk policies deleted with it, whose results the caller cleans post-commit.
type Result struct {
	Server                repo.McpServer
	DetachedPluginServers int
	DeletedRiskPolicies   []uuid.UUID
}

// Tombstone soft-deletes a locked server and its attachments; the caller audits the server delete.
func Tombstone(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, locked Locked, input Input) (Result, error) {
	if tx == nil || auditLogger == nil || input.OrganizationID == "" || input.ProjectID == uuid.Nil || !ActorPresent(ctx, input.ActorUserID) {
		return Result{}, errors.New("invalid MCP server tombstone input")
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, input.ActorUserID)
	servers := repo.New(tx)

	deleted, err := servers.DeleteMCPServer(ctx, repo.DeleteMCPServerParams{ID: locked.Server.ID, ProjectID: input.ProjectID})
	if err != nil {
		return Result{}, fmt.Errorf("delete mcp server: %w", err)
	}
	if err := servers.DeleteAssistantMCPServersByMCPServer(ctx, repo.DeleteAssistantMCPServersByMCPServerParams{McpServerID: deleted.ID, ProjectID: input.ProjectID}); err != nil {
		return Result{}, fmt.Errorf("detach assistant mcp servers: %w", err)
	}
	deletedPolicies, err := policylifecycle.NewCleaner(input.TracerProvider, auditLogger).SoftDeleteForMCPServer(ctx, tx, input.OrganizationID, input.ProjectID, deleted.ID, policylifecycle.Actor{
		Principal:   actor,
		DisplayName: input.ActorEmail,
		Slug:        nil,
	})
	if err != nil {
		return Result{}, fmt.Errorf("clean up mcp server risk policies: %w", err)
	}

	// The endpoint FK only cascades on hard deletes.
	deletedEndpoints, err := mcpendpointsrepo.New(tx).SoftDeleteMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.SoftDeleteMCPEndpointsByMCPServerIDParams{McpServerID: deleted.ID, ProjectID: input.ProjectID})
	if err != nil {
		return Result{}, fmt.Errorf("delete child mcp endpoints: %w", err)
	}
	if err := LogRootAutoClears(ctx, tx, auditLogger, input.OrganizationID, actor, input.ActorEmail, locked.RootEndpoints); err != nil {
		return Result{}, fmt.Errorf("log automatic root endpoint cleanup: %w", err)
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
			return Result{}, fmt.Errorf("log mcp endpoint deletion: %w", err)
		}
	}

	// A live attachment would keep holding its display name.
	detachedPluginServers, err := pluginsrepo.New(tx).SoftDeletePluginServersByMCPServerID(ctx, pluginsrepo.SoftDeletePluginServersByMCPServerIDParams{
		ProjectID:   input.ProjectID,
		McpServerID: uuid.NullUUID{UUID: deleted.ID, Valid: true},
	})
	if err != nil {
		return Result{}, fmt.Errorf("detach mcp server from plugins: %w", err)
	}

	deletedMemberships, err := metamcprepo.New(tx).DeleteMetaMCPMembersByMCPServerID(ctx, metamcprepo.DeleteMetaMCPMembersByMCPServerIDParams{McpServerID: deleted.ID, ProjectID: input.ProjectID})
	if err != nil {
		return Result{}, fmt.Errorf("delete meta mcp memberships: %w", err)
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
			return Result{}, fmt.Errorf("log meta mcp membership removal: %w", err)
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
			MetaMcpServerURN: nil,
		}); err != nil {
			return Result{}, fmt.Errorf("log mcp server plugin detachment: %w", err)
		}
	}

	return Result{Server: deleted, DetachedPluginServers: len(detachedPluginServers), DeletedRiskPolicies: deletedPolicies}, nil
}

// ActorPresent reports whether a write has an actor: a user id, or an agent
// principal whose audit rows the audit logger attributes to the agent.
func ActorPresent(ctx context.Context, userID string) bool {
	if userID != "" {
		return true
	}
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	return ok && actor.Type == urn.PrincipalTypeAgent
}

// LockCustomDomains locks the organization's live custom domains in id order
// and reports the deleted ones; a deleted domain already retired its endpoints.
func LockCustomDomains(ctx context.Context, tx pgx.Tx, organizationID string, domainIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	sorted := slices.Clone(domainIDs)
	slices.SortFunc(sorted, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	repository := customdomainsrepo.New(tx)
	dead := map[uuid.UUID]bool{}
	for _, domainID := range slices.Compact(sorted) {
		_, err := repository.LockCustomDomainByIDAndOrganization(ctx, customdomainsrepo.LockCustomDomainByIDAndOrganizationParams{ID: domainID, OrganizationID: organizationID})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			dead[domainID] = true
		case err != nil:
			return nil, fmt.Errorf("lock custom domain %s: %w", domainID, err)
		}
	}
	return dead, nil
}

// RootDomainIDs returns the distinct, sorted custom domains of endpoints.
func RootDomainIDs(endpoints []mcpendpointsrepo.McpEndpoint) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.CustomDomainID.Valid && !slices.Contains(result, endpoint.CustomDomainID.UUID) {
			result = append(result, endpoint.CustomDomainID.UUID)
		}
	}
	slices.SortFunc(result, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return result
}

// LogRootAutoClears audits each custom domain whose root endpoint was cleared as a side effect.
func LogRootAutoClears(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, organizationID string, actor urn.Principal, actorDisplayName *string, rootEndpoints []mcpendpointsrepo.McpEndpoint) error {
	if auditLogger == nil || organizationID == "" {
		return fmt.Errorf("invalid MCP root cleanup audit input")
	}
	repository := customdomainsrepo.New(tx)
	for _, endpoint := range rootEndpoints {
		if !endpoint.CustomDomainID.Valid {
			continue
		}
		domain, err := repository.GetCustomDomainByIDAndOrganization(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{ID: endpoint.CustomDomainID.UUID, OrganizationID: organizationID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue // A deleted domain has no root left to clear.
		}
		if err != nil {
			return fmt.Errorf("load custom domain for MCP root cleanup audit: %w", err)
		}
		if err := auditLogger.LogCustomDomainUpdate(ctx, tx, audit.LogCustomDomainUpdateEvent{
			OrganizationID:             organizationID,
			Actor:                      actor,
			ActorDisplayName:           actorDisplayName,
			ActorSlug:                  nil,
			CustomDomainURN:            urn.NewCustomDomain(domain.ID),
			DomainName:                 domain.Domain,
			CustomDomainSnapshotBefore: mv.BuildCustomDomainView(domain, false, endpoint.ID),
			CustomDomainSnapshotAfter:  mv.BuildCustomDomainView(domain, false, uuid.Nil),
		}); err != nil {
			return fmt.Errorf("audit MCP root cleanup: %w", err)
		}
	}
	return nil
}
