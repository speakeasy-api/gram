//nolint:exhaustruct // Hosted policy, endpoint, and audit literals omit optional fields intentionally.
package hostedmcp

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// BackfillSync projects a locked toolset's hosting columns onto its canonical wrapper and single endpoint, creating
// them when missing. It is a pinned copy of the toolset-to-server Sync that pods from before this change run, kept
// as the oracle tests use to prove that sync writes nothing on mirrored rows; do not edit it. Only the hosted MCP
// backfill tool and fixtures for legacy rows may call it: the wrapper owns hosting state everywhere else.
func BackfillSync(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor Actor, toolset toolsetsrepo.Toolset) ([]uuid.UUID, error) {
	principal, ok := actor.Principal(ctx)
	if auditLogger == nil || !ok {
		return nil, oops.E(oops.CodeUnauthorized, nil, "missing hosted MCP actor")
	}
	hasSlug := toolset.McpSlug.Valid && toolset.McpSlug.String != ""
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
		if held {
			return nil, nil
		}
	}

	mode := networkaccess.ModePublicOnly
	if !created {
		mode, err = networkaccess.Effective(canonical.NetworkAccessMode)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "invalid stored hosted MCP network mode")
		}
	}
	wantVisibility := backfillVisibility(toolset)
	// A deleted domain took its endpoints with it; there is no address to restore.
	addressed := hasSlug && (!toolset.CustomDomainID.Valid || !dead[toolset.CustomDomainID.UUID])
	addressChanged := !created && addressed && (len(endpoints) != 1 || endpoints[0].Slug != toolset.McpSlug.String || endpoints[0].CustomDomainID != toolset.CustomDomainID)
	// Private access needs a live ingress only when newly selected or moved.
	if mode != networkaccess.ModePublicOnly && toolset.McpEnabled && (addressChanged || canonical.Visibility == visibility.Disabled) {
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
			return nil, createServerError(err)
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
		canonical.UserSessionIssuerID != toolset.UserSessionIssuerID || canonical.ToolVariationsGroupID != toolset.ToolVariationsGroupID:
		previous := canonical
		canonical, err = servers.SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
			Name: pgtype.Text{String: toolset.Name, Valid: true}, Slug: toolset.McpSlug, Visibility: wantVisibility,
			UserSessionIssuerID: toolset.UserSessionIssuerID, ToolVariationsGroupID: toolset.ToolVariationsGroupID,
			NetworkAccessMode: canonical.NetworkAccessMode, ID: toolset.ID, ProjectID: toolset.ProjectID,
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
		if oldName := displayName(previous); oldName != toolset.Name {
			if _, err := pluginsrepo.New(tx).SyncMcpServerDisplayName(ctx, pluginsrepo.SyncMcpServerDisplayNameParams{
				NewDisplayName: toolset.Name, ProjectID: toolset.ProjectID, McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, OldDisplayName: oldName,
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "sync hosted MCP plugin display name")
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

// backfillVisibility maps a toolset's (mcp_enabled, mcp_is_public) onto mcp_servers.visibility.
func backfillVisibility(toolset toolsetsrepo.Toolset) string {
	switch {
	case !toolset.McpEnabled:
		return visibility.Disabled
	case toolset.McpIsPublic:
		return visibility.Public
	default:
		return visibility.Private
	}
}

// createServerError classifies a racing slug claim without swallowing the failed write.
// The caller must still roll back the transaction; retain the database cause for diagnostics.
func createServerError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "mcp_servers_project_id_slug_key" {
		return oops.E(oops.CodeConflict, errors.Join(ErrAddressInUse, err), "hosted MCP project server slug is already in use")
	}
	return oops.E(oops.CodeConflict, err, "create hosted MCP server")
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
