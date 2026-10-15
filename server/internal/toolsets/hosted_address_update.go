package toolsets

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	networkingressRepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ mcpendpoints.HostedAddressWriter = (*Service)(nil)

// maxMCPSlugLength mirrors the Goa toolset mcp_slug limit.
const maxMCPSlugLength = 40

// UpdateHostedAddress applies an mcpEndpoints.update on a canonical hosted wrapper's endpoint as the equivalent
// toolset mcp_slug / custom domain change, then re-syncs the wrapper so the endpoint moves in place.
func (s *Service) UpdateHostedAddress(ctx context.Context, update mcpendpoints.HostedAddressUpdate) (mcpendpointsrepo.McpEndpoint, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return mcpendpointsrepo.McpEndpoint{}, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()), attr.SlogToolsetID(update.ToolsetID.String()))

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, update.ToolsetID.String(), authCtx.ProjectID.String())); err != nil {
		return mcpendpointsrepo.McpEndpoint{}, err
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := networkingressRepo.New(dbtx).AcquireNetworkIngressOrganizationLock(ctx, authCtx.ActiveOrganizationID); err != nil {
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, err, "lock network ingress lifecycle").LogError(ctx, logger)
	}
	if err := admission.LockProject(ctx, dbtx, *authCtx.ProjectID); err != nil {
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, err, "lock project admission").LogError(ctx, logger)
	}

	tr := s.repo.WithTx(dbtx)
	candidate, err := tr.GetToolsetByIDAndProject(ctx, repo.GetToolsetByIDAndProjectParams{ID: update.ToolsetID, ProjectID: *authCtx.ProjectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").LogWarn(ctx, logger)
		}
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, err, "load hosted toolset").LogError(ctx, logger)
	}
	existing, err := tr.GetToolsetForUpdate(ctx, repo.GetToolsetForUpdateParams{Slug: candidate.Slug, ProjectID: *authCtx.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").LogWarn(ctx, logger)
	case err != nil:
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, err, "lock hosted toolset").LogError(ctx, logger)
	case existing.ID != update.ToolsetID:
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeConflict, nil, "hosted toolset changed concurrently; retry the request").LogWarn(ctx, logger)
	}

	// Hosted endpoints move only under the toolset row lock; a domain deletion can still retire them.
	endpointRepo := mcpendpointsrepo.New(dbtx)
	before, err := endpointRepo.GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{ID: update.EndpointID, ProjectID: *authCtx.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").LogWarn(ctx, logger)
	case err != nil:
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, err, "load hosted mcp endpoint").LogError(ctx, logger)
	case !before.McpServerID.Valid || before.McpServerID.UUID != existing.ID:
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeConflict, nil, "mcp endpoint changed concurrently; retry the request").LogWarn(ctx, logger)
	}

	var requestedDomainID *string
	if update.CustomDomainID.Valid {
		requestedDomainID = new(update.CustomDomainID.UUID.String())
	}
	customDomainID, err := s.resolveCustomDomain(ctx, dbtx, logger, authCtx, existing, requestedDomainID)
	if err != nil {
		return mcpendpointsrepo.McpEndpoint{}, err
	}
	if customDomainID != update.CustomDomainID {
		if existing.CustomDomainID.Valid && !update.CustomDomainID.Valid {
			return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeInvalid, nil, "a hosted MCP server cannot leave its custom domain").LogWarn(ctx, logger)
		}
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeInvalid, nil, "a hosted MCP server can only move to the organization's verified custom domain").LogWarn(ctx, logger)
	}
	// An unchanged slug is omitted, as a toolset save that leaves mcp_slug out.
	var requestedSlug *string
	if update.Slug != existing.McpSlug.String {
		if len(update.Slug) > maxMCPSlugLength {
			return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeBadRequest, nil, "a hosted MCP slug must be at most %d characters", maxMCPSlugLength).LogWarn(ctx, logger)
		}
		requestedSlug = &update.Slug
	}
	mcpSlug, err := s.resolveMCPAddress(ctx, dbtx, logger, authCtx, existing, requestedSlug, customDomainID)
	if err != nil {
		return mcpendpointsrepo.McpEndpoint{}, err
	}

	beforeView, err := mv.DescribeToolset(ctx, logger, dbtx, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(existing.Slug), new(s.toolsetCache.SkipCache()), nil)
	if err != nil {
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, err, "describe hosted toolset").LogError(ctx, logger)
	}
	updated, err := tr.UpdateToolset(ctx, repo.UpdateToolsetParams{
		Name:                   existing.Name,
		Description:            existing.Description,
		DefaultEnvironmentSlug: existing.DefaultEnvironmentSlug,
		McpSlug:                mcpSlug,
		McpIsPublic:            existing.McpIsPublic,
		CustomDomainID:         customDomainID,
		McpEnabled:             existing.McpEnabled,
		ToolSelectionMode:      existing.ToolSelectionMode,
		Slug:                   existing.Slug,
		ProjectID:              *authCtx.ProjectID,
	})
	if err != nil {
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, err, "update hosted toolset address").LogError(ctx, logger)
	}

	clearedDomainIDs, err := s.syncHostedServer(ctx, dbtx, authCtx, updated, nil)
	if err != nil {
		return mcpendpointsrepo.McpEndpoint{}, err
	}

	afterView, err := mv.DescribeToolset(ctx, logger, dbtx, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(updated.Slug), new(s.toolsetCache.SkipCache()), nil)
	if err != nil {
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, err, "describe updated hosted toolset").LogError(ctx, logger)
	}
	if err := s.audit.LogToolsetUpdate(ctx, dbtx, audit.LogToolsetUpdateEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		ToolsetURN:            urn.NewToolset(updated.ID),
		ToolsetName:           updated.Name,
		ToolsetSlug:           updated.Slug,
		ToolsetVersionAfter:   afterView.ToolsetVersion,
		ToolsetSnapshotBefore: beforeView,
		ToolsetSnapshotAfter:  afterView,
	}); err != nil {
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, err, "log hosted toolset address update").LogError(ctx, logger)
	}

	after, err := endpointRepo.GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{ID: update.EndpointID, ProjectID: *authCtx.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeConflict, err, "mcp endpoint changed concurrently; retry the request").LogWarn(ctx, logger)
	case err != nil:
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, err, "load synced hosted mcp endpoint").LogError(ctx, logger)
	}
	if after.Slug != updated.McpSlug.String || after.CustomDomainID != updated.CustomDomainID {
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, nil, "hosted mcp endpoint did not follow its toolset address").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return mcpendpointsrepo.McpEndpoint{}, oops.E(oops.CodeUnexpected, err, "commit hosted MCP address update").LogError(ctx, logger)
	}

	s.triggerPluginPublish(ctx, authCtx, existing.McpEnabled || updated.McpEnabled, false)
	s.triggerToolsetIndex(ctx, afterView)
	if err := s.reconcileCustomDomains(ctx, clearedDomainIDs); err != nil {
		return mcpendpointsrepo.McpEndpoint{}, err
	}
	return after, nil
}
