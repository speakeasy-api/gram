//nolint:exhaustruct // Endpoint commands set only applicable optional fields.
package mcpendpoints

import (
	"context"
	"errors"
	"fmt"

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
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// CreateMcpEndpointInTransactionInput describes an endpoint created within a
// caller-owned transaction. It does not attach generic backends to the Default
// plugin or enqueue publication work.
type CreateMcpEndpointInTransactionInput struct {
	AuthContext     *contextvalues.AuthContext
	CustomDomainID  uuid.NullUUID
	McpServerID     uuid.NullUUID
	MetaMcpServerID uuid.NullUUID
	Slug            string
}

// CreateMcpEndpointInTransaction creates an endpoint using the caller's
// transaction. The caller owns commit; default-plugin attachment and
// publication are intentionally outside this transaction seam.
func (s *Service) CreateMcpEndpointInTransaction(ctx context.Context, tx pgx.Tx, input CreateMcpEndpointInTransactionInput) (*types.McpEndpoint, error) {
	authCtx := input.AuthContext
	if tx == nil || authCtx == nil || authCtx.ProjectID == nil || authCtx.ActiveOrganizationID == "" || authCtx.OrganizationSlug == "" || input.Slug == "" || input.McpServerID.Valid == input.MetaMcpServerID.Valid {
		return nil, fmt.Errorf("invalid MCP endpoint create input")
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := validateSlugPrefix(input.Slug, input.CustomDomainID, authCtx.OrganizationSlug); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid slug")
	}
	if err := lockEndpointMutationScope(ctx, tx, authCtx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock endpoint mutation scope")
	}
	if err := lockCustomDomainsForOrganization(ctx, tx, authCtx.ActiveOrganizationID, uniqueIDs(input.CustomDomainID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeInvalid, err, "custom_domain_id does not reference a live custom domain")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock custom domain")
	}
	if input.McpServerID.Valid {
		servers, err := mcpserversrepo.New(tx).LockMCPServersByIDs(ctx, mcpserversrepo.LockMCPServersByIDsParams{ProjectID: *authCtx.ProjectID, Ids: []uuid.UUID{input.McpServerID.UUID}})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "lock mcp server")
		}
		if len(servers) != 1 {
			return nil, oops.E(oops.CodeInvalid, nil, "mcp_server_id does not reference a resource in this project")
		}
	}
	if err := s.lockMetaMcpServers(ctx, tx, authCtx, uniqueIDs(input.MetaMcpServerID), input.MetaMcpServerID); err != nil {
		return nil, err
	}
	if err := verifyEndpointReferenceOwnership(ctx, tx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, input.McpServerID, input.MetaMcpServerID, input.CustomDomainID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp endpoint")
	}
	if err := LockSlugScope(ctx, tx, input.CustomDomainID, input.Slug); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock mcp endpoint slug scope")
	}
	available, err := CheckSlugAvailable(ctx, tx, SlugAvailabilityCheck{
		Slug: input.Slug, CustomDomainID: input.CustomDomainID, OrganizationID: authCtx.ActiveOrganizationID,
		ExcludeToolsetID: uuid.NullUUID{}, ExcludeMcpServerID: input.McpServerID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check mcp endpoint slug availability")
	}
	if !available {
		return nil, oops.E(oops.CodeConflict, nil, "mcp endpoint slug already exists for this domain")
	}
	created, err := repo.New(tx).CreateMCPEndpoint(ctx, repo.CreateMCPEndpointParams{
		ProjectID: *authCtx.ProjectID, CustomDomainID: input.CustomDomainID, McpServerID: input.McpServerID,
		MetaMcpServerID: input.MetaMcpServerID, Slug: input.Slug,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "mcp endpoint slug already exists for this domain")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create mcp endpoint")
	}
	if err := s.audit.LogMcpEndpointCreate(ctx, tx, audit.LogMcpEndpointCreateEvent{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email,
		ActorSlug: nil, McpEndpointURN: urn.NewMcpEndpoint(created.ID), Slug: created.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp endpoint creation")
	}
	return mv.BuildMcpEndpointView(created), nil
}

func lockCustomDomainsForOrganization(ctx context.Context, tx pgx.Tx, organizationID string, domainIDs []uuid.UUID) error {
	queries := customdomainsrepo.New(tx)
	for _, domainID := range domainIDs {
		if _, err := queries.LockCustomDomainByIDAndOrganization(ctx, customdomainsrepo.LockCustomDomainByIDAndOrganizationParams{ID: domainID, OrganizationID: organizationID}); err != nil {
			return fmt.Errorf("lock custom domain %s: %w", domainID, err)
		}
	}
	return nil
}

// UpdateMcpEndpointAddressInput describes an address-only update to an existing
// endpoint. Backend references are inherited from the endpoint and cannot be
// replaced by this command.
type UpdateMcpEndpointAddressInput struct {
	AuthContext    *contextvalues.AuthContext
	EndpointID     uuid.UUID
	CustomDomainID uuid.NullUUID
	Slug           string
}

// UpdateMcpEndpointAddressInTransaction updates an endpoint's address using the
// caller's transaction. The caller owns commit and must reconcile returned
// custom-domain IDs after commit.
func (s *Service) UpdateMcpEndpointAddressInTransaction(ctx context.Context, tx pgx.Tx, input UpdateMcpEndpointAddressInput) (*types.McpEndpoint, []uuid.UUID, error) {
	authCtx := input.AuthContext
	if tx == nil || authCtx == nil || authCtx.ProjectID == nil || authCtx.ActiveOrganizationID == "" || authCtx.OrganizationSlug == "" || input.EndpointID == uuid.Nil || input.Slug == "" {
		return nil, nil, fmt.Errorf("invalid MCP endpoint address update input")
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, nil, err
	}
	if err := validateSlugPrefix(input.Slug, input.CustomDomainID, authCtx.OrganizationSlug); err != nil {
		return nil, nil, oops.E(oops.CodeInvalid, err, "invalid slug")
	}

	// Keep the shared writer order: ingress lifecycle, project admission,
	// domains, endpoint, then its backing server. The ingress lock serializes
	// this mutation with ingress state changes before any row locks are taken.
	if err := networkingressrepo.New(tx).AcquireNetworkIngressOrganizationLock(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "lock network ingress lifecycle")
	}
	if err := admission.LockProject(ctx, tx, *authCtx.ProjectID); err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "lock distribution admission")
	}

	preexisting, err := repo.New(tx).GetMCPEndpointByID(ctx, repo.GetMCPEndpointByIDParams{ID: input.EndpointID, ProjectID: *authCtx.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found")
	}
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "get mcp endpoint")
	}
	if err := lockCustomDomainsForOrganization(ctx, tx, authCtx.ActiveOrganizationID, uniqueIDs(preexisting.CustomDomainID, input.CustomDomainID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, oops.E(oops.CodeInvalid, err, "custom_domain_id does not reference a live custom domain")
		}
		return nil, nil, oops.E(oops.CodeUnexpected, err, "lock custom domains")
	}

	txRepo := repo.New(tx)
	existing, err := txRepo.LockMCPEndpointByID(ctx, repo.LockMCPEndpointByIDParams{ID: input.EndpointID, ProjectID: *authCtx.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found")
	}
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "lock mcp endpoint")
	}
	if existing.CustomDomainID != preexisting.CustomDomainID {
		return nil, nil, oops.E(oops.CodeConflict, nil, "mcp endpoint changed concurrently; retry the request")
	}

	var targetServer *mcpserversrepo.McpServer
	if existing.McpServerID.Valid {
		servers, err := mcpserversrepo.New(tx).LockMCPServersByIDs(ctx, mcpserversrepo.LockMCPServersByIDsParams{ProjectID: *authCtx.ProjectID, Ids: []uuid.UUID{existing.McpServerID.UUID}})
		if err != nil {
			return nil, nil, oops.E(oops.CodeUnexpected, err, "lock mcp server")
		}
		if len(servers) != 1 {
			return nil, nil, oops.E(oops.CodeInvalid, nil, "mcp_server_id does not reference a resource in this project")
		}
		targetServer = &servers[0]
	}
	if err := s.lockMetaMcpServers(ctx, tx, authCtx, uniqueIDs(existing.MetaMcpServerID), existing.MetaMcpServerID); err != nil {
		return nil, nil, err
	}
	if err := verifyEndpointReferenceOwnership(ctx, tx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, existing.McpServerID, existing.MetaMcpServerID, input.CustomDomainID); err != nil {
		return nil, nil, oops.E(oops.CodeInvalid, err, "invalid mcp endpoint")
	}

	if input.Slug != existing.Slug || input.CustomDomainID != existing.CustomDomainID {
		if err := LockSlugScope(ctx, tx, input.CustomDomainID, input.Slug); err != nil {
			return nil, nil, oops.E(oops.CodeUnexpected, err, "lock mcp endpoint slug scope")
		}
		available, err := CheckSlugAvailable(ctx, tx, SlugAvailabilityCheck{
			Slug: input.Slug, CustomDomainID: input.CustomDomainID,
			OrganizationID:   authCtx.ActiveOrganizationID,
			ExcludeToolsetID: uuid.NullUUID{}, ExcludeMcpServerID: existing.McpServerID,
		})
		if err != nil {
			return nil, nil, oops.E(oops.CodeUnexpected, err, "check mcp endpoint slug availability")
		}
		if !available {
			return nil, nil, oops.E(oops.CodeConflict, nil, "mcp endpoint slug already exists for this domain")
		}
	}

	wasRoot := existing.IsDomainRoot.Valid && existing.IsDomainRoot.Bool
	sameDomain := existing.CustomDomainID == input.CustomDomainID
	keepRoot := wasRoot && sameDomain && (targetServer == nil || targetServer.Visibility != mcpservers.VisibilityDisabled)
	rootMarker := pgtype.Bool{}
	if keepRoot {
		rootMarker = pgtype.Bool{Bool: true, Valid: true}
	}
	updated, err := txRepo.UpdateMCPEndpointAddress(ctx, repo.UpdateMCPEndpointAddressParams{
		CustomDomainID: input.CustomDomainID,
		Slug:           input.Slug,
		IsDomainRoot:   rootMarker,
		ID:             existing.ID,
		ProjectID:      *authCtx.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found")
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, nil, oops.E(oops.CodeConflict, err, "mcp endpoint slug already exists for this domain")
		}
		return nil, nil, oops.E(oops.CodeUnexpected, err, "update mcp endpoint address")
	}
	beforeView, afterView := mv.BuildMcpEndpointView(existing), mv.BuildMcpEndpointView(updated)
	if err := s.audit.LogMcpEndpointUpdate(ctx, tx, audit.LogMcpEndpointUpdateEvent{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Actor:                     urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:          authCtx.Email,
		ActorSlug:                 nil,
		McpEndpointURN:            urn.NewMcpEndpoint(updated.ID),
		Slug:                      updated.Slug,
		McpEndpointSnapshotBefore: beforeView,
		McpEndpointSnapshotAfter:  afterView,
	}); err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "log mcp endpoint update")
	}

	var reconcileDomains []uuid.UUID
	if wasRoot && !keepRoot && existing.CustomDomainID.Valid {
		if err := s.logRootAutoClear(ctx, tx, authCtx, existing.CustomDomainID.UUID, existing.ID); err != nil {
			return nil, nil, err
		}
		reconcileDomains = append(reconcileDomains, existing.CustomDomainID.UUID)
	}
	return afterView, reconcileDomains, nil
}
