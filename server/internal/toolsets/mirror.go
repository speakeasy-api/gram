package toolsets

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	environmentsRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpendpointsRepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// visibilityForToolset maps the toolset publishing flags onto mcp_servers
// visibility: disabled always wins over the public flag.
func visibilityForToolset(mcpEnabled, mcpIsPublic bool) string {
	switch {
	case !mcpEnabled:
		return mcpservers.VisibilityDisabled
	case mcpIsPublic:
		return mcpservers.VisibilityPublic
	default:
		return mcpservers.VisibilityPrivate
	}
}

// mirrorWrapperSlug derives the project-unique internal slug for a toolset's
// wrapper mcp_servers row. The suffix keeps it clear of user-chosen server
// slugs; the public URL slug lives on the mcp_endpoints row, never here.
func mirrorWrapperSlug(toolset repo.Toolset) string {
	compact := strings.ReplaceAll(toolset.ID.String(), "-", "")
	return toolset.Slug + "-" + compact[:8]
}

// mirrorPublishingState reconciles the canonical mcp_servers/mcp_endpoints
// projection of a toolset's publishing columns (mcp_slug, mcp_enabled,
// mcp_is_public, custom_domain_id, default_environment_slug) inside the
// caller's transaction. It runs on every toolset write during the
// expand/contract data-model swap so the two models cannot drift between the
// backfill and the write-path cutover.
//
// Root mappings on a wrapper transitioning to disabled — or on an endpoint
// whose address is moving — are cleared here with the same domain-lock
// ordering as the mcpservers/mcpendpoints services. The returned domain ids
// need reconciliation via reconcileMirroredDomains AFTER the transaction
// commits.
//
// A toolset with multiple live wrappers, or a wrapper whose addressing has
// diverged from the toolset columns, is ambiguous: the write fails with a
// conflict rather than guessing or acknowledging a change the runtime will
// not serve.
func (s *Service) mirrorPublishingState(ctx context.Context, dbtx pgx.Tx, toolset repo.Toolset, authCtx *contextvalues.AuthContext) ([]uuid.UUID, error) {
	if !toolset.McpSlug.Valid || toolset.McpSlug.String == "" {
		return nil, nil
	}

	serversTx := mcpserversRepo.New(dbtx)
	endpointsTx := mcpendpointsRepo.New(dbtx)

	visibility := visibilityForToolset(toolset.McpEnabled, toolset.McpIsPublic)

	var environmentID uuid.NullUUID
	if slug := conv.FromPGText[string](toolset.DefaultEnvironmentSlug); slug != nil && *slug != "" {
		env, err := environmentsRepo.New(dbtx).GetEnvironmentBySlug(ctx, environmentsRepo.GetEnvironmentBySlugParams{
			Slug:      *slug,
			ProjectID: toolset.ProjectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// Dangling default environment slug: mirror without an
			// environment rather than failing the toolset write.
		case err != nil:
			return nil, fmt.Errorf("resolve toolset default environment: %w", err)
		default:
			environmentID = uuid.NullUUID{UUID: env.ID, Valid: true}
		}
	}

	wrappers, err := serversTx.GetMCPServersByToolsetID(ctx, mcpserversRepo.GetMCPServersByToolsetIDParams{
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID: toolset.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("load wrapper mcp servers for toolset: %w", err)
	}

	// Ambiguous wrapper state must fail the write rather than report
	// success while the canonical rows stay stale — an old SDK client
	// would otherwise see its publishing change acknowledged with the
	// runtime still serving the previous state.
	if len(wrappers) > 1 {
		return nil, oops.E(oops.CodeConflict, nil, "toolset has multiple MCP servers; update publishing through the mcpServers API").LogError(ctx, s.logger)
	}

	var clearedDomainIDs []uuid.UUID

	if len(wrappers) == 0 {
		serverID, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("generate wrapper mcp server id: %w", err)
		}
		wrapper, err := serversTx.CreateMCPServer(ctx, mcpserversRepo.CreateMCPServerParams{
			ID:                    serverID,
			ProjectID:             toolset.ProjectID,
			Name:                  conv.ToPGText(toolset.Name),
			Slug:                  conv.ToPGText(mirrorWrapperSlug(toolset)),
			EnvironmentID:         environmentID,
			UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
			ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Visibility:            visibility,
		})
		if err != nil {
			return nil, fmt.Errorf("create wrapper mcp server: %w", err)
		}
		if _, err := endpointsTx.CreateMCPEndpoint(ctx, mcpendpointsRepo.CreateMCPEndpointParams{
			ProjectID:      toolset.ProjectID,
			CustomDomainID: toolset.CustomDomainID,
			McpServerID:    wrapper.ID,
			Slug:           toolset.McpSlug.String,
		}); err != nil {
			return nil, fmt.Errorf("create wrapper mcp endpoint: %w", err)
		}
		return nil, nil
	}

	wrapper := wrappers[0]

	// Clearing a root mapping requires the owning domain rows locked first —
	// same order as the mcpservers disable path — so root selection cannot
	// race this transaction.
	if visibility == mcpservers.VisibilityDisabled && wrapper.Visibility != mcpservers.VisibilityDisabled {
		domainIDs, err := endpointsTx.ListCustomDomainIDsByMCPServerID(ctx, mcpendpointsRepo.ListCustomDomainIDsByMCPServerIDParams{
			McpServerID: wrapper.ID,
			ProjectID:   wrapper.ProjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("list custom domains for wrapper endpoints: %w", err)
		}
		cleared, err := s.clearWrapperRoots(ctx, dbtx, authCtx, wrapper.ID, wrapper.ProjectID, domainIDs)
		if err != nil {
			return nil, err
		}
		clearedDomainIDs = append(clearedDomainIDs, cleared...)
	}

	if _, err := serversTx.UpdateMCPServer(ctx, mcpserversRepo.UpdateMCPServerParams{
		ID:                    wrapper.ID,
		ProjectID:             wrapper.ProjectID,
		Name:                  conv.ToPGText(toolset.Name),
		Slug:                  wrapper.Slug,
		EnvironmentID:         environmentID,
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ToolVariationsGroupID: wrapper.ToolVariationsGroupID,
		Visibility:            visibility,
	}); err != nil {
		return nil, fmt.Errorf("update wrapper mcp server: %w", err)
	}

	endpoints, err := endpointsTx.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsRepo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID:   wrapper.ProjectID,
		McpServerID: wrapper.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("list wrapper mcp endpoints: %w", err)
	}

	var exact *mcpendpointsRepo.McpEndpoint
	for i := range endpoints {
		if endpoints[i].Slug == toolset.McpSlug.String && endpoints[i].CustomDomainID == toolset.CustomDomainID {
			exact = &endpoints[i]
			break
		}
	}

	switch {
	case exact != nil:
		// Address already mirrored.
	case len(endpoints) == 1:
		endpoint := endpoints[0]
		// The address is moving. A root mapping is bound to the old
		// domain, so it cannot survive the move.
		if endpoint.IsDomainRoot.Valid && endpoint.IsDomainRoot.Bool && endpoint.CustomDomainID.Valid {
			cleared, err := s.clearWrapperRoots(ctx, dbtx, authCtx, wrapper.ID, wrapper.ProjectID, []uuid.UUID{endpoint.CustomDomainID.UUID})
			if err != nil {
				return nil, err
			}
			clearedDomainIDs = append(clearedDomainIDs, cleared...)
		}
		if _, err := endpointsTx.UpdateMCPEndpoint(ctx, mcpendpointsRepo.UpdateMCPEndpointParams{
			ID:             endpoint.ID,
			ProjectID:      endpoint.ProjectID,
			CustomDomainID: toolset.CustomDomainID,
			McpServerID:    wrapper.ID,
			Slug:           toolset.McpSlug.String,
			IsDomainRoot:   conv.PtrToPGBool(nil),
		}); err != nil {
			return nil, fmt.Errorf("update wrapper mcp endpoint address: %w", err)
		}
	case len(endpoints) == 0:
		if _, err := endpointsTx.CreateMCPEndpoint(ctx, mcpendpointsRepo.CreateMCPEndpointParams{
			ProjectID:      toolset.ProjectID,
			CustomDomainID: toolset.CustomDomainID,
			McpServerID:    wrapper.ID,
			Slug:           toolset.McpSlug.String,
		}); err != nil {
			return nil, fmt.Errorf("create wrapper mcp endpoint: %w", err)
		}
	default:
		// Multiple endpoints and none carries the toolset's address:
		// user-managed addressing has diverged from the toolset columns.
		// Fail the write rather than guessing which endpoint to move or
		// acknowledging a change the runtime will not serve.
		return nil, oops.E(oops.CodeConflict, nil, "toolset addressing has diverged; update publishing through the mcpEndpoints API").LogError(ctx, s.logger)
	}

	return clearedDomainIDs, nil
}

// clearWrapperRoots locks the given domains in sorted order, clears any root
// mappings held by the wrapper's endpoints on them, and records the
// custom-domain audit events. Returns the domain ids whose root was cleared.
func (s *Service) clearWrapperRoots(ctx context.Context, dbtx pgx.Tx, authCtx *contextvalues.AuthContext, wrapperID, projectID uuid.UUID, domainIDs []uuid.UUID) ([]uuid.UUID, error) {
	unique := make(map[uuid.UUID]struct{}, len(domainIDs))
	var ordered []uuid.UUID
	for _, id := range domainIDs {
		if _, seen := unique[id]; seen {
			continue
		}
		unique[id] = struct{}{}
		ordered = append(ordered, id)
	}
	if len(ordered) == 0 {
		return nil, nil
	}
	slices.SortFunc(ordered, func(a, b uuid.UUID) int {
		return strings.Compare(a.String(), b.String())
	})

	domainsTx := customdomainsRepo.New(dbtx)
	for _, domainID := range ordered {
		if _, err := domainsTx.LockCustomDomainByID(ctx, domainID); err != nil {
			return nil, fmt.Errorf("lock custom domain %s: %w", domainID, err)
		}
	}

	endpointsTx := mcpendpointsRepo.New(dbtx)
	roots, err := endpointsTx.LockRootMCPEndpointsByMCPServerID(ctx, mcpendpointsRepo.LockRootMCPEndpointsByMCPServerIDParams{
		McpServerID: wrapperID,
		ProjectID:   projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("lock root mcp endpoints for wrapper: %w", err)
	}
	if len(roots) == 0 {
		return nil, nil
	}
	if _, err := endpointsTx.ClearRootMCPEndpointsByMCPServerID(ctx, mcpendpointsRepo.ClearRootMCPEndpointsByMCPServerIDParams{
		McpServerID: wrapperID,
		ProjectID:   projectID,
	}); err != nil {
		return nil, fmt.Errorf("clear root mcp endpoints for wrapper: %w", err)
	}

	var cleared []uuid.UUID
	for _, root := range roots {
		if !root.CustomDomainID.Valid {
			continue
		}
		domain, err := domainsTx.GetCustomDomainByID(ctx, root.CustomDomainID.UUID)
		if err != nil {
			return nil, fmt.Errorf("load custom domain for root cleanup audit: %w", err)
		}
		if err := s.audit.LogCustomDomainUpdate(ctx, dbtx, audit.LogCustomDomainUpdateEvent{
			OrganizationID:             authCtx.ActiveOrganizationID,
			Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:           authCtx.Email,
			ActorSlug:                  nil,
			CustomDomainURN:            urn.NewCustomDomain(domain.ID),
			DomainName:                 domain.Domain,
			CustomDomainSnapshotBefore: mv.BuildCustomDomainView(domain, false, root.ID),
			CustomDomainSnapshotAfter:  mv.BuildCustomDomainView(domain, false, uuid.Nil),
		}); err != nil {
			return nil, fmt.Errorf("log automatic root endpoint cleanup: %w", err)
		}
		cleared = append(cleared, root.CustomDomainID.UUID)
	}
	return cleared, nil
}

// tombstoneWrapper soft-deletes a toolset's wrapper mcp_servers rows and
// their endpoints inside the caller's transaction, clearing root mappings
// first. Called by DeleteToolset so a tombstoned toolset cannot leave a live
// wrapper serving its tools. Returns domain ids needing reconciliation after
// commit.
func (s *Service) tombstoneWrapper(ctx context.Context, dbtx pgx.Tx, toolset repo.Toolset, authCtx *contextvalues.AuthContext) ([]uuid.UUID, error) {
	serversTx := mcpserversRepo.New(dbtx)
	endpointsTx := mcpendpointsRepo.New(dbtx)

	wrappers, err := serversTx.GetMCPServersByToolsetID(ctx, mcpserversRepo.GetMCPServersByToolsetIDParams{
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID: toolset.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("load wrapper mcp servers for toolset: %w", err)
	}

	var clearedDomainIDs []uuid.UUID
	for _, wrapper := range wrappers {
		domainIDs, err := endpointsTx.ListCustomDomainIDsByMCPServerID(ctx, mcpendpointsRepo.ListCustomDomainIDsByMCPServerIDParams{
			McpServerID: wrapper.ID,
			ProjectID:   wrapper.ProjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("list custom domains for wrapper endpoints: %w", err)
		}
		cleared, err := s.clearWrapperRoots(ctx, dbtx, authCtx, wrapper.ID, wrapper.ProjectID, domainIDs)
		if err != nil {
			return nil, err
		}
		clearedDomainIDs = append(clearedDomainIDs, cleared...)

		if _, err := endpointsTx.SoftDeleteMCPEndpointsByMCPServerID(ctx, mcpendpointsRepo.SoftDeleteMCPEndpointsByMCPServerIDParams{
			McpServerID: wrapper.ID,
			ProjectID:   wrapper.ProjectID,
		}); err != nil {
			return nil, fmt.Errorf("soft delete wrapper mcp endpoints: %w", err)
		}
		if _, err := serversTx.DeleteMCPServer(ctx, mcpserversRepo.DeleteMCPServerParams{
			ID:        wrapper.ID,
			ProjectID: wrapper.ProjectID,
		}); err != nil {
			return nil, fmt.Errorf("soft delete wrapper mcp server: %w", err)
		}
	}

	return clearedDomainIDs, nil
}

// reconcileMirroredDomains schedules ingress reconciliation for domains whose
// root mapping was cleared by a mirror operation. Must run after the
// mirroring transaction commits.
func (s *Service) reconcileMirroredDomains(ctx context.Context, customDomainIDs []uuid.UUID) {
	if s.temporalEnv == nil || len(customDomainIDs) == 0 {
		return
	}
	for _, customDomainID := range customDomainIDs {
		if _, err := (&background.CustomDomainRegistrationClient{TemporalEnv: s.temporalEnv}).ExecuteCustomDomainReconcile(ctx, customDomainID); err != nil {
			_ = oops.E(oops.CodeUnexpected, err, "start custom domain reconciliation").LogError(ctx, s.logger)
		}
	}
}
