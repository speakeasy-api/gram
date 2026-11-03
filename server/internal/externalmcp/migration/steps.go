package migration

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func lockNetworkIngress(ctx context.Context, tx pgx.Tx, organizationID string) error {
	if err := networkingressrepo.New(tx).AcquireNetworkIngressOrganizationLock(ctx, organizationID); err != nil {
		return fmt.Errorf("lock network ingress lifecycle: %w", err)
	}
	return nil
}

func lockAdmission(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) error {
	if err := admission.LockProject(ctx, tx, projectID); err != nil {
		return fmt.Errorf("lock project admission: %w", err)
	}
	return nil
}

// migrateInTx runs every step of one migration inside tx. Nothing is
// committed here; the caller commits or, for a preview, rolls back.
func (m *Migrator) migrateInTx(ctx context.Context, tx pgx.Tx, actor Actor, request Request) (Outcome, error) {
	outcome := Outcome{ToolsetID: request.ToolsetID, Action: "", Status: "", Reason: "", McpSlug: "", McpServerID: uuid.Nil, RemoteMcpServerID: uuid.Nil, RemoteURL: "", Actions: nil, Warnings: nil}
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, actor.UserID)

	// The scope locks need the toolset's organization and project, which are
	// read before the locks and confirmed after them.
	unlocked, err := loadCandidate(ctx, tx, request.ToolsetID)
	if err != nil {
		return outcome, err
	}
	if unlocked == nil {
		return outcome, errNotCandidate
	}
	organizationID, projectID := unlocked.Row.OrganizationID, unlocked.Row.ProjectID
	if err := lockScope(ctx, tx, organizationID, projectID); err != nil {
		return outcome, err
	}
	queries := repo.New(tx)
	if _, err := queries.LockToolset(ctx, repo.LockToolsetParams{ID: request.ToolsetID, ProjectID: projectID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return outcome, errNotCandidate
		}
		return outcome, fmt.Errorf("lock toolset: %w", err)
	}
	reloaded, err := loadCandidate(ctx, tx, request.ToolsetID)
	if err != nil {
		return outcome, err
	}
	if reloaded == nil || reloaded.Row.ProjectID != projectID {
		return outcome, errNotCandidate
	}
	candidate := *reloaded
	if len(candidate.Blockers) > 0 {
		return outcome, fail("%s", strings.Join(candidate.Blockers, "; "))
	}
	toolset, err := toolsetsrepo.New(tx).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: request.ToolsetID, ProjectID: projectID})
	if err != nil {
		return outcome, fmt.Errorf("read locked toolset: %w", err)
	}
	outcome.Action = candidate.Action
	if candidate.Action == ActionDelete {
		outcome.McpSlug = candidate.Row.EndpointSlug
		err := m.deleteInTx(ctx, tx, actor, toolset, candidate.Row.EndpointSlug, &outcome)
		return outcome, err
	}
	outcome.McpSlug = candidate.Row.EndpointSlug
	outcome.Warnings = append(outcome.Warnings, candidate.Warnings...)

	// Lock order matches hostedmcp: custom domains, then the server's
	// endpoints, then the server row, which the switch locks as it updates it.
	if _, err := hostedmcp.LockDomains(ctx, tx, organizationID, candidate.Row.EndpointCustomDomainID); err != nil {
		return outcome, fmt.Errorf("lock custom domain: %w", err)
	}
	serverID, err := queries.GetToolsetServerID(ctx, repo.GetToolsetServerIDParams{ToolsetID: conv.ToNullUUID(toolset.ID), ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return outcome, fail("%s", blockerNoServer)
	}
	if err != nil {
		return outcome, fmt.Errorf("read toolset server: %w", err)
	}
	endpoints, err := mcpendpointsrepo.New(tx).LockMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.LockMCPEndpointsByMCPServerIDParams{McpServerID: serverID, ProjectID: projectID})
	if err != nil {
		return outcome, fmt.Errorf("lock server endpoints: %w", err)
	}
	// The domain lock above came from the candidate read; the locked endpoint must still be that address.
	if len(endpoints) != 1 || endpoints[0].Slug != candidate.Row.EndpointSlug || endpoints[0].CustomDomainID != candidate.Row.EndpointCustomDomainID {
		return outcome, fail("its address changed during the migration; run it again")
	}
	outcome.McpServerID = serverID
	server, err := mcpserversrepo.New(tx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	if err != nil {
		return outcome, fmt.Errorf("read toolset server: %w", err)
	}

	url := candidate.ReplacementURL
	if _, err := proxy.ValidateRemoteMCPURL(ctx, m.policy, url); err != nil {
		return outcome, fail("upstream URL is not allowed: %s", err.Error())
	}
	headers, err := m.planHeaders(ctx, tx, toolset, &outcome)
	if err != nil {
		return outcome, err
	}

	name := toolset.Name
	remote, err := remotemcp.CreateRemoteMCPSourceInTransaction(ctx, tx, m.audit, remotemcp.RemoteMCPSourceTransactionInput{
		OrganizationID: organizationID, ProjectID: projectID,
		ActorUserID: actor.UserID, ActorEmail: actor.Email,
		Name: &name, URL: url, TransportType: candidate.Row.TransportTypes[0],
	})
	if err != nil {
		return outcome, fmt.Errorf("create remote MCP source: %w", err)
	}
	outcome.RemoteMcpServerID = remote.ID
	outcome.RemoteURL = remote.Url
	outcome.Actions = append(outcome.Actions, fmt.Sprintf("Create a remote MCP source at %s", remote.Url))
	if len(candidate.Row.SessionResources) == 0 && url != candidate.RemoteURL {
		outcome.Actions = append(outcome.Actions, fmt.Sprintf("Drop the unfilled template from %s", candidate.RemoteURL))
	}
	if err := m.createHeaders(ctx, tx, organizationID, principal, actor.Email, remote, headers); err != nil {
		return outcome, err
	}
	outcome.Actions = append(outcome.Actions, describeHeaders(headers)...)

	if err := m.switchServer(ctx, tx, principal, actor.Email, toolset, serverID, remote.ID, candidate, &outcome); err != nil {
		return outcome, err
	}

	if hasUpstreamClient(candidate) {
		filled, err := queries.FillRemoteSessionResources(ctx, repo.FillRemoteSessionResourcesParams{
			Resource: conv.ToPGText(remote.Url), UserSessionIssuerID: candidate.Row.UserSessionIssuerID.UUID, ProjectID: projectID, OrganizationID: organizationID,
		})
		if err != nil {
			return outcome, fmt.Errorf("fill remote session resources: %w", err)
		}
		if filled > 0 {
			outcome.Actions = append(outcome.Actions, fmt.Sprintf("Record the upstream URL on %d signed-in sessions", filled))
		}
	}

	entries, err := queries.CountServerPluginEntries(ctx, repo.CountServerPluginEntriesParams{ProjectID: projectID, McpServerID: conv.ToNullUUID(serverID)})
	if err != nil {
		return outcome, fmt.Errorf("count plugin entries: %w", err)
	}
	if entries > 0 {
		if err := m.requestPublication(ctx, tx, organizationID, projectID, actor, &outcome); err != nil {
			return outcome, err
		}
	}

	if err := m.audit.LogToolsetMigrateToRemoteMcp(ctx, tx, audit.LogToolsetMigrateToRemoteMcpEvent{
		OrganizationID: organizationID, ProjectID: projectID,
		Actor: principal, ActorDisplayName: actor.Email, ActorSlug: nil,
		ToolsetURN: urn.NewToolset(toolset.ID), ToolsetName: toolset.Name, ToolsetSlug: toolset.Slug,
		McpSlug: server.Slug.String, McpServerURN: urn.NewMcpServer(serverID), RemoteMcpServerURN: urn.NewRemoteMcpServer(remote.ID),
		PreviousRemoteURL: candidate.RemoteURL, RemoteURL: remote.Url,
	}); err != nil {
		return outcome, fmt.Errorf("audit migration: %w", err)
	}
	return outcome, nil
}

// hasUpstreamClient reports whether the server's sign-in provider has an
// upstream client, and so upstream credentials to carry over.
func hasUpstreamClient(candidate Candidate) bool {
	return candidate.Row.UserSessionIssuerID.Valid && len(candidate.Row.RemoteSessionIssuerIds) > 0
}

// switchServer repoints the server row from the toolset to the remote source
// in one statement and records the change. The row keeps its id, slug,
// visibility, environment and network access, so its endpoint, plugin
// entries and access grants carry on unchanged.
func (m *Migrator) switchServer(ctx context.Context, tx pgx.Tx, principal urn.Principal, actorEmail *string, toolset toolsetsrepo.Toolset, serverID uuid.UUID, remoteID uuid.UUID, candidate Candidate, outcome *Outcome) error {
	servers := mcpserversrepo.New(tx)
	before, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: toolset.ProjectID})
	if err != nil {
		return fmt.Errorf("read server before switch: %w", err)
	}
	switched, err := repo.New(tx).SwitchServerToRemote(ctx, repo.SwitchServerToRemoteParams{
		RemoteMcpServerID:   conv.ToNullUUID(remoteID),
		UserSessionIssuerID: candidate.Row.UserSessionIssuerID,
		ID:                  serverID,
		ProjectID:           toolset.ProjectID,
		ToolsetID:           conv.ToNullUUID(toolset.ID),
	})
	if err != nil {
		return fmt.Errorf("switch server to remote source: %w", err)
	}
	if switched != 1 {
		return errNotCandidate
	}
	if candidate.Row.UserSessionIssuerID.Valid {
		if err := remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, tx, toolset.OrganizationID, toolset.ProjectID, []uuid.UUID{candidate.Row.UserSessionIssuerID.UUID}); err != nil {
			return fmt.Errorf("resync upstream issuer: %w", err)
		}
	}
	after, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: toolset.ProjectID})
	if err != nil {
		return fmt.Errorf("read server after switch: %w", err)
	}
	if hasUpstreamClient(candidate) && !after.RemoteSessionIssuerID.Valid {
		return fail("the sign-in provider's upstream authorization server could not be resolved for this project")
	}
	if err := m.audit.LogMcpServerUpdate(ctx, tx, audit.LogMcpServerUpdateEvent{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID,
		Actor: principal, ActorDisplayName: actorEmail, ActorSlug: nil,
		McpServerURN: urn.NewMcpServer(serverID), McpServerName: after.Name.String, McpServerSlug: after.Slug.String,
		McpServerSnapshotBefore: mv.BuildMcpServerView(before), McpServerSnapshotAfter: mv.BuildMcpServerView(after),
	}); err != nil {
		return fmt.Errorf("audit server switch: %w", err)
	}
	outcome.Actions = append(outcome.Actions, "Switch the server to the remote source, keeping its ID, address, plugins and access")
	return nil
}
