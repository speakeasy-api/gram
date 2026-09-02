package toolsets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// mirrorToolset projects a locked toolset onto its wrapper and endpoints,
// creating both when absent. previous is the row before this write. Returns
// the custom domains whose root endpoint was cleared, for post-commit reconcile.
func (s *Service) mirrorToolset(ctx context.Context, dbtx pgx.Tx, authCtx *contextvalues.AuthContext, previous, toolset repo.Toolset) ([]uuid.UUID, error) {
	logger := s.logger.With(attr.SlogProjectID(toolset.ProjectID.String()), attr.SlogToolsetSlug(toolset.Slug))
	serversRepo := mcpserversrepo.New(dbtx)
	wantVisibility := hostedmcp.VisibilityForToolset(toolset.McpEnabled, toolset.McpIsPublic)

	wrapper, err := serversRepo.GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{
		ToolsetID: toolset.ID,
		ProjectID: toolset.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		deadDomains, err := s.lockLiveCustomDomains(ctx, dbtx, []uuid.NullUUID{toolset.CustomDomainID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "lock custom domains").LogError(ctx, logger)
		}
		wrapper, err = mcpservers.CreateMCPServerInTransaction(ctx, dbtx, s.audit, mcpservers.MCPServerTransactionInput{
			OrganizationID:        toolset.OrganizationID,
			ProjectID:             toolset.ProjectID,
			ActorUserID:           authCtx.UserID,
			ActorEmail:            authCtx.Email,
			Name:                  toolset.Name,
			Visibility:            wantVisibility,
			EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			UserSessionIssuerID:   toolset.UserSessionIssuerID,
			RemoteMCPServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			TunneledMCPServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
			UnproxiedMCPServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ToolVariationsGroupID: toolset.ToolVariationsGroupID,
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
				return nil, oops.E(oops.CodeConflict, err, "%s", mcpservers.UniqueViolationMessage(pgErr)).LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "create mcp server for toolset").LogError(ctx, logger)
		}
		deadDomain := toolset.CustomDomainID.Valid && deadDomains[toolset.CustomDomainID.UUID]
		return s.mirrorToolsetAddress(ctx, dbtx, logger, authCtx, previous, toolset, wrapper, nil, deadDomain)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server for toolset").LogError(ctx, logger)
	}

	endpointsRepo := mcpendpointsrepo.New(dbtx)
	endpoints, err := endpointsRepo.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID:   toolset.ProjectID,
		McpServerID: wrapper.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp server endpoints").LogError(ctx, logger)
	}
	domainIDs := []uuid.NullUUID{toolset.CustomDomainID}
	if primary := mcpendpoints.PrimaryEndpoint(endpoints); primary != nil {
		domainIDs = append(domainIDs, primary.CustomDomainID)
	}
	deadDomains, err := s.lockLiveCustomDomains(ctx, dbtx, domainIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock custom domains").LogError(ctx, logger)
	}
	if wantVisibility == mcpservers.VisibilityDisabled && wrapper.Visibility != mcpservers.VisibilityDisabled {
		if err := mcpservers.LockMCPServerVisibilityDependencies(ctx, dbtx, toolset.OrganizationID, toolset.ProjectID, wrapper.ID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "lock mcp server visibility dependencies").LogError(ctx, logger)
		}
	}
	locked, err := serversRepo.LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
		ID:        wrapper.ID,
		ProjectID: toolset.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock mcp server").LogError(ctx, logger)
	}

	current, cleared, err := s.reconcileWrapper(ctx, dbtx, logger, authCtx, toolset, locked, wantVisibility, previous.Name != toolset.Name)
	if err != nil {
		return nil, err
	}

	// Re-read under the server lock.
	endpoints, err = endpointsRepo.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID:   toolset.ProjectID,
		McpServerID: current.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp server endpoints").LogError(ctx, logger)
	}
	deadDomain := toolset.CustomDomainID.Valid && deadDomains[toolset.CustomDomainID.UUID]
	more, err := s.mirrorToolsetAddress(ctx, dbtx, logger, authCtx, previous, toolset, current, endpoints, deadDomain)
	if err != nil {
		return nil, err
	}
	return append(cleared, more...), nil
}

// lockLiveCustomDomains locks the given domains in id order and reports the
// ones that no longer exist, which an address projection must skip.
func (s *Service) lockLiveCustomDomains(ctx context.Context, dbtx pgx.Tx, domainIDs []uuid.NullUUID) (map[uuid.UUID]bool, error) {
	dead := map[uuid.UUID]bool{}
	for _, id := range mcpendpoints.UniqueIDs(domainIDs...) {
		if _, err := s.domainsRepo.WithTx(dbtx).LockCustomDomainByID(ctx, id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				dead[id] = true
				continue
			}
			return nil, fmt.Errorf("lock custom domain %s: %w", id, err)
		}
	}
	return dead, nil
}

// reconcileWrapper writes the toolset's visibility, issuer, tool variations
// group, and (when it changed in this write) name onto a locked wrapper.
func (s *Service) reconcileWrapper(ctx context.Context, dbtx pgx.Tx, logger *slog.Logger, authCtx *contextvalues.AuthContext, toolset repo.Toolset, locked mcpserversrepo.McpServer, wantVisibility string, syncName bool) (mcpserversrepo.McpServer, []uuid.UUID, error) {
	current := locked
	var cleared []uuid.UUID

	var name *string
	if syncName && conv.FromPGTextOrEmpty[string](current.Name) != toolset.Name && len(toolset.Name) <= 256 && !strings.ContainsAny(toolset.Name, "\r\n") {
		name = &toolset.Name
	}
	if name != nil || current.Visibility != wantVisibility || current.ToolVariationsGroupID != toolset.ToolVariationsGroupID {
		input := mcpservers.LifecycleUpdateInput{
			OrganizationID:        toolset.OrganizationID,
			ProjectID:             toolset.ProjectID,
			ActorUserID:           authCtx.UserID,
			ActorEmail:            authCtx.Email,
			ServerID:              current.ID,
			Name:                  name,
			Visibility:            wantVisibility,
			EnvironmentID:         current.EnvironmentID,
			UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			RemoteMcpServerID:     current.RemoteMcpServerID,
			TunneledMcpServerID:   current.TunneledMcpServerID,
			ToolsetID:             current.ToolsetID,
			UnproxiedMcpServerID:  current.UnproxiedMcpServerID,
			ToolVariationsGroupID: toolset.ToolVariationsGroupID,
		}
		if wantVisibility == mcpservers.VisibilityDisabled {
			result, err := mcpservers.UpdateMCPServerVisibilityInTransaction(ctx, dbtx, s.audit, current, input)
			if err != nil {
				return current, nil, oops.E(oops.CodeUnexpected, err, "disable mcp server for toolset").LogError(ctx, logger)
			}
			current, cleared = result.Server, result.ClearedRootDomainIDs
		} else {
			updated, err := mcpservers.UpdateMCPServerLifecycleInTransaction(ctx, dbtx, s.audit, current, input)
			if err != nil {
				return current, nil, oops.E(oops.CodeUnexpected, err, "update mcp server for toolset").LogError(ctx, logger)
			}
			current = updated
		}
	}

	if current.UserSessionIssuerID != toolset.UserSessionIssuerID {
		updated, err := mcpserversrepo.New(dbtx).SetMCPServerUserSessionIssuer(ctx, mcpserversrepo.SetMCPServerUserSessionIssuerParams{
			UserSessionIssuerID: toolset.UserSessionIssuerID,
			ID:                  current.ID,
			ProjectID:           toolset.ProjectID,
		})
		if err != nil {
			return current, nil, oops.E(oops.CodeUnexpected, err, "set mcp server issuer for toolset").LogError(ctx, logger)
		}
		current = updated
	}

	return current, cleared, nil
}

// mirrorToolsetAddress makes the wrapper's endpoints carry the toolset's
// (custom_domain_id, mcp_slug): every endpoint that held the previous address
// follows it (an alias twin in the other scope keeps its scope), else the
// ranked primary is re-keyed, else one is created. No slug or a dead domain
// expresses no address and leaves the endpoints alone.
func (s *Service) mirrorToolsetAddress(ctx context.Context, dbtx pgx.Tx, logger *slog.Logger, authCtx *contextvalues.AuthContext, previous, toolset repo.Toolset, wrapper mcpserversrepo.McpServer, endpoints []mcpendpointsrepo.McpEndpoint, deadDomain bool) ([]uuid.UUID, error) {
	if !toolset.McpSlug.Valid || toolset.McpSlug.String == "" || deadDomain {
		return nil, nil
	}
	holdsWanted := func(e mcpendpointsrepo.McpEndpoint) bool {
		return e.CustomDomainID == toolset.CustomDomainID && e.Slug == toolset.McpSlug.String
	}
	held := slices.ContainsFunc(endpoints, holdsWanted)

	var cleared []uuid.UUID
	moved := false
	for i := range endpoints {
		endpoint := &endpoints[i]
		if !previous.McpSlug.Valid || endpoint.Slug != previous.McpSlug.String || holdsWanted(*endpoint) {
			continue
		}
		var ids []uuid.UUID
		var err error
		switch {
		case endpoint.CustomDomainID != previous.CustomDomainID:
			ids, err = s.rekeyEndpoint(ctx, dbtx, logger, authCtx, toolset, endpoint, endpoint.CustomDomainID)
		case held:
			ids, err = s.tombstoneEndpoint(ctx, dbtx, logger, authCtx, toolset, endpoint)
		default:
			ids, err = s.rekeyEndpoint(ctx, dbtx, logger, authCtx, toolset, endpoint, toolset.CustomDomainID)
			moved = true
		}
		if err != nil {
			return nil, err
		}
		cleared = append(cleared, ids...)
	}
	if held || moved {
		return cleared, nil
	}

	primary := mcpendpoints.PrimaryEndpoint(endpoints)
	if primary != nil {
		return s.rekeyEndpoint(ctx, dbtx, logger, authCtx, toolset, primary, toolset.CustomDomainID)
	}
	created, err := mcpendpointsrepo.New(dbtx).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       toolset.ProjectID,
		CustomDomainID:  toolset.CustomDomainID,
		McpServerID:     uuid.NullUUID{UUID: wrapper.ID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            toolset.McpSlug.String,
	})
	if err != nil {
		return nil, mirrorEndpointError(ctx, logger, err, "create mcp endpoint for toolset")
	}
	if err := s.audit.LogMcpEndpointCreate(ctx, dbtx, audit.LogMcpEndpointCreateEvent{
		OrganizationID:   toolset.OrganizationID,
		ProjectID:        toolset.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpEndpointURN:   urn.NewMcpEndpoint(created.ID),
		Slug:             created.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp endpoint creation").LogError(ctx, logger)
	}
	return nil, nil
}

// rekeyEndpoint moves an endpoint to (domain, toolset.mcp_slug); a root marker
// survives only within its domain.
func (s *Service) rekeyEndpoint(ctx context.Context, dbtx pgx.Tx, logger *slog.Logger, authCtx *contextvalues.AuthContext, toolset repo.Toolset, endpoint *mcpendpointsrepo.McpEndpoint, domain uuid.NullUUID) ([]uuid.UUID, error) {
	if endpoint.CustomDomainID == domain && endpoint.Slug == toolset.McpSlug.String {
		return nil, nil
	}
	wasRoot := endpoint.IsDomainRoot.Valid && endpoint.IsDomainRoot.Bool
	keepRoot := wasRoot && endpoint.CustomDomainID == domain
	rootMarker := pgtype.Bool{Bool: false, Valid: false}
	if keepRoot {
		rootMarker = pgtype.Bool{Bool: true, Valid: true}
	}
	updated, err := mcpendpointsrepo.New(dbtx).UpdateMCPEndpoint(ctx, mcpendpointsrepo.UpdateMCPEndpointParams{
		CustomDomainID:  domain,
		McpServerID:     endpoint.McpServerID,
		MetaMcpServerID: endpoint.MetaMcpServerID,
		Slug:            toolset.McpSlug.String,
		IsDomainRoot:    rootMarker,
		ID:              endpoint.ID,
		ProjectID:       toolset.ProjectID,
	})
	if err != nil {
		return nil, mirrorEndpointError(ctx, logger, err, "re-key mcp endpoint for toolset")
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	if err := s.audit.LogMcpEndpointUpdate(ctx, dbtx, audit.LogMcpEndpointUpdateEvent{
		OrganizationID:            toolset.OrganizationID,
		ProjectID:                 toolset.ProjectID,
		Actor:                     actor,
		ActorDisplayName:          authCtx.Email,
		ActorSlug:                 nil,
		McpEndpointURN:            urn.NewMcpEndpoint(updated.ID),
		Slug:                      updated.Slug,
		McpEndpointSnapshotBefore: mv.BuildMcpEndpointView(*endpoint),
		McpEndpointSnapshotAfter:  mv.BuildMcpEndpointView(updated),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp endpoint update").LogError(ctx, logger)
	}
	if wasRoot && !keepRoot {
		if err := mcpservers.LogMCPServerRootAutoClears(ctx, dbtx, s.audit, toolset.OrganizationID, actor, authCtx.Email, []mcpendpointsrepo.McpEndpoint{*endpoint}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log automatic root endpoint cleanup").LogError(ctx, logger)
		}
		return []uuid.UUID{endpoint.CustomDomainID.UUID}, nil
	}
	return nil, nil
}

// tombstoneEndpoint retires an endpoint whose address the toolset left behind.
func (s *Service) tombstoneEndpoint(ctx context.Context, dbtx pgx.Tx, logger *slog.Logger, authCtx *contextvalues.AuthContext, toolset repo.Toolset, endpoint *mcpendpointsrepo.McpEndpoint) ([]uuid.UUID, error) {
	deleted, err := mcpendpointsrepo.New(dbtx).DeleteMCPEndpoint(ctx, mcpendpointsrepo.DeleteMCPEndpointParams{
		ID:        endpoint.ID,
		ProjectID: toolset.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete mcp endpoint for toolset").LogError(ctx, logger)
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	if err := s.audit.LogMcpEndpointDelete(ctx, dbtx, audit.LogMcpEndpointDeleteEvent{
		OrganizationID:   toolset.OrganizationID,
		ProjectID:        toolset.ProjectID,
		Actor:            actor,
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpEndpointURN:   urn.NewMcpEndpoint(deleted.ID),
		Slug:             deleted.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp endpoint deletion").LogError(ctx, logger)
	}
	if endpoint.IsDomainRoot.Valid && endpoint.IsDomainRoot.Bool {
		if err := mcpservers.LogMCPServerRootAutoClears(ctx, dbtx, s.audit, toolset.OrganizationID, actor, authCtx.Email, []mcpendpointsrepo.McpEndpoint{*endpoint}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log automatic root endpoint cleanup").LogError(ctx, logger)
		}
		return []uuid.UUID{endpoint.CustomDomainID.UUID}, nil
	}
	return nil, nil
}

func mirrorEndpointError(ctx context.Context, logger *slog.Logger, err error, msg string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
		return oops.E(oops.CodeConflict, err, "this slug is already taken").LogError(ctx, logger)
	}
	return oops.E(oops.CodeUnexpected, err, "%s", msg).LogError(ctx, logger)
}

// tombstoneToolsetWrapper deletes a deleted toolset's wrapper and endpoints,
// child rows first. Returns the custom domains whose root endpoint went away.
func (s *Service) tombstoneToolsetWrapper(ctx context.Context, dbtx pgx.Tx, authCtx *contextvalues.AuthContext, toolset repo.Toolset) ([]uuid.UUID, error) {
	logger := s.logger.With(attr.SlogProjectID(toolset.ProjectID.String()), attr.SlogToolsetSlug(toolset.Slug))
	wrapper, err := mcpserversrepo.New(dbtx).GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{
		ToolsetID: toolset.ID,
		ProjectID: toolset.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server for toolset").LogError(ctx, logger)
	}

	locked, err := mcpservers.LockMCPServerForDelete(ctx, dbtx, toolset.ProjectID, wrapper.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock mcp server for toolset deletion").LogError(ctx, logger)
	}
	deleted, err := mcpservers.TombstoneMCPServerInTransaction(ctx, dbtx, s.audit, locked, mcpservers.TombstoneInput{
		OrganizationID: toolset.OrganizationID,
		ProjectID:      toolset.ProjectID,
		ActorUserID:    authCtx.UserID,
		ActorEmail:     authCtx.Email,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete mcp server for toolset").LogError(ctx, logger)
	}
	if err := s.audit.LogMcpServerDelete(ctx, dbtx, audit.LogMcpServerDeleteEvent{
		OrganizationID:   toolset.OrganizationID,
		ProjectID:        toolset.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpServerURN:     urn.NewMcpServer(deleted.ID),
		McpServerName:    conv.FromPGTextOrEmpty[string](deleted.Name),
		McpServerSlug:    conv.FromPGTextOrEmpty[string](deleted.Slug),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp server deletion").LogError(ctx, logger)
	}
	return mcpservers.RootDomainIDs(locked.RootEndpoints), nil
}

func (s *Service) reconcileCustomDomains(ctx context.Context, customDomainIDs []uuid.UUID) error {
	if err := background.ReconcileCustomDomains(ctx, s.logger, s.temporalEnv, customDomainIDs); err != nil {
		return fmt.Errorf("reconcile custom domains: %w", err)
	}
	return nil
}
