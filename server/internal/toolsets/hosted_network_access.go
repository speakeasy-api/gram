//nolint:exhaustruct // Hosted policy, endpoint, and audit literals omit optional fields intentionally.
package toolsets

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsRepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	networkingressRepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// SetHostedNetworkAccessInTransaction changes only the canonical hosted policy
// within a caller-owned transaction. The caller must authorize the target and
// hold the organization's ingress and project admission locks first.
func SetHostedNetworkAccessInTransaction(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor *contextvalues.AuthContext, toolsetID uuid.UUID, mode networkaccess.Mode) error {
	if actor == nil || actor.ProjectID == nil || auditLogger == nil {
		return oops.E(oops.CodeUnauthorized, nil, "missing hosted MCP actor")
	}
	toolsets := repo.New(tx)
	candidate, err := toolsets.GetToolsetByIDAndProject(ctx, repo.GetToolsetByIDAndProjectParams{ID: toolsetID, ProjectID: *actor.ProjectID})
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "hosted toolset not found")
	}
	if candidate.OrganizationID != actor.ActiveOrganizationID {
		return oops.E(oops.CodeNotFound, nil, "hosted toolset not found")
	}
	locked, err := toolsets.GetToolsetForUpdate(ctx, repo.GetToolsetForUpdateParams{Slug: candidate.Slug, ProjectID: *actor.ProjectID})
	if err != nil || locked.ID != toolsetID || locked.OrganizationID != actor.ActiveOrganizationID {
		return oops.E(oops.CodeConflict, err, "hosted toolset changed concurrently")
	}
	return (&Service{audit: auditLogger}).reconcileHostedNetworkAccess(ctx, tx, actor, locked, &mode)
}

// A hosted wrapper is identified by its toolset ID, unlike independently
// managed toolset-backed MCP servers (for example gateway members).
func (s *Service) reconcileHostedNetworkAccess(ctx context.Context, tx pgx.Tx, actor *contextvalues.AuthContext, after repo.Toolset, requested *networkaccess.Mode) error {
	servers := mcpserversRepo.New(tx)
	canonical, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversRepo.GetMCPServerByIDAndProjectIDParams{ID: after.ID, ProjectID: after.ProjectID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeUnexpected, err, "load hosted MCP server")
	}
	created := errors.Is(err, pgx.ErrNoRows)
	if created && requested == nil {
		return nil
	}
	if !created && (!canonical.ToolsetID.Valid || canonical.ToolsetID.UUID != after.ID) {
		return oops.E(oops.CodeConflict, nil, "hosted MCP identity belongs to another server")
	}
	if !after.McpSlug.Valid || after.McpSlug.String == "" {
		return oops.E(oops.CodeInvalid, nil, "hosted MCP needs a slug before network access can be configured")
	}

	mode := networkaccess.ModePublicOnly
	if !created {
		mode, err = networkaccess.Effective(canonical.NetworkAccessMode)
		if err != nil && requested == nil {
			return oops.E(oops.CodeUnexpected, err, "invalid stored hosted MCP network mode")
		}
	}
	storedMode := mode
	if requested != nil {
		mode = *requested
	}
	// Existing private policy remains valid during an ingress outage. Require a
	// live ingress only when newly selecting private access or moving its address.
	endpointRepo := mcpendpointsRepo.New(tx)
	var endpoints []mcpendpointsRepo.McpEndpoint
	if !created {
		endpoints, err = endpointRepo.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsRepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: after.ProjectID, McpServerID: after.ID})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "list hosted MCP endpoints")
		}
	}
	addressChanged := !created && (len(endpoints) != 1 || endpoints[0].Slug != after.McpSlug.String || endpoints[0].CustomDomainID != after.CustomDomainID)
	if mode != networkaccess.ModePublicOnly && after.McpEnabled &&
		((requested != nil && (created || mode != storedMode)) || addressChanged || canonical.Visibility == "disabled") {
		ingress, ingressErr := networkingressRepo.New(tx).GetNetworkIngressByOrganization(ctx, after.OrganizationID)
		if ingressErr != nil || !ingress.Enabled || ingress.Deleted || ingress.Status != "online" || !ingress.DnsName.Valid {
			return oops.E(oops.CodeInvalid, ingressErr, "online private network ingress is required")
		}
		if (ingress.EndpointNamespaceKind == "platform" && after.CustomDomainID.Valid) ||
			(ingress.EndpointNamespaceKind == "custom_domain" && (!after.CustomDomainID.Valid || after.CustomDomainID != ingress.CustomDomainID)) {
			return oops.E(oops.CodeInvalid, nil, "hosted MCP address does not match the private ingress namespace")
		}
		if ingress.EndpointNamespaceKind != "platform" && ingress.EndpointNamespaceKind != "custom_domain" {
			return oops.E(oops.CodeInvalid, nil, "private ingress namespace is invalid")
		}
	}
	if created {
		canonical, err = servers.CreateMCPServer(ctx, mcpserversRepo.CreateMCPServerParams{
			ID: after.ID, ProjectID: after.ProjectID, Name: pgtype.Text{String: after.Name, Valid: true}, Slug: after.McpSlug,
			ToolsetID: uuid.NullUUID{UUID: after.ID, Valid: true}, UserSessionIssuerID: after.UserSessionIssuerID,
			Visibility: hostedVisibility(after), NetworkAccessMode: networkaccess.Storage(mode),
		})
		if err != nil {
			return oops.E(oops.CodeConflict, err, "create hosted MCP network policy")
		}
		if err := s.audit.LogMcpServerCreate(ctx, tx, audit.LogMcpServerCreateEvent{
			OrganizationID: after.OrganizationID, ProjectID: after.ProjectID,
			Actor: urn.NewPrincipal(urn.PrincipalTypeUser, actor.UserID), ActorDisplayName: actor.Email,
			McpServerURN: urn.NewMcpServer(after.ID), McpServerName: after.Name, McpServerSlug: after.McpSlug.String,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "audit hosted MCP creation")
		}
	} else {
		previous := canonical
		updated, updateErr := tx.Exec(ctx, `UPDATE mcp_servers SET name = $1, slug = $2, visibility = $3, user_session_issuer_id = $4, network_access_mode = $5, updated_at = clock_timestamp() WHERE id = $6 AND project_id = $7 AND toolset_id = $6 AND deleted IS FALSE`, after.Name, after.McpSlug, hostedVisibility(after), after.UserSessionIssuerID, networkaccess.Storage(mode), after.ID, after.ProjectID)
		if updateErr != nil || updated.RowsAffected() != 1 {
			return oops.E(oops.CodeUnexpected, updateErr, "update hosted MCP network policy")
		}
		canonical, err = servers.GetMCPServerByIDAndProjectID(ctx, mcpserversRepo.GetMCPServerByIDAndProjectIDParams{ID: after.ID, ProjectID: after.ProjectID})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "reload hosted MCP network policy")
		}
		if err := s.audit.LogMcpServerUpdate(ctx, tx, audit.LogMcpServerUpdateEvent{
			OrganizationID: after.OrganizationID, ProjectID: after.ProjectID,
			Actor: urn.NewPrincipal(urn.PrincipalTypeUser, actor.UserID), ActorDisplayName: actor.Email,
			McpServerURN: urn.NewMcpServer(after.ID), McpServerName: after.Name, McpServerSlug: after.McpSlug.String,
			McpServerSnapshotBefore: mv.BuildMcpServerView(previous), McpServerSnapshotAfter: mv.BuildMcpServerView(canonical),
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "audit hosted MCP network policy")
		}
	}

	if created {
		endpoints, err = endpointRepo.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsRepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: after.ProjectID, McpServerID: after.ID})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "list hosted MCP endpoints")
		}
	}
	// Slug/domain updates keep the same endpoint identity for client references.
	if len(endpoints) > 1 {
		return oops.E(oops.CodeConflict, nil, "hosted MCP has multiple endpoints; resolve them before editing network access")
	}
	if len(endpoints) == 1 && endpoints[0].Slug == after.McpSlug.String && endpoints[0].CustomDomainID == after.CustomDomainID {
		return nil
	}
	if err := mcpendpoints.LockSlugScope(ctx, tx, after.CustomDomainID, after.McpSlug.String); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock hosted MCP address")
	}
	available, err := mcpendpoints.CheckSlugAvailable(ctx, tx, mcpendpoints.SlugAvailabilityCheck{
		Slug: after.McpSlug.String, CustomDomainID: after.CustomDomainID, OrganizationID: after.OrganizationID,
		ExcludeToolsetID: uuid.NullUUID{UUID: after.ID, Valid: true}, ExcludeMcpServerID: uuid.NullUUID{UUID: after.ID, Valid: true},
		SkipDomainOwnershipCheck: true, // The toolset owns this address scope, including soft-deleted domains.
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check hosted MCP address")
	}
	if !available {
		return oops.E(oops.CodeConflict, nil, "hosted MCP address is already in use")
	}
	if len(endpoints) == 0 {
		endpoint, createErr := endpointRepo.CreateMCPEndpoint(ctx, mcpendpointsRepo.CreateMCPEndpointParams{
			ProjectID: after.ProjectID, McpServerID: uuid.NullUUID{UUID: after.ID, Valid: true}, CustomDomainID: after.CustomDomainID, Slug: after.McpSlug.String,
		})
		if createErr != nil {
			return oops.E(oops.CodeConflict, createErr, "create hosted MCP endpoint")
		}
		if err := s.audit.LogMcpEndpointCreate(ctx, tx, audit.LogMcpEndpointCreateEvent{
			OrganizationID: after.OrganizationID, ProjectID: after.ProjectID,
			Actor: urn.NewPrincipal(urn.PrincipalTypeUser, actor.UserID), ActorDisplayName: actor.Email,
			McpEndpointURN: urn.NewMcpEndpoint(endpoint.ID), Slug: endpoint.Slug,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "audit hosted MCP endpoint")
		}
		return nil
	}
	rootMarker := pgtype.Bool{}
	if after.CustomDomainID == endpoints[0].CustomDomainID {
		rootMarker = endpoints[0].IsDomainRoot
	}
	updatedEndpoint, err := endpointRepo.UpdateMCPEndpointAddress(ctx, mcpendpointsRepo.UpdateMCPEndpointAddressParams{
		ID: endpoints[0].ID, ProjectID: after.ProjectID, CustomDomainID: after.CustomDomainID,
		Slug: after.McpSlug.String, IsDomainRoot: rootMarker,
	})
	if err != nil {
		return oops.E(oops.CodeConflict, err, "update hosted MCP endpoint address")
	}
	if err := s.audit.LogMcpEndpointUpdate(ctx, tx, audit.LogMcpEndpointUpdateEvent{
		OrganizationID: after.OrganizationID, ProjectID: after.ProjectID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, actor.UserID), ActorDisplayName: actor.Email,
		McpEndpointURN: urn.NewMcpEndpoint(updatedEndpoint.ID), Slug: updatedEndpoint.Slug,
		McpEndpointSnapshotBefore: mv.BuildMcpEndpointView(endpoints[0]), McpEndpointSnapshotAfter: mv.BuildMcpEndpointView(updatedEndpoint),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit hosted MCP endpoint address")
	}
	return nil
}

func (s *Service) deleteHostedNetworkAccess(ctx context.Context, tx pgx.Tx, actor *contextvalues.AuthContext, toolset repo.Toolset) error {
	servers := mcpserversRepo.New(tx)
	server, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversRepo.GetMCPServerByIDAndProjectIDParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load hosted MCP policy for deletion")
	}
	if !server.ToolsetID.Valid || server.ToolsetID.UUID != toolset.ID {
		return oops.E(oops.CodeConflict, nil, "hosted MCP identity belongs to another server")
	}
	endpoints, err := mcpendpointsRepo.New(tx).SoftDeleteMCPEndpointsByMCPServerID(ctx, mcpendpointsRepo.SoftDeleteMCPEndpointsByMCPServerIDParams{
		McpServerID: server.ID, ProjectID: toolset.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete hosted MCP endpoints")
	}
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, actor.UserID)
	for _, endpoint := range endpoints {
		if err := s.audit.LogMcpEndpointDelete(ctx, tx, audit.LogMcpEndpointDeleteEvent{
			OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID, Actor: principal, ActorDisplayName: actor.Email,
			McpEndpointURN: urn.NewMcpEndpoint(endpoint.ID), Slug: endpoint.Slug,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "audit hosted MCP endpoint deletion")
		}
	}
	if _, err := servers.DeleteMCPServer(ctx, mcpserversRepo.DeleteMCPServerParams{ID: server.ID, ProjectID: toolset.ProjectID}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete hosted MCP policy")
	}
	if err := s.audit.LogMcpServerDelete(ctx, tx, audit.LogMcpServerDeleteEvent{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID, Actor: principal, ActorDisplayName: actor.Email,
		McpServerURN: urn.NewMcpServer(server.ID), McpServerName: toolset.Name, McpServerSlug: toolset.McpSlug.String,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit hosted MCP policy deletion")
	}
	return nil
}

func hostedVisibility(toolset repo.Toolset) string {
	if !toolset.McpEnabled {
		return "disabled"
	}
	if toolset.McpIsPublic {
		return "public"
	}
	return "private"
}
