// Package hostedmcp writes a toolset's canonical wrapper (mcp_servers.id = toolset id), which owns its hosting state.
//
//nolint:exhaustruct // Hosted policy, endpoint, and audit literals omit optional fields intentionally.
package hostedmcp

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/audit"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostingstate"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tombstone"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// NoServerMessage is returned when a write names a toolset that has no canonical MCP server.
const NoServerMessage = "toolset has no hosted MCP server"

// ErrAddressInUse marks a hosted MCP write that conflicts with another server holding the address or server slug.
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

// SystemActor audits wrapper writes as the named server component; Delete takes users and agents only.
func SystemActor(component string) Actor {
	return Actor{UserID: "", Email: nil, System: component}
}

// Principal is the audit principal for the actor; false when there is no actor to attribute.
func (a Actor) Principal(ctx context.Context) (urn.Principal, bool) {
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

// Sync mirrors a locked toolset's name and tool variations group onto its existing canonical wrapper.
// Hosting state (address, visibility, issuer, network mode) belongs to the wrapper and is left alone.
func Sync(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor Actor, toolset toolsetsrepo.Toolset) error {
	if _, ok := actor.Principal(ctx); auditLogger == nil || !ok {
		return oops.E(oops.CodeUnauthorized, nil, "missing hosted MCP actor")
	}
	servers := mcpserversrepo.New(tx)
	canonical, err := servers.LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load hosted MCP server")
	case !canonical.ToolsetID.Valid || canonical.ToolsetID.UUID != toolset.ID:
		return oops.E(oops.CodeConflict, nil, "hosted MCP identity belongs to another server")
	case canonical.Name.Valid && canonical.Name.String == toolset.Name && canonical.ToolVariationsGroupID == toolset.ToolVariationsGroupID:
		return nil
	}
	updated, err := servers.SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
		Name: pgtype.Text{String: toolset.Name, Valid: true}, Slug: canonical.Slug, Visibility: canonical.Visibility,
		UserSessionIssuerID: canonical.UserSessionIssuerID, ToolVariationsGroupID: toolset.ToolVariationsGroupID,
		NetworkAccessMode: canonical.NetworkAccessMode, ID: toolset.ID, ProjectID: toolset.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "update hosted MCP server")
	}
	if err := LogUpdate(ctx, tx, auditLogger, actor, toolset.OrganizationID, canonical, updated); err != nil {
		return err
	}
	return SyncDisplayName(ctx, tx, canonical, updated)
}

// LogUpdate audits a canonical wrapper write with before and after snapshots.
func LogUpdate(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor Actor, organizationID string, before, after mcpserversrepo.McpServer) error {
	principal, ok := actor.Principal(ctx)
	if auditLogger == nil || !ok {
		return oops.E(oops.CodeUnauthorized, nil, "missing hosted MCP actor")
	}
	if err := auditLogger.LogMcpServerUpdate(ctx, tx, audit.LogMcpServerUpdateEvent{
		OrganizationID: organizationID, ProjectID: after.ProjectID, Actor: principal, ActorDisplayName: actor.Email,
		McpServerURN: urn.NewMcpServer(after.ID), McpServerName: after.Name.String, McpServerSlug: after.Slug.String,
		McpServerSnapshotBefore: mv.BuildMcpServerView(before), McpServerSnapshotAfter: mv.BuildMcpServerView(after),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit hosted MCP update")
	}
	return nil
}

// SyncDisplayName carries a wrapper rename onto the plugin entries still showing its old default name.
func SyncDisplayName(ctx context.Context, tx pgx.Tx, before, after mcpserversrepo.McpServer) error {
	oldName, newName := displayName(before), displayName(after)
	if oldName == newName {
		return nil
	}
	if _, err := pluginsrepo.New(tx).SyncMcpServerDisplayName(ctx, pluginsrepo.SyncMcpServerDisplayNameParams{
		NewDisplayName: newName, ProjectID: after.ProjectID, McpServerID: uuid.NullUUID{UUID: after.ID, Valid: true}, OldDisplayName: oldName,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "sync hosted MCP plugin display name")
	}
	return nil
}

// CreateCanonicalInput is a new toolset's hosted MCP server: its canonical wrapper and one platform endpoint.
type CreateCanonicalInput struct {
	// Toolset is the newly created toolset; the wrapper takes its id, name and tool variations group.
	Toolset toolsetsrepo.Toolset

	// Slug is the platform endpoint slug, already checked against the unified slug namespace under its scope lock.
	Slug string

	// Visibility is the wrapper's initial disabled, private or public state.
	Visibility string
}

// CreateCanonical writes a new toolset's canonical wrapper and its platform endpoint, audited as the actor.
func CreateCanonical(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor Actor, input CreateCanonicalInput) (mcpserversrepo.McpServer, error) {
	principal, ok := actor.Principal(ctx)
	if auditLogger == nil || !ok {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnauthorized, nil, "missing hosted MCP actor")
	}
	toolset := input.Toolset
	server, err := mcpserversrepo.New(tx).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: toolset.ProjectID, Name: pgtype.Text{String: toolset.Name, Valid: true}, Slug: pgtype.Text{String: input.Slug, Valid: true},
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ToolVariationsGroupID: toolset.ToolVariationsGroupID,
		Visibility: input.Visibility, NetworkAccessMode: networkaccess.Storage(networkaccess.ModePublicOnly),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return mcpserversrepo.McpServer{}, createServerError(err)
		}
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "create hosted MCP server")
	}
	if err := auditLogger.LogMcpServerCreate(ctx, tx, audit.LogMcpServerCreateEvent{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID, Actor: principal, ActorDisplayName: actor.Email,
		McpServerURN: urn.NewMcpServer(toolset.ID), McpServerName: toolset.Name, McpServerSlug: input.Slug,
	}); err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "audit hosted MCP creation")
	}
	endpoint, err := mcpendpointsrepo.New(tx).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: toolset.ProjectID, McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Slug: input.Slug,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeConflict, errors.Join(ErrAddressInUse, err), "hosted MCP address is already in use")
		}
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "create hosted MCP endpoint")
	}
	if err := auditLogger.LogMcpEndpointCreate(ctx, tx, audit.LogMcpEndpointCreateEvent{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID, Actor: principal, ActorDisplayName: actor.Email,
		McpEndpointURN: urn.NewMcpEndpoint(endpoint.ID), Slug: endpoint.Slug,
	}); err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "audit hosted MCP endpoint")
	}
	return server, nil
}

// LockServer locks a hosted server for a hosting write in the order deletion and root selection take: the
// organization's custom domain, the server's endpoint domains, its endpoints, then the server. The endpoints are
// read under the server lock. A missing server returns pgx.ErrNoRows.
func LockServer(ctx context.Context, tx pgx.Tx, organizationID string, projectID, serverID uuid.UUID) ([]mcpendpointsrepo.McpEndpoint, mcpserversrepo.McpServer, error) {
	if _, err := customdomainsrepo.New(tx).LockCustomDomainByOrganization(ctx, organizationID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, mcpserversrepo.McpServer{}, fmt.Errorf("lock organization custom domain: %w", err)
	}
	endpointRepo := mcpendpointsrepo.New(tx)
	domainIDs, err := endpointRepo.ListCustomDomainIDsByMCPServerID(ctx, mcpendpointsrepo.ListCustomDomainIDsByMCPServerIDParams{McpServerID: serverID, ProjectID: projectID})
	if err != nil {
		return nil, mcpserversrepo.McpServer{}, fmt.Errorf("list hosted MCP domains: %w", err)
	}
	if _, err := tombstone.LockCustomDomains(ctx, tx, organizationID, domainIDs); err != nil {
		return nil, mcpserversrepo.McpServer{}, fmt.Errorf("lock hosted MCP domains: %w", err)
	}
	if _, err := endpointRepo.LockMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.LockMCPEndpointsByMCPServerIDParams{McpServerID: serverID, ProjectID: projectID}); err != nil {
		return nil, mcpserversrepo.McpServer{}, fmt.Errorf("lock hosted MCP endpoints: %w", err)
	}
	server, err := mcpserversrepo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	if err != nil {
		return nil, mcpserversrepo.McpServer{}, fmt.Errorf("lock hosted MCP server: %w", err)
	}
	// Endpoint creates and deletes lock the server row, so a read under that lock sees every endpoint that can serve.
	// Endpoints left on a deleted domain cannot serve and are left out, so they never hold back the ingress check.
	rows, err := endpointRepo.ListMCPEndpointsOnLiveDomainsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsOnLiveDomainsByMCPServerIDParams{
		OrganizationID: organizationID, ProjectID: projectID, McpServerID: serverID,
	})
	if err != nil {
		return nil, mcpserversrepo.McpServer{}, fmt.Errorf("read hosted MCP endpoints: %w", err)
	}
	endpoints := make([]mcpendpointsrepo.McpEndpoint, 0, len(rows))
	for _, row := range rows {
		endpoints = append(endpoints, row.McpEndpoint)
	}
	return endpoints, server, nil
}

// RequireLiveAddress rejects enabling a hosted server with no endpoint that can serve: one on the platform or on
// a live, verified and activated custom domain.
func RequireLiveAddress(ctx context.Context, tx pgx.Tx, organizationID string, server mcpserversrepo.McpServer) error {
	live, err := mcpendpointsrepo.New(tx).ListServableMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListServableMCPEndpointsByMCPServerIDParams{
		OrganizationID: organizationID, ProjectID: server.ProjectID, McpServerID: server.ID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list hosted MCP addresses")
	}
	if len(live) == 0 {
		return oops.E(oops.CodeInvalid, nil, "hosted MCP server has no address; add one before enabling it")
	}
	return nil
}

// Enable re-enables a toolset's disabled canonical wrapper with hostingstate.ReEnableVisibility: public only with
// external OAuth or an OAuth proxy, else private. A private network mode needs a live ingress for the server's
// endpoints. It reports false when there is no disabled wrapper to enable.
func Enable(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor Actor, toolset toolsetsrepo.Toolset) (bool, error) {
	endpoints, canonical, err := LockServer(ctx, tx, toolset.OrganizationID, toolset.ProjectID, toolset.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, oops.E(oops.CodeUnexpected, err, "lock hosted MCP server")
	case !IsCanonical(&canonical, toolset.ID) || canonical.Visibility != visibility.Disabled:
		return false, nil
	}
	if err := RequireLiveAddress(ctx, tx, toolset.OrganizationID, canonical); err != nil {
		return false, err
	}
	target := hostingstate.ReEnableVisibility(toolset)
	mode, err := networkaccess.Effective(canonical.NetworkAccessMode)
	if err != nil {
		return false, oops.E(oops.CodeInvalid, err, "hosted MCP server has an invalid network access mode")
	}
	if !mode.IsPublicOnly() {
		if err := hostingstate.CheckPrivateIngress(ctx, tx, toolset.OrganizationID, endpoints); err != nil {
			return false, err //nolint:wrapcheck // oops errors pass through.
		}
	}
	updated, err := mcpserversrepo.New(tx).SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
		Name: canonical.Name, Slug: canonical.Slug, Visibility: target,
		UserSessionIssuerID: canonical.UserSessionIssuerID, ToolVariationsGroupID: canonical.ToolVariationsGroupID,
		NetworkAccessMode: canonical.NetworkAccessMode, ID: canonical.ID, ProjectID: canonical.ProjectID,
	})
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "enable hosted MCP server")
	}
	if err := LogUpdate(ctx, tx, auditLogger, actor, toolset.OrganizationID, canonical, updated); err != nil {
		return false, err
	}
	return true, nil
}

// DeleteResult lists the post-commit work a hosted MCP deletion leaves.
type DeleteResult struct {
	// RootDomainIDs are custom domains whose root was cleared and need a reconcile.
	RootDomainIDs []uuid.UUID

	// DeletedRiskPolicies are lifecycle-bound policies whose results need cleanup.
	DeletedRiskPolicies []uuid.UUID
}

// Delete tombstones a deleted toolset's canonical wrapper.
func Delete(ctx context.Context, tx pgx.Tx, tracerProvider trace.TracerProvider, auditLogger *audit.Logger, actor Actor, toolset toolsetsrepo.Toolset) (DeleteResult, error) {
	if auditLogger == nil || !tombstone.ActorPresent(ctx, actor.UserID) {
		return DeleteResult{}, oops.E(oops.CodeUnauthorized, nil, "missing hosted MCP actor")
	}
	locked, err := tombstone.Lock(ctx, tx, toolset.OrganizationID, toolset.ProjectID, toolset.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeleteResult{}, nil
	}
	if err != nil {
		return DeleteResult{}, oops.E(oops.CodeUnexpected, err, "lock hosted MCP server for deletion")
	}
	// A server that kept the toolset's id but moved to a remote MCP backend no
	// longer belongs to the toolset, so the toolset has nothing to tombstone.
	if !locked.Server.ToolsetID.Valid && locked.Server.RemoteMcpServerID.Valid {
		return DeleteResult{}, nil
	}
	if !locked.Server.ToolsetID.Valid || locked.Server.ToolsetID.UUID != toolset.ID {
		return DeleteResult{}, oops.E(oops.CodeConflict, nil, "hosted MCP identity belongs to another server")
	}
	tombstoned, err := tombstone.Tombstone(ctx, tx, auditLogger, locked, tombstone.Input{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID, ActorUserID: actor.UserID, ActorEmail: actor.Email,
		ActorSystem: "", TracerProvider: tracerProvider,
	})
	if err != nil {
		return DeleteResult{}, oops.E(oops.CodeUnexpected, err, "delete hosted MCP server")
	}
	if err := auditLogger.LogMcpServerDelete(ctx, tx, audit.LogMcpServerDeleteEvent{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, actor.UserID), ActorDisplayName: actor.Email,
		McpServerURN: urn.NewMcpServer(toolset.ID), McpServerName: toolset.Name, McpServerSlug: locked.Server.Slug.String,
	}); err != nil {
		return DeleteResult{}, oops.E(oops.CodeUnexpected, err, "audit hosted MCP deletion")
	}
	return DeleteResult{
		RootDomainIDs:       tombstone.RootDomainIDs(locked.RootEndpoints),
		DeletedRiskPolicies: tombstoned.DeletedRiskPolicies,
	}, nil
}

// displayName mirrors mcpservers.ServerDisplayName, the default plugin entry name.
func displayName(server mcpserversrepo.McpServer) string {
	switch {
	case server.Name.Valid && server.Name.String != "":
		return server.Name.String
	case server.Slug.Valid && server.Slug.String != "":
		return server.Slug.String
	default:
		return server.ID.String()
	}
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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}
