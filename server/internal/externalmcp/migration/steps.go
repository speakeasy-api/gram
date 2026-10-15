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
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
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

// replacement is the remote-backed server a migration moves everything onto.
type replacement struct {
	id       uuid.UUID
	remoteID uuid.UUID
	url      string
	merged   bool
}

// migrateInTx runs every step of one migration inside tx. Nothing is
// committed here; the caller commits or, for a preview, rolls back.
func (m *Migrator) migrateInTx(ctx context.Context, tx pgx.Tx, actor Actor, request Request) (Outcome, error) {
	outcome := Outcome{ToolsetID: request.ToolsetID, Status: "", Reason: "", McpSlug: "", McpServerID: uuid.Nil, RemoteMcpServerID: uuid.Nil, MergedIntoExisting: false, RemoteURL: "", EndpointIDs: nil, Actions: nil, Warnings: nil}
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
	if _, err := repo.New(tx).LockToolset(ctx, repo.LockToolsetParams{ID: request.ToolsetID, ProjectID: projectID}); err != nil {
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
	issuerID := candidate.Row.UserSessionIssuerID.UUID
	outcome.McpSlug = toolset.McpSlug.String

	// Lock order matches hostedmcp: custom domains, then the wrapper's
	// endpoints, then the wrapper row.
	if _, err := hostedmcp.LockDomains(ctx, tx, organizationID, toolset.CustomDomainID); err != nil {
		return outcome, fmt.Errorf("lock custom domain: %w", err)
	}
	// The canonical wrapper's id is the toolset's, so its endpoints can be
	// locked before the wrapper row is.
	endpoints, err := mcpendpointsrepo.New(tx).LockMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.LockMCPEndpointsByMCPServerIDParams{McpServerID: toolset.ID, ProjectID: projectID})
	if err != nil {
		return outcome, fmt.Errorf("lock wrapper endpoints: %w", err)
	}
	wrapper, err := repo.New(tx).GetCanonicalWrapper(ctx, repo.GetCanonicalWrapperParams{ToolsetID: toolset.ID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return outcome, fmt.Errorf("lock canonical wrapper: %w", err)
	default:
		mode, err := networkaccess.Effective(wrapper.NetworkAccessMode)
		if err != nil {
			return outcome, fmt.Errorf("read wrapper network access: %w", err)
		}
		if !mode.IsPublicOnly() {
			return outcome, fail("uses private network access, which the migration does not carry over")
		}
	}

	target, err := m.resolveReplacement(ctx, tx, actor, request, candidate, toolset)
	if err != nil {
		return outcome, err
	}
	outcome.McpServerID = target.id
	outcome.RemoteMcpServerID = target.remoteID
	outcome.MergedIntoExisting = target.merged
	outcome.RemoteURL = target.url
	if target.merged {
		outcome.Actions = append(outcome.Actions, fmt.Sprintf("Merge into the existing remote server %s", target.id))
		if !sameUpstream(target.url, candidate.CleanedURL) {
			outcome.Warnings = append(outcome.Warnings, fmt.Sprintf("The existing server dials %s, not %s", target.url, candidate.CleanedURL))
		}
	} else {
		outcome.Actions = append(outcome.Actions, fmt.Sprintf("Create a remote server at %s on the existing sign-in provider", target.url))
	}
	if candidate.CleanedURL != candidate.RemoteURL {
		outcome.Actions = append(outcome.Actions, fmt.Sprintf("Drop the unfilled template from %s", candidate.RemoteURL))
	}

	moves := dependentMove{
		tx: tx, audit: m.audit, principal: principal, actorEmail: actor.Email,
		organizationID: organizationID, projectID: projectID, backendID: toolset.ID, targetID: target.id,
	}
	pluginRowsMoved, err := moves.run(ctx, &outcome)
	if err != nil {
		return outcome, err
	}

	if err := m.moveEndpoints(ctx, tx, principal, actor.Email, toolset, endpoints, target, &outcome); err != nil {
		return outcome, err
	}

	if err := remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, tx, organizationID, projectID, []uuid.UUID{issuerID}); err != nil {
		return outcome, fmt.Errorf("resync remote session issuer: %w", err)
	}
	if err := rewriteSessionResources(ctx, tx, issuerID, target, candidate.RemoteURL, &outcome); err != nil {
		return outcome, err
	}

	hostedActor := hostedmcp.Actor{UserID: actor.UserID, Email: actor.Email, System: ""}
	deleted, err := hostedmcp.Delete(ctx, tx, m.tracerProvider, m.audit, hostedActor, toolset)
	if err != nil {
		return outcome, fmt.Errorf("delete hosted wrapper: %w", err)
	}
	if len(deleted.DeletedRiskPolicies) > 0 {
		outcome.Warnings = append(outcome.Warnings, fmt.Sprintf("Deleted %d risk policies bound to the old server", len(deleted.DeletedRiskPolicies)))
	}
	if err := retireToolset(ctx, tx, m.audit, principal, actor.Email, toolset, &outcome); err != nil {
		return outcome, err
	}

	if pluginRowsMoved {
		if err := m.publication.Project(ctx, tx, organizationID, projectID, actor.UserID); err != nil {
			return outcome, fmt.Errorf("request plugin publication: %w", err)
		}
		outcome.Actions = append(outcome.Actions, "Republish the project's plugins")
	}

	endpointURNs := make([]urn.McpEndpoint, 0, len(outcome.EndpointIDs))
	for _, id := range outcome.EndpointIDs {
		endpointURNs = append(endpointURNs, urn.NewMcpEndpoint(id))
	}
	if err := m.audit.LogToolsetMigrateToRemoteMcp(ctx, tx, audit.LogToolsetMigrateToRemoteMcpEvent{
		OrganizationID: organizationID, ProjectID: projectID,
		Actor: principal, ActorDisplayName: actor.Email, ActorSlug: nil,
		ToolsetURN: urn.NewToolset(toolset.ID), ToolsetName: toolset.Name, ToolsetSlug: toolset.Slug,
		McpSlug: toolset.McpSlug.String, McpServerURN: urn.NewMcpServer(target.id), MergedIntoExisting: target.merged,
		PreviousRemoteURL: candidate.RemoteURL, RemoteURL: target.url, McpEndpointURNs: endpointURNs,
	}); err != nil {
		return outcome, fmt.Errorf("audit migration: %w", err)
	}
	return outcome, nil
}

// resolveReplacement locks the requested existing server or creates a new
// remote server on the toolset's own issuer. Creation passes the issuer
// explicitly, so no issuer is minted, and touches no remote session client.
func (m *Migrator) resolveReplacement(ctx context.Context, tx pgx.Tx, actor Actor, request Request, candidate Candidate, toolset toolsetsrepo.Toolset) (replacement, error) {
	issuerID := candidate.Row.UserSessionIssuerID
	if request.TargetMcpServerID.Valid {
		existing, err := repo.New(tx).LockTargetServer(ctx, repo.LockTargetServerParams{ID: request.TargetMcpServerID.UUID, ProjectID: toolset.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return replacement{}, fail("the selected server is not a live remote server in this project")
		}
		if err != nil {
			return replacement{}, fmt.Errorf("lock target server: %w", err)
		}
		if existing.UserSessionIssuerID != issuerID {
			return replacement{}, fail("the selected server signs in through a different provider")
		}
		return replacement{id: existing.ID, remoteID: uuid.Nil, url: existing.RemoteUrl, merged: true}, nil
	}

	if _, err := proxy.ValidateRemoteMCPURL(ctx, m.policy, candidate.CleanedURL); err != nil {
		return replacement{}, fail("upstream URL is not allowed: %s", err.Error())
	}
	environmentID, err := environmentForToolset(ctx, tx, toolset)
	if err != nil {
		return replacement{}, err
	}
	name := toolset.Name
	remote, err := remotemcp.CreateRemoteMCPSourceInTransaction(ctx, tx, m.audit, remotemcp.RemoteMCPSourceTransactionInput{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID,
		ActorUserID: actor.UserID, ActorEmail: actor.Email,
		Name: &name, URL: candidate.CleanedURL, TransportType: candidate.Row.TransportTypes[0],
	})
	if err != nil {
		return replacement{}, fmt.Errorf("create remote MCP source: %w", err)
	}
	server, err := mcpservers.CreateProjectMCPServerInTransaction(ctx, tx, m.audit, mcpservers.MCPServerTransactionInput{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID,
		ActorUserID: actor.UserID, ActorEmail: actor.Email,
		Name: toolset.Name, Visibility: hostedmcp.Visibility(toolset), NetworkAccessMode: networkaccess.ModePublicOnly,
		EnvironmentID: environmentID, UserSessionIssuerID: issuerID,
		RemoteMCPServerID:   uuid.NullUUID{UUID: remote.ID, Valid: true},
		TunneledMCPServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ToolsetID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, UnproxiedMCPServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if err != nil {
		return replacement{}, fmt.Errorf("create replacement MCP server: %w", err)
	}
	return replacement{id: server.ID, remoteID: remote.ID, url: remote.Url, merged: false}, nil
}

func environmentForToolset(ctx context.Context, tx pgx.Tx, toolset toolsetsrepo.Toolset) (uuid.NullUUID, error) {
	if !toolset.DefaultEnvironmentSlug.Valid {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	environment, err := environmentsrepo.New(tx).GetEnvironmentBySlug(ctx, environmentsrepo.GetEnvironmentBySlugParams{Slug: toolset.DefaultEnvironmentSlug.String, ProjectID: toolset.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, fmt.Errorf("read default environment: %w", err)
	}
	return uuid.NullUUID{UUID: environment.ID, Valid: true}, nil
}

// moveEndpoints repoints the wrapper's endpoints at the replacement, keeping
// each endpoint's id, slug and domain. A toolset served only from its legacy
// mcp_slug gets an endpoint for that slug instead. Either way the address
// moves in this transaction, before the toolset is deleted, so it never 404s.
func (m *Migrator) moveEndpoints(ctx context.Context, tx pgx.Tx, principal urn.Principal, actorEmail *string, toolset toolsetsrepo.Toolset, endpoints []mcpendpointsrepo.McpEndpoint, target replacement, outcome *Outcome) error {
	endpointRepo := mcpendpointsrepo.New(tx)
	for _, existing := range endpoints {
		updated, err := endpointRepo.UpdateMCPEndpoint(ctx, mcpendpointsrepo.UpdateMCPEndpointParams{
			CustomDomainID: existing.CustomDomainID, McpServerID: uuid.NullUUID{UUID: target.id, Valid: true},
			MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Slug: existing.Slug, IsDomainRoot: existing.IsDomainRoot,
			ID: existing.ID, ProjectID: toolset.ProjectID,
		})
		if err != nil {
			return fmt.Errorf("repoint endpoint: %w", err)
		}
		if err := m.audit.LogMcpEndpointUpdate(ctx, tx, audit.LogMcpEndpointUpdateEvent{
			OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID,
			Actor: principal, ActorDisplayName: actorEmail, ActorSlug: nil,
			McpEndpointURN: urn.NewMcpEndpoint(updated.ID), Slug: updated.Slug,
			McpEndpointSnapshotBefore: mv.BuildMcpEndpointView(existing), McpEndpointSnapshotAfter: mv.BuildMcpEndpointView(updated),
		}); err != nil {
			return fmt.Errorf("audit endpoint repoint: %w", err)
		}
		outcome.EndpointIDs = append(outcome.EndpointIDs, updated.ID)
		outcome.Actions = append(outcome.Actions, fmt.Sprintf("Move the address /mcp/%s to the replacement", updated.Slug))
	}
	if len(endpoints) > 0 {
		return nil
	}
	if !toolset.McpSlug.Valid || toolset.McpSlug.String == "" {
		outcome.Warnings = append(outcome.Warnings, "The server had no address to keep")
		return nil
	}

	slug := toolset.McpSlug.String
	if err := endpointRepo.LockSlugScope(ctx, mcpendpointsrepo.LockSlugScopeParams{CustomDomainID: toolset.CustomDomainID, Slug: slug}); err != nil {
		return fmt.Errorf("lock slug scope: %w", err)
	}
	available, err := endpointRepo.CheckUnifiedSlugAvailability(ctx, mcpendpointsrepo.CheckUnifiedSlugAvailabilityParams{
		SkipDomainCheck: true, CustomDomainID: toolset.CustomDomainID, OrganizationID: toolset.OrganizationID, Slug: slug,
		ExcludeToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ExcludeMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if err != nil {
		return fmt.Errorf("check slug availability: %w", err)
	}
	if !available.Valid || !available.Bool {
		return fail("the address /mcp/%s is held by another server", slug)
	}
	created, err := endpointRepo.CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: toolset.ProjectID, CustomDomainID: toolset.CustomDomainID,
		McpServerID: uuid.NullUUID{UUID: target.id, Valid: true}, MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Slug: slug,
	})
	if err != nil {
		return fmt.Errorf("create endpoint for legacy address: %w", err)
	}
	if err := m.audit.LogMcpEndpointCreate(ctx, tx, audit.LogMcpEndpointCreateEvent{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID,
		Actor: principal, ActorDisplayName: actorEmail, ActorSlug: nil,
		McpEndpointURN: urn.NewMcpEndpoint(created.ID), Slug: created.Slug,
	}); err != nil {
		return fmt.Errorf("audit endpoint creation: %w", err)
	}
	outcome.EndpointIDs = append(outcome.EndpointIDs, created.ID)
	outcome.Actions = append(outcome.Actions, fmt.Sprintf("Serve the address /mcp/%s from the replacement", slug))
	return nil
}

// rewriteSessionResources stamps the replacement's URL on the issuer's
// sessions, so upstream tokens route to it at once instead of after their
// next refresh. It is skipped when the issuer also fronts a remote server at
// another URL, where the resource would be ambiguous.
func rewriteSessionResources(ctx context.Context, tx pgx.Tx, issuerID uuid.UUID, target replacement, previousURL string, outcome *Outcome) error {
	queries := repo.New(tx)
	urls, err := queries.ListRemoteURLsOnIssuer(ctx, repo.ListRemoteURLsOnIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true}, ExcludedMcpServerID: target.id,
	})
	if err != nil {
		return fmt.Errorf("list remote URLs on issuer: %w", err)
	}
	for _, other := range urls {
		if !sameUpstream(other, target.url) {
			outcome.Warnings = append(outcome.Warnings, "Signed-in sessions were left as they are because the sign-in provider also serves another upstream; people may need to sign in again")
			return nil
		}
	}
	rewritten, err := queries.RewriteRemoteSessionResources(ctx, repo.RewriteRemoteSessionResourcesParams{
		Resource: conv.ToPGText(target.url), UserSessionIssuerID: issuerID, PreviousResource: previousURL,
	})
	if err != nil {
		return fmt.Errorf("rewrite remote session resources: %w", err)
	}
	if rewritten > 0 {
		outcome.Actions = append(outcome.Actions, fmt.Sprintf("Point %d signed-in sessions at the new upstream URL", rewritten))
	}
	return nil
}

// retireToolset soft-deletes the toolset and its remaining assistant rows,
// as toolsets.DeleteToolset does once its dependents are gone.
func retireToolset(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, principal urn.Principal, actorEmail *string, toolset toolsetsrepo.Toolset, outcome *Outcome) error {
	toolsets := toolsetsrepo.New(tx)
	if _, err := toolsets.DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: toolset.Slug, ProjectID: toolset.ProjectID}); err != nil {
		return fmt.Errorf("delete toolset: %w", err)
	}
	if err := toolsets.DeleteAssistantToolsetsByToolset(ctx, toolsetsrepo.DeleteAssistantToolsetsByToolsetParams{ToolsetID: toolset.ID, ProjectID: toolset.ProjectID}); err != nil {
		return fmt.Errorf("delete assistant toolsets: %w", err)
	}
	if err := auditLogger.LogToolsetDelete(ctx, tx, audit.LogToolsetDeleteEvent{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID,
		Actor: principal, ActorDisplayName: actorEmail, ActorSlug: nil,
		ToolsetURN: urn.NewToolset(toolset.ID), ToolsetName: toolset.Name, ToolsetSlug: toolset.Slug,
	}); err != nil {
		return fmt.Errorf("audit toolset deletion: %w", err)
	}
	outcome.Actions = append(outcome.Actions, "Delete the old toolset")
	return nil
}
