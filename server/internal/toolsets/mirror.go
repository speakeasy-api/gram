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

// mirrorWrapperSlug derives the project-unique internal slug for a toolset's
// wrapper mcp_servers row. The suffix keeps it clear of user-chosen server
// slugs; the public URL slug lives on the mcp_endpoints row, never here.
func mirrorWrapperSlug(toolset repo.Toolset) string {
	compact := strings.ReplaceAll(toolset.ID.String(), "-", "")
	return toolset.Slug + "-" + compact[:8]
}

// effectiveMcpPublic reports whether a toolset's MCP surface is public,
// answering from its wrapper mcp_servers visibility. Ambiguous wrapper state
// (zero or multiple wrappers) reads as not public.
func (s *Service) effectiveMcpPublic(ctx context.Context, projectID, toolsetID uuid.UUID) (bool, error) {
	wrappers, err := mcpserversRepo.New(s.db).GetMCPServersByToolsetID(ctx, mcpserversRepo.GetMCPServersByToolsetIDParams{
		ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true},
		ProjectID: projectID,
	})
	if err != nil {
		return false, fmt.Errorf("load wrapper mcp servers for toolset: %w", err)
	}
	if len(wrappers) != 1 {
		return false, nil
	}
	return wrappers[0].Visibility == mcpservers.VisibilityPublic, nil
}

// createWrapperForNewToolset provisions the canonical wrapper mcp_servers
// row and platform mcp_endpoints row for a newly created toolset, returning
// the wrapper's id. New toolsets carry no publishing columns — publishing
// state is born on the wrapper — so this is a direct creation rather than a
// column mirror.
func (s *Service) createWrapperForNewToolset(ctx context.Context, dbtx pgx.Tx, toolset repo.Toolset, endpointSlug, visibility string) (uuid.UUID, error) {
	var environmentID uuid.NullUUID
	if slug := conv.FromPGText[string](toolset.DefaultEnvironmentSlug); slug != nil && *slug != "" {
		env, err := environmentsRepo.New(dbtx).GetEnvironmentBySlug(ctx, environmentsRepo.GetEnvironmentBySlugParams{
			Slug:      *slug,
			ProjectID: toolset.ProjectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return uuid.Nil, fmt.Errorf("resolve toolset default environment: %w", err)
		default:
			environmentID = uuid.NullUUID{UUID: env.ID, Valid: true}
		}
	}

	serverID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("generate wrapper mcp server id: %w", err)
	}
	wrapper, err := mcpserversRepo.New(dbtx).CreateMCPServer(ctx, mcpserversRepo.CreateMCPServerParams{
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
		return uuid.Nil, fmt.Errorf("create wrapper mcp server: %w", err)
	}
	if _, err := mcpendpointsRepo.New(dbtx).CreateMCPEndpoint(ctx, mcpendpointsRepo.CreateMCPEndpointParams{
		ProjectID:      toolset.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    wrapper.ID,
		Slug:           endpointSlug,
	}); err != nil {
		return uuid.Nil, fmt.Errorf("create wrapper mcp endpoint: %w", err)
	}
	return wrapper.ID, nil
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
