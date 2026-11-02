package mcpendpoints

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostingstate"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tombstone"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// maxHostedSlugLength mirrors the Goa limit on a hosted MCP server's slug.
const maxHostedSlugLength = 40

// updateHostedEndpoint re-addresses one endpoint of a canonical hosted wrapper in place, so client
// references keep its identity, under the hosted address rules: a hosted server only moves onto the
// organization's verified custom domain and leaves a domain only once it can no longer serve.
func (s *Service) updateHostedEndpoint(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, preexisting repo.McpEndpoint, requestedDomainID uuid.NullUUID, slug string) (*types.McpEndpoint, error) {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	afterView, reconcileDomains, err := s.updateHostedEndpointInTx(ctx, dbtx, logger, authCtx, preexisting, requestedDomainID, slug)
	if err != nil {
		return nil, err
	}
	if err := s.requestPublicationForMCPMembership(ctx, dbtx, authCtx, []uuid.NullUUID{preexisting.McpServerID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "enqueue endpoint publication").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	s.publishForMCPMembership(ctx, authCtx, []uuid.NullUUID{preexisting.McpServerID})

	if len(reconcileDomains) > 0 {
		if err := s.reconcileCustomDomains(ctx, reconcileDomains); err != nil {
			return nil, err
		}
	}
	return afterView, nil
}

// updateHostedEndpointInTx applies a hosted endpoint update in the caller's transaction, which owns publication
// and commit. It returns the custom domains whose root it cleared, to reconcile after commit.
func (s *Service) updateHostedEndpointInTx(ctx context.Context, dbtx pgx.Tx, logger *slog.Logger, authCtx *contextvalues.AuthContext, preexisting repo.McpEndpoint, requestedDomainID uuid.NullUUID, slug string) (*types.McpEndpoint, []uuid.UUID, error) {
	serverID := preexisting.McpServerID.UUID
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, serverID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, nil, err
	}

	if err := lockEndpointMutationScope(ctx, dbtx, authCtx); err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "lock endpoint mutation scope").LogError(ctx, logger)
	}
	if err := lockHostedToolset(ctx, dbtx, *authCtx.ProjectID, serverID); err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "lock hosted toolset").LogError(ctx, logger)
	}
	dead, err := tombstone.LockCustomDomains(ctx, dbtx, authCtx.ActiveOrganizationID, uniqueIDs(preexisting.CustomDomainID, requestedDomainID))
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "lock custom domains").LogError(ctx, logger)
	}
	txRepo := repo.New(dbtx)
	existing, err := txRepo.LockMCPEndpointByID(ctx, repo.LockMCPEndpointByIDParams{ID: preexisting.ID, ProjectID: *authCtx.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").LogWarn(ctx, logger)
	case err != nil:
		return nil, nil, oops.E(oops.CodeUnexpected, err, "lock mcp endpoint").LogError(ctx, logger)
	case existing.CustomDomainID != preexisting.CustomDomainID || existing.McpServerID != preexisting.McpServerID:
		return nil, nil, oops.E(oops.CodeConflict, nil, "mcp endpoint changed concurrently; retry the request").LogWarn(ctx, logger)
	}
	server, err := mcpserversrepo.New(dbtx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: *authCtx.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogWarn(ctx, logger)
	case err != nil:
		return nil, nil, oops.E(oops.CodeUnexpected, err, "lock mcp server").LogError(ctx, logger)
	case !server.ToolsetID.Valid || server.ToolsetID.UUID != server.ID:
		return nil, nil, oops.E(oops.CodeConflict, nil, "mcp server changed concurrently; retry the request").LogWarn(ctx, logger)
	}

	customDomainID := existing.CustomDomainID
	switch {
	case requestedDomainID == existing.CustomDomainID:
	case requestedDomainID.Valid:
		if err := requireOrganizationDomain(ctx, dbtx, logger, authCtx, requestedDomainID.UUID); err != nil {
			return nil, nil, err
		}
		if existing.CustomDomainID.Valid {
			if err := requireDomainLeavable(ctx, dbtx, logger, authCtx.ActiveOrganizationID, existing.CustomDomainID.UUID, dead); err != nil {
				return nil, nil, err
			}
		}
		customDomainID = requestedDomainID
	default:
		if err := requireDomainLeavable(ctx, dbtx, logger, authCtx.ActiveOrganizationID, existing.CustomDomainID.UUID, dead); err != nil {
			return nil, nil, err
		}
		customDomainID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}

	if slug != existing.Slug && len(slug) > maxHostedSlugLength {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "a hosted MCP slug must be at most %d characters", maxHostedSlugLength).LogWarn(ctx, logger)
	}
	if slug != existing.Slug || customDomainID != existing.CustomDomainID {
		// An unchanged address keeps its slug, so an endpoint created before the prefix rule can still be saved.
		if err := validateSlugPrefix(slug, customDomainID, authCtx.OrganizationSlug); err != nil {
			return nil, nil, oops.E(oops.CodeInvalid, err, "invalid slug").LogWarn(ctx, logger)
		}
		if err := requireHostedSlugPrefix(ctx, logger, authCtx, customDomainID, slug); err != nil {
			return nil, nil, err
		}
		if err := LockSlugScope(ctx, dbtx, customDomainID, slug); err != nil {
			return nil, nil, oops.E(oops.CodeUnexpected, err, "lock mcp endpoint slug scope").LogError(ctx, logger)
		}
		// The scope is the endpoint's own domain or the organization's verified one, so a
		// soft-deleted domain must not block a move within it; the server's frozen toolset alias does not count.
		available, err := CheckSlugAvailable(ctx, dbtx, SlugAvailabilityCheck{
			Slug:                     slug,
			CustomDomainID:           customDomainID,
			OrganizationID:           authCtx.ActiveOrganizationID,
			ExcludeToolsetID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ExcludeMcpServerID:       uuid.NullUUID{UUID: server.ID, Valid: true},
			SkipDomainOwnershipCheck: true,
		})
		if err != nil {
			return nil, nil, oops.E(oops.CodeUnexpected, err, "check mcp endpoint slug availability").LogError(ctx, logger)
		}
		if !available {
			return nil, nil, oops.E(oops.CodeConflict, nil, "this slug is already taken").LogWarn(ctx, logger)
		}
	}

	if slug != existing.Slug || customDomainID != existing.CustomDomainID {
		// Endpoints left on a deleted domain cannot serve, so they do not take part in the ingress check.
		siblings, err := txRepo.ListMCPEndpointsOnLiveDomainsByMCPServerID(ctx, repo.ListMCPEndpointsOnLiveDomainsByMCPServerIDParams{
			OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, McpServerID: server.ID,
		})
		if err != nil {
			return nil, nil, oops.E(oops.CodeUnexpected, err, "list hosted MCP endpoints").LogError(ctx, logger)
		}
		addresses := []repo.McpEndpoint{proposedAddress(customDomainID, slug)}
		for _, sibling := range siblings {
			if sibling.McpEndpoint.ID != existing.ID {
				addresses = append(addresses, sibling.McpEndpoint)
			}
		}
		if err := checkHostedIngress(ctx, dbtx, logger, authCtx, server, addresses); err != nil {
			return nil, nil, err
		}
	}

	wasRoot := existing.IsDomainRoot.Valid && existing.IsDomainRoot.Bool
	keepRoot := wasRoot && existing.CustomDomainID == customDomainID && server.Visibility != mcpservers.VisibilityDisabled
	rootMarker := pgtype.Bool{Bool: false, Valid: false}
	if keepRoot {
		rootMarker = pgtype.Bool{Bool: true, Valid: true}
	}
	updated, err := txRepo.UpdateMCPEndpointAddress(ctx, repo.UpdateMCPEndpointAddressParams{
		CustomDomainID: customDomainID, Slug: slug, IsDomainRoot: rootMarker, ID: existing.ID, ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, nil, oops.E(oops.CodeConflict, err, "this slug is already taken").LogWarn(ctx, logger)
		}
		return nil, nil, oops.E(oops.CodeUnexpected, err, "update hosted mcp endpoint address").LogError(ctx, logger)
	}
	afterView := mv.BuildMcpEndpointView(updated)
	if err := s.audit.LogMcpEndpointUpdate(ctx, dbtx, audit.LogMcpEndpointUpdateEvent{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Actor:                     urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:          authCtx.Email,
		ActorSlug:                 nil,
		McpEndpointURN:            urn.NewMcpEndpoint(updated.ID),
		Slug:                      updated.Slug,
		McpEndpointSnapshotBefore: mv.BuildMcpEndpointView(existing),
		McpEndpointSnapshotAfter:  afterView,
	}); err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "log mcp endpoint update").LogError(ctx, logger)
	}
	var reconcileDomains []uuid.UUID
	if wasRoot && !keepRoot && existing.CustomDomainID.Valid {
		if err := s.logRootAutoClear(ctx, dbtx, authCtx, existing.CustomDomainID.UUID, existing.ID); err != nil {
			return nil, nil, err
		}
		reconcileDomains = append(reconcileDomains, existing.CustomDomainID.UUID)
	}

	if err := legacyalias.FollowAlias(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, server.ID); err != nil {
		return nil, nil, err //nolint:wrapcheck // oops errors pass through.
	}
	return afterView, reconcileDomains, nil
}

// requireDomainLeavable lets a hosted endpoint leave a missing, deleted or never-live domain only;
// clients may hold URLs on a live one, and a health-disabled one can come back.
func requireDomainLeavable(ctx context.Context, tx pgx.Tx, logger *slog.Logger, organizationID string, domainID uuid.UUID, dead map[uuid.UUID]bool) error {
	if dead[domainID] {
		return nil
	}
	domain, err := customdomainsrepo.New(tx).GetCustomDomainByIDAndOrganization(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{ID: domainID, OrganizationID: organizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load hosted MCP custom domain").LogError(ctx, logger)
	}
	if (domain.Activated && domain.Verified) || domain.HealthCheckedAt.Valid {
		return oops.E(oops.CodeInvalid, nil, "a hosted MCP server cannot leave its custom domain while the domain is live or can be reverified; delete the domain first").LogWarn(ctx, logger)
	}
	return nil
}

// requireEndpointCreateWrite authorizes an endpoint create: mcp:write on the server for a canonical hosted
// server, otherwise on the project. It reports whether the server was hosted when authorized.
func (s *Service) requireEndpointCreateWrite(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, mcpServerID uuid.NullUUID) (bool, error) {
	if mcpServerID.Valid {
		server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: mcpServerID.UUID, ProjectID: *authCtx.ProjectID})
		switch {
		case err == nil && isCanonicalHosted(server):
			if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, server.ID.String(), authCtx.ProjectID.String())); err != nil {
				return false, err
			}
			return true, nil
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return false, oops.E(oops.CodeUnexpected, err, "get endpoint backing server").LogError(ctx, logger)
		}
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return false, err
	}
	return false, nil
}

// isCanonicalHosted reports whether server is a toolset's canonical hosted wrapper.
func isCanonicalHosted(server mcpserversrepo.McpServer) bool {
	return server.ToolsetID.Valid && server.ToolsetID.UUID == server.ID
}

// slugPrefixRule selects whether a new hosted address must carry the organization slug prefix.
type slugPrefixRule string

const (
	// slugPrefixRequired applies the prefix rule, as every API create does.
	slugPrefixRequired slugPrefixRule = "required"

	// slugPrefixExemptLegacyAlias skips it for an address a legacy toolset alias already serves, so the backfill
	// that makes the address explicit keeps the URL clients hold.
	slugPrefixExemptLegacyAlias slugPrefixRule = "exempt_legacy_alias"
)

// checkHostedNewAddress applies the hosted address rules to an endpoint created on a canonical hosted server: the
// slug length, the organization's verified and activated custom domain only, the organization prefix on the
// platform and for free accounts unless prefixRule exempts it, and a private network mode's ingress namespace.
func checkHostedNewAddress(ctx context.Context, tx pgx.Tx, logger *slog.Logger, authCtx *contextvalues.AuthContext, server mcpserversrepo.McpServer, slug string, customDomainID uuid.NullUUID, prefixRule slugPrefixRule) error {
	if len(slug) > maxHostedSlugLength {
		return oops.E(oops.CodeBadRequest, nil, "a hosted MCP slug must be at most %d characters", maxHostedSlugLength).LogWarn(ctx, logger)
	}
	if customDomainID.Valid {
		if err := requireOrganizationDomain(ctx, tx, logger, authCtx, customDomainID.UUID); err != nil {
			return err
		}
	}
	if prefixRule != slugPrefixExemptLegacyAlias {
		if err := requireHostedSlugPrefix(ctx, logger, authCtx, customDomainID, slug); err != nil {
			return err
		}
	}
	// Endpoints left on a deleted domain cannot serve, so they do not hold back a new address.
	live, err := repo.New(tx).ListMCPEndpointsOnLiveDomainsByMCPServerID(ctx, repo.ListMCPEndpointsOnLiveDomainsByMCPServerIDParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: server.ProjectID, McpServerID: server.ID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list hosted MCP endpoints").LogError(ctx, logger)
	}
	addresses := make([]repo.McpEndpoint, 0, len(live)+1)
	for _, row := range live {
		addresses = append(addresses, row.McpEndpoint)
	}
	return checkHostedIngress(ctx, tx, logger, authCtx, server, append(addresses, proposedAddress(customDomainID, slug)))
}

// requireOrganizationDomain lets a hosted address sit only on the organization's verified, activated custom domain.
func requireOrganizationDomain(ctx context.Context, tx pgx.Tx, logger *slog.Logger, authCtx *contextvalues.AuthContext, domainID uuid.UUID) error {
	domain, err := customdomainsrepo.New(tx).GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeUnexpected, err, "load organization custom domain").LogError(ctx, logger)
	case err != nil || !domain.Activated || !domain.Verified || domain.ID != domainID:
		return oops.E(oops.CodeInvalid, err, "a hosted MCP server can only use the organization's verified custom domain").LogWarn(ctx, logger)
	}
	return nil
}

// requireHostedSlugPrefix requires the organization prefix on platform slugs and on every slug of a free account.
func requireHostedSlugPrefix(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, customDomainID uuid.NullUUID, slug string) error {
	if (!customDomainID.Valid || authCtx.AccountType == "free") && validateSlugPrefix(slug, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, authCtx.OrganizationSlug) != nil {
		return oops.E(oops.CodeBadRequest, nil, "mcp slug must be prefixed with the org slug for free accounts").LogWarn(ctx, logger)
	}
	return nil
}

// checkHostedIngress requires a live private ingress covering every endpoint of a hosted server whose network
// mode is not public_only, even while it is disabled, so a re-enable cannot bypass it.
func checkHostedIngress(ctx context.Context, tx pgx.Tx, logger *slog.Logger, authCtx *contextvalues.AuthContext, server mcpserversrepo.McpServer, endpoints []repo.McpEndpoint) error {
	mode, err := networkaccess.Effective(server.NetworkAccessMode)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "invalid stored hosted MCP network mode").LogError(ctx, logger)
	}
	if mode.IsPublicOnly() {
		return nil
	}
	if err := hostingstate.CheckPrivateIngress(ctx, tx, authCtx.ActiveOrganizationID, endpoints); err != nil {
		return err //nolint:wrapcheck // oops errors pass through.
	}
	return nil
}

// rejectLastHostedAddress keeps an enabled hosted server from losing its last endpoint that can serve. Removing an
// endpoint that cannot serve, such as one on a deleted or unverified domain, is always allowed.
func rejectLastHostedAddress(ctx context.Context, tx pgx.Tx, logger *slog.Logger, authCtx *contextvalues.AuthContext, server mcpserversrepo.McpServer, endpointID uuid.UUID) error {
	if !isCanonicalHosted(server) || server.Visibility == mcpservers.VisibilityDisabled {
		return nil
	}
	servable, err := repo.New(tx).ListServableMCPEndpointsByMCPServerID(ctx, repo.ListServableMCPEndpointsByMCPServerIDParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: server.ProjectID, McpServerID: server.ID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list hosted MCP endpoints").LogError(ctx, logger)
	}
	if !slices.ContainsFunc(servable, func(row repo.ListServableMCPEndpointsByMCPServerIDRow) bool { return row.McpEndpoint.ID == endpointID }) {
		return nil
	}
	if len(servable) > 1 {
		return nil
	}
	return oops.E(oops.CodeInvalid, nil, "disable the server before removing its last address").LogWarn(ctx, logger)
}

// proposedAddress is an endpoint address not yet written, for checks that only read its domain and slug.
func proposedAddress(customDomainID uuid.NullUUID, slug string) repo.McpEndpoint {
	unset := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	return repo.McpEndpoint{
		ID: uuid.Nil, ProjectID: uuid.Nil, CustomDomainID: customDomainID, McpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Slug: slug, IsDomainRoot: pgtype.Bool{Bool: false, Valid: false},
		CreatedAt: unset, UpdatedAt: unset, DeletedAt: unset, Deleted: false,
	}
}

// lockHostedToolset takes the toolset row lock hosting writes take before domains, endpoints and the server; id is
// a server id, which only a canonical hosted server shares with a toolset.
func lockHostedToolset(ctx context.Context, tx pgx.Tx, projectID, id uuid.UUID) error {
	if _, err := toolsetsrepo.New(tx).LockToolsetByIDAndProject(ctx, toolsetsrepo.LockToolsetByIDAndProjectParams{ID: id, ProjectID: projectID}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock toolset: %w", err)
	}
	return nil
}
