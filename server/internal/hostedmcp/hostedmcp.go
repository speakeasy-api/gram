// Package hostedmcp syncs a toolset's canonical wrapper (mcp_servers.id = toolset id) and its endpoint.
//
//nolint:exhaustruct // Hosted policy, endpoint, and audit literals omit optional fields intentionally.
package hostedmcp

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/audit"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tombstone"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ErrAddressInUse marks a Sync conflict caused by another server holding the hosted address.
var ErrAddressInUse = errors.New("hosted MCP address is already in use")

// Actor is who wrapper writes are audited under: a user, or a server component when UserID is empty.
type Actor struct {
	// UserID is the acting user; it takes precedence over System.
	UserID string

	// Email is the acting user's display name in audit entries.
	Email *string

	// System names a server component acting with no user behind it, audited as system:<System>.
	System string
}

// SystemActor audits Sync writes as the named server component; Delete takes users and agents only.
func SystemActor(component string) Actor {
	return Actor{UserID: "", Email: nil, System: component}
}

func (a Actor) principal(ctx context.Context) (urn.Principal, bool) {
	switch {
	case a.UserID != "":
		return urn.NewPrincipal(urn.PrincipalTypeUser, a.UserID), true
	case a.System != "":
		return urn.NewSystemPrincipal(a.System), true
	case tombstone.ActorPresent(ctx, ""):
		// The audit logger attributes rows to the agent in context.
		return urn.NewPrincipal(urn.PrincipalTypeUser, ""), true
	default:
		return urn.Principal{}, false
	}
}

// Visibility maps a toolset's (mcp_enabled, mcp_is_public) onto mcp_servers.visibility.
func Visibility(toolset toolsetsrepo.Toolset) string {
	switch {
	case !toolset.McpEnabled:
		return visibility.Disabled
	case toolset.McpIsPublic:
		return visibility.Public
	default:
		return visibility.Private
	}
}

// LockDomains locks the organization's custom domains in id order and reports deleted ones.
func LockDomains(ctx context.Context, tx pgx.Tx, organizationID string, ids ...uuid.NullUUID) (map[uuid.UUID]bool, error) {
	valid := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id.Valid {
			valid = append(valid, id.UUID)
		}
	}
	return tombstone.LockCustomDomains(ctx, tx, organizationID, valid) //nolint:wrapcheck // tombstone already names the failing domain.
}

// Sync mirrors a locked toolset onto its canonical wrapper; returned domains need a post-commit reconcile.
func Sync(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor Actor, toolset toolsetsrepo.Toolset, requested *networkaccess.Mode) ([]uuid.UUID, error) {
	principal, ok := actor.principal(ctx)
	if auditLogger == nil || !ok {
		return nil, oops.E(oops.CodeUnauthorized, nil, "missing hosted MCP actor")
	}
	hasSlug := toolset.McpSlug.Valid && toolset.McpSlug.String != ""
	if !hasSlug && requested != nil {
		return nil, oops.E(oops.CodeInvalid, nil, "hosted MCP needs a slug before network access can be configured")
	}
	servers := mcpserversrepo.New(tx)
	endpointRepo := mcpendpointsrepo.New(tx)

	// Domain -> endpoint -> server: the order root selection and deletion take.
	endpointDomains, err := endpointRepo.ListCustomDomainIDsByMCPServerID(ctx, mcpendpointsrepo.ListCustomDomainIDsByMCPServerIDParams{McpServerID: toolset.ID, ProjectID: toolset.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list hosted MCP domains")
	}
	domainIDs := []uuid.NullUUID{toolset.CustomDomainID}
	for _, id := range endpointDomains {
		domainIDs = append(domainIDs, uuid.NullUUID{UUID: id, Valid: true})
	}
	dead, err := LockDomains(ctx, tx, toolset.OrganizationID, domainIDs...)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock hosted MCP domains")
	}
	endpoints, err := endpointRepo.LockMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.LockMCPEndpointsByMCPServerIDParams{McpServerID: toolset.ID, ProjectID: toolset.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock hosted MCP endpoints")
	}
	canonical, err := servers.LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "load hosted MCP server")
	}
	created := errors.Is(err, pgx.ErrNoRows)
	if created && !hasSlug {
		return nil, nil
	}
	if !created && (!canonical.ToolsetID.Valid || canonical.ToolsetID.UUID != toolset.ID) {
		return nil, oops.E(oops.CodeConflict, nil, "hosted MCP identity belongs to another server")
	}
	if created {
		// A legacy toolset whose address is taken stays legacy rather than failing its own save.
		held, err := addressHeld(ctx, tx, toolset, !toolset.CustomDomainID.Valid || !dead[toolset.CustomDomainID.UUID])
		if err != nil {
			return nil, err
		}
		if held && requested != nil {
			return nil, oops.E(oops.CodeConflict, ErrAddressInUse, "hosted MCP address is already in use; free it before configuring network access")
		}
		if held {
			return nil, nil
		}
	}

	mode := networkaccess.ModePublicOnly
	if !created {
		mode, err = networkaccess.Effective(canonical.NetworkAccessMode)
		if err != nil && requested == nil {
			return nil, oops.E(oops.CodeUnexpected, err, "invalid stored hosted MCP network mode")
		}
	}
	storedMode := mode
	if requested != nil {
		mode = *requested
	}
	wantVisibility := Visibility(toolset)
	// A deleted domain took its endpoints with it; there is no address to restore.
	addressed := hasSlug && (!toolset.CustomDomainID.Valid || !dead[toolset.CustomDomainID.UUID])
	addressChanged := !created && addressed && (len(endpoints) != 1 || endpoints[0].Slug != toolset.McpSlug.String || endpoints[0].CustomDomainID != toolset.CustomDomainID)
	// Private access needs a live ingress only when newly selected or moved.
	if mode != networkaccess.ModePublicOnly && toolset.McpEnabled &&
		((requested != nil && (created || mode != storedMode)) || addressChanged || canonical.Visibility == visibility.Disabled) {
		ingress, ingressErr := networkingressrepo.New(tx).GetNetworkIngressByOrganization(ctx, toolset.OrganizationID)
		if ingressErr != nil || !ingress.Enabled || ingress.Deleted || ingress.Status != "online" || !ingress.DnsName.Valid {
			return nil, oops.E(oops.CodeInvalid, ingressErr, "online private network ingress is required")
		}
		if ingress.EndpointNamespaceKind != "platform" && ingress.EndpointNamespaceKind != "custom_domain" {
			return nil, oops.E(oops.CodeInvalid, nil, "private ingress namespace is invalid")
		}
		if (ingress.EndpointNamespaceKind == "platform" && toolset.CustomDomainID.Valid) ||
			(ingress.EndpointNamespaceKind == "custom_domain" && (!toolset.CustomDomainID.Valid || toolset.CustomDomainID != ingress.CustomDomainID)) {
			return nil, oops.E(oops.CodeInvalid, nil, "hosted MCP address does not match the private ingress namespace")
		}
	}

	var clearedRoots []mcpendpointsrepo.McpEndpoint
	switch {
	case created:
		canonical, err = servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
			ID: toolset.ID, ProjectID: toolset.ProjectID, Name: pgtype.Text{String: toolset.Name, Valid: true}, Slug: toolset.McpSlug,
			ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, UserSessionIssuerID: toolset.UserSessionIssuerID,
			ToolVariationsGroupID: toolset.ToolVariationsGroupID, Visibility: wantVisibility, NetworkAccessMode: networkaccess.Storage(mode),
		})
		if err != nil {
			return nil, oops.E(oops.CodeConflict, err, "create hosted MCP server")
		}
		if err := auditLogger.LogMcpServerCreate(ctx, tx, audit.LogMcpServerCreateEvent{
			OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID, Actor: principal, ActorDisplayName: actor.Email,
			McpServerURN: urn.NewMcpServer(toolset.ID), McpServerName: toolset.Name, McpServerSlug: toolset.McpSlug.String,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "audit hosted MCP creation")
		}
		if err := resyncIssuers(ctx, tx, toolset, toolset.UserSessionIssuerID); err != nil {
			return nil, err
		}
	case canonical.Name.String != toolset.Name || canonical.Slug != toolset.McpSlug || canonical.Visibility != wantVisibility ||
		canonical.UserSessionIssuerID != toolset.UserSessionIssuerID || canonical.ToolVariationsGroupID != toolset.ToolVariationsGroupID || mode != storedMode:
		previous := canonical
		storage := canonical.NetworkAccessMode
		if mode != storedMode {
			storage = networkaccess.Storage(mode)
		}
		canonical, err = servers.SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
			Name: pgtype.Text{String: toolset.Name, Valid: true}, Slug: toolset.McpSlug, Visibility: wantVisibility,
			UserSessionIssuerID: toolset.UserSessionIssuerID, ToolVariationsGroupID: toolset.ToolVariationsGroupID,
			NetworkAccessMode: storage, ID: toolset.ID, ProjectID: toolset.ProjectID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "update hosted MCP server")
		}
		if err := auditLogger.LogMcpServerUpdate(ctx, tx, audit.LogMcpServerUpdateEvent{
			OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID, Actor: principal, ActorDisplayName: actor.Email,
			McpServerURN: urn.NewMcpServer(toolset.ID), McpServerName: toolset.Name, McpServerSlug: toolset.McpSlug.String,
			McpServerSnapshotBefore: mv.BuildMcpServerView(previous), McpServerSnapshotAfter: mv.BuildMcpServerView(canonical),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "audit hosted MCP update")
		}
		if previous.UserSessionIssuerID != toolset.UserSessionIssuerID {
			if err := resyncIssuers(ctx, tx, toolset, previous.UserSessionIssuerID, toolset.UserSessionIssuerID); err != nil {
				return nil, err
			}
		}
	}

	// A disabled server holds no domain root, including one left over from before this sync.
	if wantVisibility == visibility.Disabled && slices.ContainsFunc(endpoints, isRoot) {
		clearedRoots, err = endpointRepo.ClearRootMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ClearRootMCPEndpointsByMCPServerIDParams{McpServerID: toolset.ID, ProjectID: toolset.ProjectID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "clear hosted MCP root endpoints")
		}
		if err := tombstone.LogRootAutoClears(ctx, tx, auditLogger, toolset.OrganizationID, principal, actor.Email, clearedRoots); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "audit hosted MCP root cleanup")
		}
		// Later steps must see the cleared rows, or they would audit the same cleanup again.
		for i, endpoint := range endpoints {
			if j := slices.IndexFunc(clearedRoots, func(cleared mcpendpointsrepo.McpEndpoint) bool { return cleared.ID == endpoint.ID }); j >= 0 {
				endpoints[i] = clearedRoots[j]
			}
		}
	}

	if !hasSlug {
		// No slug, no address: endpoints retire but the wrapper stays, since a tombstoned id cannot be reused.
		retired, err := retireEndpoints(ctx, tx, auditLogger, principal, actor.Email, toolset, endpoints)
		if err != nil {
			return nil, err
		}
		return tombstone.RootDomainIDs(append(clearedRoots, retired...)), nil
	}
	if !addressed {
		return tombstone.RootDomainIDs(clearedRoots), nil
	}
	moved, err := syncEndpoint(ctx, tx, auditLogger, principal, actor.Email, toolset, canonical, endpoints)
	if err != nil {
		return nil, err
	}
	return tombstone.RootDomainIDs(append(clearedRoots, moved...)), nil
}

// addressHeld reports whether another live endpoint or server already holds the toolset's address or server slug.
func addressHeld(ctx context.Context, tx pgx.Tx, toolset toolsetsrepo.Toolset, addressed bool) (bool, error) {
	if addressed {
		endpointRepo := mcpendpointsrepo.New(tx)
		if err := endpointRepo.LockSlugScope(ctx, mcpendpointsrepo.LockSlugScopeParams{CustomDomainID: toolset.CustomDomainID, Slug: toolset.McpSlug.String}); err != nil {
			return false, oops.E(oops.CodeUnexpected, err, "lock hosted MCP address")
		}
		_, err := endpointRepo.GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByCustomDomainAndSlugParams{Slug: toolset.McpSlug.String, CustomDomainID: toolset.CustomDomainID})
		switch {
		case err == nil:
			return true, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return false, oops.E(oops.CodeUnexpected, err, "check hosted MCP address")
		}
	}
	_, err := mcpserversrepo.New(tx).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{Slug: toolset.McpSlug, ProjectID: toolset.ProjectID})
	switch {
	case err == nil:
		return true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return false, oops.E(oops.CodeUnexpected, err, "check hosted MCP server slug")
	}
	return false, nil
}

// syncEndpoint re-keys the single endpoint in place so client references keep its identity.
func syncEndpoint(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, principal urn.Principal, actorEmail *string, toolset toolsetsrepo.Toolset, server mcpserversrepo.McpServer, endpoints []mcpendpointsrepo.McpEndpoint) ([]mcpendpointsrepo.McpEndpoint, error) {
	if len(endpoints) > 1 {
		return nil, oops.E(oops.CodeConflict, nil, "hosted MCP has multiple endpoints; resolve them before editing the toolset")
	}
	if len(endpoints) == 1 && endpoints[0].Slug == toolset.McpSlug.String && endpoints[0].CustomDomainID == toolset.CustomDomainID {
		return nil, nil
	}
	endpointRepo := mcpendpointsrepo.New(tx)
	if err := endpointRepo.LockSlugScope(ctx, mcpendpointsrepo.LockSlugScopeParams{CustomDomainID: toolset.CustomDomainID, Slug: toolset.McpSlug.String}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock hosted MCP address")
	}
	available, err := endpointRepo.CheckUnifiedSlugAvailability(ctx, mcpendpointsrepo.CheckUnifiedSlugAvailabilityParams{
		Slug: toolset.McpSlug.String, CustomDomainID: toolset.CustomDomainID, OrganizationID: toolset.OrganizationID,
		ExcludeToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ExcludeMcpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
		SkipDomainCheck: true, // The toolset owns this address scope.
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check hosted MCP address")
	}
	if !available.Valid || !available.Bool {
		return nil, oops.E(oops.CodeConflict, ErrAddressInUse, "hosted MCP address is already in use")
	}
	if len(endpoints) == 0 {
		endpoint, err := endpointRepo.CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
			ProjectID: toolset.ProjectID, McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, CustomDomainID: toolset.CustomDomainID, Slug: toolset.McpSlug.String,
		})
		if err != nil {
			return nil, oops.E(oops.CodeConflict, err, "create hosted MCP endpoint")
		}
		if err := auditLogger.LogMcpEndpointCreate(ctx, tx, audit.LogMcpEndpointCreateEvent{
			OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID, Actor: principal, ActorDisplayName: actorEmail,
			McpEndpointURN: urn.NewMcpEndpoint(endpoint.ID), Slug: endpoint.Slug,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "audit hosted MCP endpoint")
		}
		return nil, nil
	}
	existing := endpoints[0]
	wasRoot := isRoot(existing)
	keepRoot := wasRoot && existing.CustomDomainID == toolset.CustomDomainID && server.Visibility != visibility.Disabled
	rootMarker := pgtype.Bool{}
	if keepRoot {
		rootMarker = pgtype.Bool{Bool: true, Valid: true}
	}
	updated, err := endpointRepo.UpdateMCPEndpointAddress(ctx, mcpendpointsrepo.UpdateMCPEndpointAddressParams{
		ID: existing.ID, ProjectID: toolset.ProjectID, CustomDomainID: toolset.CustomDomainID,
		Slug: toolset.McpSlug.String, IsDomainRoot: rootMarker,
	})
	if err != nil {
		return nil, oops.E(oops.CodeConflict, err, "update hosted MCP endpoint address")
	}
	if err := auditLogger.LogMcpEndpointUpdate(ctx, tx, audit.LogMcpEndpointUpdateEvent{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID, Actor: principal, ActorDisplayName: actorEmail,
		McpEndpointURN: urn.NewMcpEndpoint(updated.ID), Slug: updated.Slug,
		McpEndpointSnapshotBefore: mv.BuildMcpEndpointView(existing), McpEndpointSnapshotAfter: mv.BuildMcpEndpointView(updated),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit hosted MCP endpoint address")
	}
	if !wasRoot || keepRoot {
		return nil, nil
	}
	lost := []mcpendpointsrepo.McpEndpoint{existing}
	if err := tombstone.LogRootAutoClears(ctx, tx, auditLogger, toolset.OrganizationID, principal, actorEmail, lost); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit hosted MCP root cleanup")
	}
	return lost, nil
}

func retireEndpoints(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, principal urn.Principal, actorEmail *string, toolset toolsetsrepo.Toolset, endpoints []mcpendpointsrepo.McpEndpoint) ([]mcpendpointsrepo.McpEndpoint, error) {
	if len(endpoints) == 0 {
		return nil, nil
	}
	deleted, err := mcpendpointsrepo.New(tx).SoftDeleteMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.SoftDeleteMCPEndpointsByMCPServerIDParams{McpServerID: toolset.ID, ProjectID: toolset.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "retire hosted MCP endpoints")
	}
	roots := slices.DeleteFunc(slices.Clone(endpoints), func(endpoint mcpendpointsrepo.McpEndpoint) bool {
		return !endpoint.IsDomainRoot.Valid || !endpoint.IsDomainRoot.Bool
	})
	if err := tombstone.LogRootAutoClears(ctx, tx, auditLogger, toolset.OrganizationID, principal, actorEmail, roots); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit hosted MCP root cleanup")
	}
	for _, endpoint := range deleted {
		if err := auditLogger.LogMcpEndpointDelete(ctx, tx, audit.LogMcpEndpointDeleteEvent{
			OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID, Actor: principal, ActorDisplayName: actorEmail,
			McpEndpointURN: urn.NewMcpEndpoint(endpoint.ID), Slug: endpoint.Slug,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "audit hosted MCP endpoint deletion")
		}
	}
	return roots, nil
}

// Delete tombstones a deleted toolset's canonical wrapper; returned domains need a post-commit reconcile.
func Delete(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor Actor, toolset toolsetsrepo.Toolset) ([]uuid.UUID, error) {
	if auditLogger == nil || !tombstone.ActorPresent(ctx, actor.UserID) {
		return nil, oops.E(oops.CodeUnauthorized, nil, "missing hosted MCP actor")
	}
	locked, err := tombstone.Lock(ctx, tx, toolset.OrganizationID, toolset.ProjectID, toolset.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock hosted MCP server for deletion")
	}
	if !locked.Server.ToolsetID.Valid || locked.Server.ToolsetID.UUID != toolset.ID {
		return nil, oops.E(oops.CodeConflict, nil, "hosted MCP identity belongs to another server")
	}
	if _, err := tombstone.Tombstone(ctx, tx, auditLogger, locked, tombstone.Input{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID, ActorUserID: actor.UserID, ActorEmail: actor.Email,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete hosted MCP server")
	}
	if err := auditLogger.LogMcpServerDelete(ctx, tx, audit.LogMcpServerDeleteEvent{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, actor.UserID), ActorDisplayName: actor.Email,
		McpServerURN: urn.NewMcpServer(toolset.ID), McpServerName: toolset.Name, McpServerSlug: toolset.McpSlug.String,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit hosted MCP deletion")
	}
	return tombstone.RootDomainIDs(locked.RootEndpoints), nil
}

func isRoot(endpoint mcpendpointsrepo.McpEndpoint) bool {
	return endpoint.IsDomainRoot.Valid && endpoint.IsDomainRoot.Bool
}

// resyncIssuers recomputes the derived remote_session_issuer_id for each issuer touched.
func resyncIssuers(ctx context.Context, tx pgx.Tx, toolset toolsetsrepo.Toolset, issuers ...uuid.NullUUID) error {
	ids := make([]uuid.UUID, 0, len(issuers))
	for _, id := range issuers {
		if id.Valid && !slices.Contains(ids, id.UUID) {
			ids = append(ids, id.UUID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if err := remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, tx, toolset.OrganizationID, toolset.ProjectID, ids); err != nil {
		return oops.E(oops.CodeUnexpected, err, "resync hosted MCP remote session issuer")
	}
	return nil
}
