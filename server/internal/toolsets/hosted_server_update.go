package toolsets

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	networkingressRepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionbindings "github.com/speakeasy-api/gram/server/internal/usersessions/bindings"
	variationsRepo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

var _ mcpservers.HostedServerWriter = (*Service)(nil)

// maxToolsetNameLength mirrors the toolsets.name CHECK constraint and the Goa toolset name limit.
const maxToolsetNameLength = 60

// UpdateHostedServer applies an mcpServers.update on a canonical hosted wrapper to its toolset's
// hosting columns and re-syncs the wrapper, with the side effects of the equivalent toolset writes.
func (s *Service) UpdateHostedServer(ctx context.Context, update mcpservers.HostedServerUpdate) (mcpserversrepo.McpServer, error) {
	return s.writeHostedServer(ctx, update, false)
}

// DisableHostedServer applies an mcpServers.delete on a canonical hosted wrapper as a toolset MCP disable.
func (s *Service) DisableHostedServer(ctx context.Context, toolsetID uuid.UUID) error {
	_, err := s.writeHostedServer(ctx, mcpservers.HostedServerUpdate{
		ToolsetID:             toolsetID,
		Name:                  nil,
		Visibility:            mcpservers.VisibilityDisabled,
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		NetworkAccessMode:     nil,
	}, true)
	return err
}

func (s *Service) writeHostedServer(ctx context.Context, update mcpservers.HostedServerUpdate, keepToolVariationsGroup bool) (mcpserversrepo.McpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return mcpserversrepo.McpServer{}, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()), attr.SlogToolsetID(update.ToolsetID.String()))

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, update.ToolsetID.String(), authCtx.ProjectID.String())); err != nil {
		return mcpserversrepo.McpServer{}, err
	}

	private := update.NetworkAccessMode != nil && !update.NetworkAccessMode.IsPublicOnly()
	var finalizeNetworkAccess networkaccess.AdmissionFinalizer
	if private {
		finalize, err := s.networkAccessEligibility.PrepareNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: authCtx.ActiveOrganizationID, Mode: *update.NetworkAccessMode})
		if err != nil {
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeInvalid, err, "private network access is unavailable")
		}
		finalizeNetworkAccess = finalize
	}

	// Resolved before the transaction so flag latency never holds its locks.
	serverKey := s.hostedServerKeyWrites(ctx, authCtx)
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if private {
		if err := finalizeNetworkAccess.Finalize(ctx, dbtx); err != nil {
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeInvalid, err, "private network access is unavailable")
		}
	} else if err := networkingressRepo.New(dbtx).AcquireNetworkIngressOrganizationLock(ctx, authCtx.ActiveOrganizationID); err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "lock network ingress lifecycle").LogError(ctx, logger)
	}
	if err := admission.LockProject(ctx, dbtx, *authCtx.ProjectID); err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "lock project admission").LogError(ctx, logger)
	}
	if update.UserSessionIssuerID.Valid {
		if _, err := usersessionbindings.ValidateAndLock(ctx, dbtx, update.UserSessionIssuerID.UUID, *authCtx.ProjectID, authCtx.ActiveOrganizationID); err != nil {
			if errors.Is(err, usersessionbindings.ErrNotFound) {
				return mcpserversrepo.McpServer{}, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogWarn(ctx, logger)
			}
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "validate user session issuer").LogError(ctx, logger)
		}
	}
	if update.ToolVariationsGroupID.Valid {
		if _, err := variationsRepo.New(dbtx).GetToolVariationsGroupByID(ctx, variationsRepo.GetToolVariationsGroupByIDParams{ID: update.ToolVariationsGroupID.UUID, ProjectID: *authCtx.ProjectID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return mcpserversrepo.McpServer{}, oops.E(oops.CodeNotFound, err, "tool variations group not found").LogWarn(ctx, logger)
			}
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "load tool variations group").LogError(ctx, logger)
		}
	}

	tr := s.repo.WithTx(dbtx)
	candidate, err := tr.GetToolsetByIDAndProject(ctx, repo.GetToolsetByIDAndProjectParams{ID: update.ToolsetID, ProjectID: *authCtx.ProjectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeNotFound, err, "mcp server not found").LogWarn(ctx, logger)
		}
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "load hosted toolset").LogError(ctx, logger)
	}
	existing, err := tr.GetToolsetForUpdate(ctx, repo.GetToolsetForUpdateParams{Slug: candidate.Slug, ProjectID: *authCtx.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeNotFound, err, "mcp server not found").LogWarn(ctx, logger)
	case err != nil:
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "lock hosted toolset").LogError(ctx, logger)
	case existing.ID != update.ToolsetID:
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeConflict, nil, "hosted toolset changed concurrently; retry the request").LogWarn(ctx, logger)
	}

	var enabled bool
	public := existing.McpIsPublic
	switch update.Visibility {
	case mcpservers.VisibilityDisabled:
		enabled = false
	case mcpservers.VisibilityPrivate:
		enabled, public = true, false
	case mcpservers.VisibilityPublic:
		enabled, public = true, true
	default:
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeBadRequest, nil, "invalid visibility").LogWarn(ctx, logger)
	}
	if enabled && (!existing.McpSlug.Valid || existing.McpSlug.String == "") {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeInvalid, nil, "hosted MCP server has no address to serve").LogWarn(ctx, logger)
	}
	if public != existing.McpIsPublic && (existing.ExternalOauthServerID.Valid || existing.OauthProxyServerID.Valid) {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeInvalid, nil, "remove the toolset's external OAuth server before changing public visibility").LogWarn(ctx, logger)
	}

	beforeView, err := mv.DescribeToolset(ctx, logger, dbtx, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(existing.Slug), new(s.toolsetCache.SkipCache()), nil)
	if err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "describe hosted toolset").LogError(ctx, logger)
	}

	name := existing.Name
	if update.Name != nil {
		if utf8.RuneCountInString(*update.Name) > maxToolsetNameLength {
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeBadRequest, nil, "name must be at most %d characters", maxToolsetNameLength).LogWarn(ctx, logger)
		}
		name = *update.Name
	}
	updated, err := tr.UpdateToolset(ctx, repo.UpdateToolsetParams{
		Name:                   name,
		Description:            existing.Description,
		DefaultEnvironmentSlug: existing.DefaultEnvironmentSlug,
		McpSlug:                existing.McpSlug,
		McpIsPublic:            public,
		CustomDomainID:         existing.CustomDomainID,
		McpEnabled:             enabled,
		ToolSelectionMode:      existing.ToolSelectionMode,
		Slug:                   existing.Slug,
		ProjectID:              *authCtx.ProjectID,
	})
	if err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "update hosted toolset").LogError(ctx, logger)
	}
	if update.UserSessionIssuerID.Valid && update.UserSessionIssuerID != existing.UserSessionIssuerID {
		updated, err = tr.UpdateToolsetUserSessionIssuer(ctx, repo.UpdateToolsetUserSessionIssuerParams{UserSessionIssuerID: update.UserSessionIssuerID, Slug: existing.Slug, ProjectID: *authCtx.ProjectID})
		if err != nil {
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "update hosted toolset user session issuer").LogError(ctx, logger)
		}
	}
	if !keepToolVariationsGroup && update.ToolVariationsGroupID != existing.ToolVariationsGroupID {
		updated, err = tr.UpdateToolsetToolVariationsGroup(ctx, repo.UpdateToolsetToolVariationsGroupParams{ToolVariationsGroupID: update.ToolVariationsGroupID, Slug: existing.Slug, ProjectID: *authCtx.ProjectID})
		if err != nil {
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "update hosted toolset tool variations group").LogError(ctx, logger)
		}
	}

	clearedDomainIDs, err := s.syncHostedServer(ctx, dbtx, authCtx, updated, update.NetworkAccessMode)
	if err != nil {
		return mcpserversrepo.McpServer{}, err
	}

	pluginCreated := false
	if !existing.McpEnabled && updated.McpEnabled {
		pluginCreated, err = s.attachToDefaultPlugin(ctx, dbtx, authCtx, updated.ID, updated.Name, serverKey)
		if err != nil {
			return mcpserversrepo.McpServer{}, err
		}
	}
	// A package renders an OAuth server differently from a key-authenticated one.
	if updated.McpEnabled && existing.UserSessionIssuerID.Valid != updated.UserSessionIssuerID.Valid {
		carried, err := toolsetCarriedByPlugin(ctx, dbtx, authCtx, updated.ID)
		if err != nil {
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "check toolset plugin membership").LogError(ctx, logger)
		}
		if carried {
			if err := s.requestPluginPublication(ctx, dbtx, authCtx); err != nil {
				return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "enqueue toolset plugin publication").LogError(ctx, logger)
			}
		}
	}

	afterView, err := mv.DescribeToolset(ctx, logger, dbtx, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(updated.Slug), new(s.toolsetCache.SkipCache()), nil)
	if err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "describe updated hosted toolset").LogError(ctx, logger)
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
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "log hosted toolset update").LogError(ctx, logger)
	}

	server, err := mcpserversrepo.New(dbtx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: updated.ID, ProjectID: *authCtx.ProjectID})
	if err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "load synced hosted MCP server").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "commit hosted MCP server update").LogError(ctx, logger)
	}

	s.triggerPluginPublish(ctx, authCtx, existing.McpEnabled || updated.McpEnabled, pluginCreated)
	s.triggerToolsetIndex(ctx, afterView)
	if err := s.reconcileCustomDomains(ctx, clearedDomainIDs); err != nil {
		return mcpserversrepo.McpServer{}, err
	}
	return server, nil
}
