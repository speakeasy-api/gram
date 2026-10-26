package toolsets

import (
	"context"
	"errors"
	"slices"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostingstate"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tombstone"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	networkingressRepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionbindings "github.com/speakeasy-api/gram/server/internal/usersessions/bindings"
	"github.com/speakeasy-api/gram/server/internal/usersessions/lifecycle"
	variationsRepo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

var _ mcpservers.HostedServerWriter = (*Service)(nil)

// maxToolsetNameLength mirrors the toolsets.name CHECK constraint and the Goa toolset name limit.
const maxToolsetNameLength = 60

// UpdateHostedServer applies an mcpServers.update to a canonical hosted wrapper. The wrapper owns the
// hosting state; only the name and tool variations group, which the toolset shares, are written to the toolset too.
func (s *Service) UpdateHostedServer(ctx context.Context, update mcpservers.HostedServerUpdate) (mcpserversrepo.McpServer, error) {
	return s.writeHostedServer(ctx, update, false)
}

// DisableHostedServer applies an mcpServers.delete on a canonical hosted wrapper: it disables the wrapper and keeps it.
func (s *Service) DisableHostedServer(ctx context.Context, toolsetID uuid.UUID) error {
	_, err := s.writeHostedServer(ctx, mcpservers.HostedServerUpdate{
		ToolsetID:              toolsetID,
		Name:                   nil,
		Visibility:             mcpservers.VisibilityDisabled,
		UserSessionIssuerID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClearUserSessionIssuer: false,
		ToolVariationsGroupID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		NetworkAccessMode:      nil,
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
	if update.Name != nil && utf8.RuneCountInString(*update.Name) > maxToolsetNameLength {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeBadRequest, nil, "name must be at most %d characters", maxToolsetNameLength).LogWarn(ctx, logger)
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

	existing, err := lockHostedToolset(ctx, dbtx, authCtx, update.ToolsetID)
	if err != nil {
		return mcpserversrepo.McpServer{}, err
	}
	beforeView, err := mv.DescribeToolset(ctx, logger, dbtx, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(existing.Slug), new(s.toolsetCache.SkipCache()), nil)
	if err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "describe hosted toolset").LogError(ctx, logger)
	}

	tr := s.repo.WithTx(dbtx)
	updated := existing
	if update.Name != nil && *update.Name != existing.Name {
		updated, err = tr.UpdateToolsetDetails(ctx, repo.UpdateToolsetDetailsParams{
			Name: *update.Name, Description: existing.Description, DefaultEnvironmentSlug: existing.DefaultEnvironmentSlug,
			ToolSelectionMode: existing.ToolSelectionMode, Slug: existing.Slug, ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "rename hosted toolset").LogError(ctx, logger)
		}
	}
	change := hostedServerChange{
		Name:                   update.Name,
		Visibility:             &update.Visibility,
		UserSessionIssuerID:    update.UserSessionIssuerID,
		ClearUserSessionIssuer: update.ClearUserSessionIssuer,
		ToolVariationsGroupID:  nil,
		NetworkAccessMode:      update.NetworkAccessMode,
	}
	if !keepToolVariationsGroup {
		change.ToolVariationsGroupID = &update.ToolVariationsGroupID
		if update.ToolVariationsGroupID != existing.ToolVariationsGroupID {
			updated, err = tr.UpdateToolsetToolVariationsGroup(ctx, repo.UpdateToolsetToolVariationsGroupParams{ToolVariationsGroupID: update.ToolVariationsGroupID, Slug: existing.Slug, ProjectID: *authCtx.ProjectID})
			if err != nil {
				return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "update hosted toolset tool variations group").LogError(ctx, logger)
			}
		}
	}

	written, err := applyHostedServerChange(ctx, dbtx, s.audit, hostedActor(authCtx), updated, change)
	if err != nil {
		return mcpserversrepo.McpServer{}, err
	}
	wasEnabled := written.Before.Visibility != mcpservers.VisibilityDisabled
	nowEnabled := written.After.Visibility != mcpservers.VisibilityDisabled

	pluginCreated := false
	if !wasEnabled && nowEnabled {
		pluginCreated, err = s.attachToDefaultPlugin(ctx, dbtx, authCtx, updated.ID, updated.Name)
		if err != nil {
			return mcpserversrepo.McpServer{}, err
		}
	}
	// A package renders an OAuth server differently from a key-authenticated one.
	if nowEnabled && written.Before.UserSessionIssuerID.Valid != written.After.UserSessionIssuerID.Valid {
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

	if err := dbtx.Commit(ctx); err != nil {
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "commit hosted MCP server update").LogError(ctx, logger)
	}

	s.triggerPluginPublish(ctx, authCtx, wasEnabled || nowEnabled, pluginCreated)
	s.triggerToolsetIndex(ctx, afterView)
	if err := s.reconcileCustomDomains(ctx, written.ClearedRootDomainIDs); err != nil {
		return mcpserversrepo.McpServer{}, err
	}
	return written.After, nil
}

// lockHostedToolset locks the toolset behind a canonical wrapper, which hosted writes take before the wrapper.
func lockHostedToolset(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, toolsetID uuid.UUID) (repo.Toolset, error) {
	toolsets := repo.New(tx)
	candidate, err := toolsets.GetToolsetByIDAndProject(ctx, repo.GetToolsetByIDAndProjectParams{ID: toolsetID, ProjectID: *authCtx.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.Toolset{}, oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return repo.Toolset{}, oops.E(oops.CodeUnexpected, err, "load hosted toolset")
	case candidate.OrganizationID != authCtx.ActiveOrganizationID:
		return repo.Toolset{}, oops.E(oops.CodeNotFound, nil, "mcp server not found")
	}
	locked, err := toolsets.GetToolsetForUpdate(ctx, repo.GetToolsetForUpdateParams{Slug: candidate.Slug, ProjectID: *authCtx.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.Toolset{}, oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return repo.Toolset{}, oops.E(oops.CodeUnexpected, err, "lock hosted toolset")
	case locked.ID != toolsetID:
		return repo.Toolset{}, oops.E(oops.CodeConflict, nil, "hosted toolset changed concurrently; retry the request")
	}
	return locked, nil
}

// hostedServerChange is a write to a canonical wrapper; nil or invalid fields keep the stored value.
type hostedServerChange struct {
	// Name renames the wrapper; the caller has validated it.
	Name *string

	// Visibility is the requested disabled, private or public state.
	Visibility *string

	// UserSessionIssuerID links an issuer when valid; invalid keeps the current one.
	UserSessionIssuerID uuid.NullUUID

	// ClearUserSessionIssuer detaches the issuer; UserSessionIssuerID is then invalid.
	ClearUserSessionIssuer bool

	// ToolVariationsGroupID replaces the tool filtering group when set; an invalid value clears it.
	ToolVariationsGroupID *uuid.NullUUID

	// NetworkAccessMode changes the network policy when set.
	NetworkAccessMode *networkaccess.Mode
}

// hostedServerWrite is the wrapper before and after a change, and the custom domains whose root it cleared.
type hostedServerWrite struct {
	// Before is the locked wrapper as it was.
	Before mcpserversrepo.McpServer

	// After is the wrapper as written.
	After mcpserversrepo.McpServer

	// ClearedRootDomainIDs need a reconcile after commit.
	ClearedRootDomainIDs []uuid.UUID
}

// applyHostedServerChange writes a change to the canonical wrapper of a locked toolset, never to the toolset,
// under hostedmcp.LockServer's locks.
func applyHostedServerChange(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor hostedmcp.Actor, toolset repo.Toolset, change hostedServerChange) (hostedServerWrite, error) {
	endpoints, canonical, err := hostedmcp.LockServer(ctx, tx, toolset.OrganizationID, toolset.ProjectID, toolset.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return hostedServerWrite{}, oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return hostedServerWrite{}, oops.E(oops.CodeUnexpected, err, "lock hosted MCP server")
	case !hostedmcp.IsCanonical(&canonical, toolset.ID):
		return hostedServerWrite{}, oops.E(oops.CodeConflict, nil, "hosted MCP identity belongs to another server")
	}
	servers := mcpserversrepo.New(tx)
	endpointRepo := mcpendpointsrepo.New(tx)

	visibility := canonical.Visibility
	if change.Visibility != nil {
		visibility = *change.Visibility
	}
	switch visibility {
	case mcpservers.VisibilityDisabled, mcpservers.VisibilityPrivate, mcpservers.VisibilityPublic:
	default:
		return hostedServerWrite{}, oops.E(oops.CodeBadRequest, nil, "invalid visibility")
	}
	enabled := visibility != mcpservers.VisibilityDisabled
	if enabled && canonical.Visibility == mcpservers.VisibilityDisabled {
		if err := hostedmcp.RequireLiveAddress(ctx, tx, toolset.OrganizationID, canonical); err != nil {
			return hostedServerWrite{}, err //nolint:wrapcheck // oops errors pass through.
		}
	}
	// External OAuth is only allowed on public servers; disabling keeps it for a later re-enable.
	if visibility == mcpservers.VisibilityPrivate && canonical.Visibility != mcpservers.VisibilityPrivate && toolset.ExternalOauthServerID.Valid {
		return hostedServerWrite{}, oops.E(oops.CodeInvalid, nil, "remove the toolset's external OAuth server before changing public visibility")
	}

	storedMode, storedErr := networkaccess.Effective(canonical.NetworkAccessMode)
	if storedErr != nil && change.NetworkAccessMode == nil {
		return hostedServerWrite{}, oops.E(oops.CodeUnexpected, storedErr, "invalid stored hosted MCP network mode")
	}
	mode := storedMode
	modeStorage := canonical.NetworkAccessMode
	if change.NetworkAccessMode != nil {
		mode = *change.NetworkAccessMode
		modeStorage = networkaccess.Storage(mode)
	}
	// Private access needs a live ingress only when newly selected or newly served.
	if !mode.IsPublicOnly() && enabled && (storedErr != nil || mode != storedMode || canonical.Visibility == mcpservers.VisibilityDisabled) {
		if err := hostingstate.CheckPrivateIngress(ctx, tx, toolset.OrganizationID, endpoints); err != nil {
			return hostedServerWrite{}, err //nolint:wrapcheck // oops errors pass through.
		}
	}

	name := canonical.Name
	if change.Name != nil {
		name = pgtype.Text{String: *change.Name, Valid: true}
	}
	issuer := canonical.UserSessionIssuerID
	switch {
	case change.ClearUserSessionIssuer:
		if canonical.UserSessionIssuerID.Valid {
			if err := lifecycle.GuardServerIssuerEMABindings(ctx, tx, toolset.OrganizationID, toolset.ProjectID, canonical.UserSessionIssuerID.UUID); err != nil {
				return hostedServerWrite{}, err //nolint:wrapcheck // oops errors pass through.
			}
		}
		issuer = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	case change.UserSessionIssuerID.Valid:
		issuer = change.UserSessionIssuerID
	}
	variationsGroup := canonical.ToolVariationsGroupID
	if change.ToolVariationsGroupID != nil {
		variationsGroup = *change.ToolVariationsGroupID
	}

	result := hostedServerWrite{Before: canonical, After: canonical, ClearedRootDomainIDs: nil}
	if name != canonical.Name || visibility != canonical.Visibility || issuer != canonical.UserSessionIssuerID ||
		variationsGroup != canonical.ToolVariationsGroupID || modeStorage != canonical.NetworkAccessMode {
		result.After, err = servers.SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
			Name: name, Slug: canonical.Slug, Visibility: visibility, UserSessionIssuerID: issuer,
			ToolVariationsGroupID: variationsGroup, NetworkAccessMode: modeStorage, ID: canonical.ID, ProjectID: canonical.ProjectID,
		})
		if err != nil {
			return hostedServerWrite{}, oops.E(oops.CodeUnexpected, err, "update hosted MCP server")
		}
		if err := hostedmcp.LogUpdate(ctx, tx, auditLogger, actor, toolset.OrganizationID, canonical, result.After); err != nil {
			return hostedServerWrite{}, err //nolint:wrapcheck // oops errors pass through.
		}
		if err := hostedmcp.SyncDisplayName(ctx, tx, canonical, result.After); err != nil {
			return hostedServerWrite{}, err //nolint:wrapcheck // oops errors pass through.
		}
	}
	if issuer != canonical.UserSessionIssuerID {
		touched := make([]uuid.UUID, 0, 2)
		for _, id := range []uuid.NullUUID{canonical.UserSessionIssuerID, issuer} {
			if id.Valid {
				touched = append(touched, id.UUID)
			}
		}
		if err := remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, tx, toolset.OrganizationID, toolset.ProjectID, touched); err != nil {
			return hostedServerWrite{}, oops.E(oops.CodeUnexpected, err, "resync hosted MCP remote session issuer")
		}
		result.After, err = servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: canonical.ID, ProjectID: canonical.ProjectID})
		if err != nil {
			return hostedServerWrite{}, oops.E(oops.CodeUnexpected, err, "reload hosted MCP server")
		}
	}

	// A disabled server holds no domain root.
	if !enabled && slices.ContainsFunc(endpoints, func(endpoint mcpendpointsrepo.McpEndpoint) bool {
		return endpoint.IsDomainRoot.Valid && endpoint.IsDomainRoot.Bool
	}) {
		actorPrincipal, ok := actor.Principal(ctx)
		if !ok {
			return hostedServerWrite{}, oops.E(oops.CodeUnauthorized, nil, "missing hosted MCP actor")
		}
		cleared, err := endpointRepo.ClearRootMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ClearRootMCPEndpointsByMCPServerIDParams{McpServerID: toolset.ID, ProjectID: toolset.ProjectID})
		if err != nil {
			return hostedServerWrite{}, oops.E(oops.CodeUnexpected, err, "clear hosted MCP root endpoints")
		}
		if err := tombstone.LogRootAutoClears(ctx, tx, auditLogger, toolset.OrganizationID, actorPrincipal, actor.Email, cleared); err != nil {
			return hostedServerWrite{}, oops.E(oops.CodeUnexpected, err, "audit hosted MCP root cleanup")
		}
		result.ClearedRootDomainIDs = tombstone.RootDomainIDs(cleared)
	}
	if err := hostingstate.MirrorToToolset(ctx, tx, toolset.ProjectID, toolset.ID); err != nil {
		return hostedServerWrite{}, err //nolint:wrapcheck // oops errors pass through.
	}
	return result, nil
}
