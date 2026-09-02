// Package hostedmcp mirrors hosting state between toolsets and their mcp_servers wrapper.
package hostedmcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/mv"
	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// MaxToolsetSlugLength is the toolsets.mcp_slug CHECK bound.
const MaxToolsetSlugLength = 60

// ErrAddressTaken reports a (custom_domain_id, slug) held by another server or toolset.
var ErrAddressTaken = errors.New("mcp address already taken")

// ClaimAddress takes the slug-scope advisory lock and runs the unified
// availability check for one address a toolset and its wrapper are about to
// hold, so every mirror write claims addresses the way API writers do.
func ClaimAddress(ctx context.Context, db mcpendpointsrepo.DBTX, organizationID string, toolsetID, serverID uuid.UUID, customDomainID uuid.NullUUID, slug string) error {
	q := mcpendpointsrepo.New(db)
	if err := q.LockSlugScope(ctx, mcpendpointsrepo.LockSlugScopeParams{CustomDomainID: customDomainID, Slug: slug}); err != nil {
		return fmt.Errorf("lock mcp slug scope: %w", err)
	}
	available, err := q.CheckUnifiedSlugAvailability(ctx, mcpendpointsrepo.CheckUnifiedSlugAvailabilityParams{
		SkipDomainCheck:    false,
		CustomDomainID:     customDomainID,
		OrganizationID:     organizationID,
		Slug:               slug,
		ExcludeToolsetID:   uuid.NullUUID{UUID: toolsetID, Valid: true},
		ExcludeMcpServerID: uuid.NullUUID{UUID: serverID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("check unified mcp slug availability: %w", err)
	}
	if !available.Valid || !available.Bool {
		return ErrAddressTaken
	}
	return nil
}

// VisibilityForToolset maps the toolset flag pair onto mcp_servers.visibility.
func VisibilityForToolset(mcpEnabled, mcpIsPublic bool) string {
	switch {
	case !mcpEnabled:
		return visibility.Disabled
	case mcpIsPublic:
		return visibility.Public
	default:
		return visibility.Private
	}
}

// ToolsetFlags maps a wrapper visibility back onto (mcp_enabled, mcp_is_public).
func ToolsetFlags(serverVisibility string) (mcpEnabled, mcpIsPublic bool) {
	switch serverVisibility {
	case visibility.Public:
		return true, true
	case visibility.Private:
		return true, false
	default:
		return false, false
	}
}

// LockToolsets locks live toolsets in id order. The toolset lock precedes every
// domain, server, and endpoint lock so both mirror directions serialize.
func LockToolsets(ctx context.Context, db toolsetsrepo.DBTX, projectID uuid.UUID, ids ...uuid.NullUUID) error {
	valid := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id.Valid {
			valid = append(valid, id.UUID)
		}
	}
	slices.SortFunc(valid, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, id := range slices.Compact(valid) {
		if _, err := toolsetsrepo.New(db).LockToolsetByID(ctx, toolsetsrepo.LockToolsetByIDParams{ID: id, ProjectID: projectID}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("lock toolset %s: %w", id, err)
		}
	}
	return nil
}

// Mirror projects wrapper-side writes back onto the toolset, audited under the acting user.
type Mirror struct {
	Audit       *audit.Logger
	ActorUserID string
	ActorEmail  *string
}

// SyncToolsetFromWrapper writes a toolset-backed wrapper's visibility, issuer,
// and tool variations group onto its toolset.
func (m Mirror) SyncToolsetFromWrapper(ctx context.Context, tx pgx.Tx, server mcpserversrepo.McpServer) error {
	q := toolsetsrepo.New(tx)
	before, err := q.LockToolsetByID(ctx, toolsetsrepo.LockToolsetByIDParams{ID: server.ToolsetID.UUID, ProjectID: server.ProjectID})
	if err != nil {
		return fmt.Errorf("lock toolset: %w", err)
	}
	enabled, public := ToolsetFlags(server.Visibility)
	if err := m.detachOAuthOnFlip(ctx, tx, before, public); err != nil {
		return err
	}
	after, err := q.SyncToolsetHostingFromWrapper(ctx, toolsetsrepo.SyncToolsetHostingFromWrapperParams{
		McpEnabled:            enabled,
		McpIsPublic:           public,
		UserSessionIssuerID:   server.UserSessionIssuerID,
		ToolVariationsGroupID: server.ToolVariationsGroupID,
		ID:                    before.ID,
		ProjectID:             before.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("sync toolset hosting from mcp server: %w", err)
	}
	return m.logToolsetUpdate(ctx, tx, after)
}

// SetToolsetAddress writes the wrapper's primary endpoint address onto the
// toolset; a null slug, or one the toolsets.mcp_slug CHECK cannot hold, means
// the wrapper has no address the toolset can carry.
func (m Mirror) SetToolsetAddress(ctx context.Context, tx pgx.Tx, projectID, toolsetID uuid.UUID, slug pgtype.Text, customDomainID uuid.NullUUID) error {
	if slug.Valid && len(slug.String) > MaxToolsetSlugLength {
		slug = pgtype.Text{String: "", Valid: false}
		customDomainID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	after, err := toolsetsrepo.New(tx).SetToolsetHostedAddress(ctx, toolsetsrepo.SetToolsetHostedAddressParams{
		McpSlug:        slug,
		CustomDomainID: customDomainID,
		ID:             toolsetID,
		ProjectID:      projectID,
	})
	if err != nil {
		return fmt.Errorf("set toolset hosted address: %w", err)
	}
	return m.logToolsetUpdate(ctx, tx, after)
}

// ClearToolsetHosting removes every hosting column from a toolset whose wrapper
// was deleted; the toolset and its issuer row stay.
func (m Mirror) ClearToolsetHosting(ctx context.Context, tx pgx.Tx, projectID, toolsetID uuid.UUID) error {
	q := toolsetsrepo.New(tx)
	before, err := q.LockToolsetByID(ctx, toolsetsrepo.LockToolsetByIDParams{ID: toolsetID, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("lock toolset: %w", err)
	}
	if err := m.detachOAuthOnFlip(ctx, tx, before, false); err != nil {
		return err
	}
	after, err := q.ClearToolsetHosting(ctx, toolsetsrepo.ClearToolsetHostingParams{ID: toolsetID, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("clear toolset hosting: %w", err)
	}
	return m.logToolsetUpdate(ctx, tx, after)
}

// EnableToolsetMCP enables MCP on an addressable toolset and brings its wrapper
// in line (visibility, issuer, variations group), the way attaching the toolset
// to an assistant needs. The wrapper is reconciled even when the toolset flag
// was already set, so a stale disabled wrapper never leaves the endpoint dark.
func (m Mirror) EnableToolsetMCP(ctx context.Context, tx pgx.Tx, projectID, toolsetID uuid.UUID) error {
	q := toolsetsrepo.New(tx)
	toolset, err := q.LockToolsetByID(ctx, toolsetsrepo.LockToolsetByIDParams{ID: toolsetID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("lock toolset: %w", err)
	case !toolset.McpSlug.Valid:
		return nil
	}
	if !toolset.McpEnabled {
		toolset, err = q.SyncToolsetHostingFromWrapper(ctx, toolsetsrepo.SyncToolsetHostingFromWrapperParams{
			McpEnabled:            true,
			McpIsPublic:           toolset.McpIsPublic,
			UserSessionIssuerID:   toolset.UserSessionIssuerID,
			ToolVariationsGroupID: toolset.ToolVariationsGroupID,
			ID:                    toolset.ID,
			ProjectID:             projectID,
		})
		if err != nil {
			return fmt.Errorf("enable toolset mcp: %w", err)
		}
		if err := m.logToolsetUpdate(ctx, tx, toolset); err != nil {
			return err
		}
	}

	servers := mcpserversrepo.New(tx)
	wrapper, err := servers.GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{ToolsetID: toolset.ID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("get mcp server for toolset: %w", err)
	}
	wantVisibility := VisibilityForToolset(true, toolset.McpIsPublic)
	if wrapper.Visibility == wantVisibility && wrapper.UserSessionIssuerID == toolset.UserSessionIssuerID && wrapper.ToolVariationsGroupID == toolset.ToolVariationsGroupID {
		return nil
	}
	locked, err := servers.LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: wrapper.ID, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("lock mcp server: %w", err)
	}
	updated, err := servers.UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
		Name:                  locked.Name,
		Slug:                  locked.Slug,
		EnvironmentID:         locked.EnvironmentID,
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     locked.RemoteMcpServerID,
		TunneledMcpServerID:   locked.TunneledMcpServerID,
		ToolsetID:             locked.ToolsetID,
		UnproxiedMcpServerID:  locked.UnproxiedMcpServerID,
		ToolVariationsGroupID: toolset.ToolVariationsGroupID,
		Visibility:            wantVisibility,
		ID:                    locked.ID,
		ProjectID:             projectID,
	})
	if err != nil {
		return fmt.Errorf("enable mcp server for toolset: %w", err)
	}
	if updated.UserSessionIssuerID != toolset.UserSessionIssuerID {
		updated, err = servers.SetMCPServerUserSessionIssuer(ctx, mcpserversrepo.SetMCPServerUserSessionIssuerParams{
			UserSessionIssuerID: toolset.UserSessionIssuerID,
			ID:                  updated.ID,
			ProjectID:           projectID,
		})
		if err != nil {
			return fmt.Errorf("set mcp server issuer for toolset: %w", err)
		}
	}
	if err := m.Audit.LogMcpServerUpdate(ctx, tx, audit.LogMcpServerUpdateEvent{
		OrganizationID:          toolset.OrganizationID,
		ProjectID:               projectID,
		Actor:                   m.actor(),
		ActorDisplayName:        m.ActorEmail,
		ActorSlug:               nil,
		McpServerURN:            urn.NewMcpServer(updated.ID),
		McpServerName:           conv.FromPGTextOrEmpty[string](updated.Name),
		McpServerSlug:           conv.FromPGTextOrEmpty[string](updated.Slug),
		McpServerSnapshotBefore: mv.BuildMcpServerView(locked),
		McpServerSnapshotAfter:  mv.BuildMcpServerView(updated),
	}); err != nil {
		return fmt.Errorf("audit mcp server enable: %w", err)
	}
	return nil
}

func (m Mirror) actor() urn.Principal {
	return urn.NewPrincipal(urn.PrincipalTypeUser, m.ActorUserID)
}

// detachOAuthOnFlip applies UpdateToolset's rule: a publicness change with an
// OAuth server attached detaches it.
func (m Mirror) detachOAuthOnFlip(ctx context.Context, tx pgx.Tx, toolset toolsetsrepo.Toolset, public bool) error {
	if toolset.McpIsPublic == public || (!toolset.ExternalOauthServerID.Valid && !toolset.OauthProxyServerID.Valid) {
		return nil
	}
	if _, err := toolsetsrepo.New(tx).ClearToolsetOAuthServers(ctx, toolsetsrepo.ClearToolsetOAuthServersParams{ProjectID: toolset.ProjectID, Slug: toolset.Slug}); err != nil {
		return fmt.Errorf("clear toolset oauth servers: %w", err)
	}
	if !toolset.ExternalOauthServerID.Valid {
		return nil
	}
	var slug *string
	meta, err := oauthrepo.New(tx).GetExternalOAuthServerMetadata(ctx, oauthrepo.GetExternalOAuthServerMetadataParams{ProjectID: toolset.ProjectID, ID: toolset.ExternalOauthServerID.UUID})
	switch {
	case err == nil:
		slug = &meta.Slug
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("get external oauth server: %w", err)
	}
	version, err := toolsetVersion(ctx, tx, toolset.ID)
	if err != nil {
		return err
	}
	if err := m.Audit.LogToolsetDetachExternalOAuth(ctx, tx, audit.LogToolsetDetachExternalOAuthEvent{
		OrganizationID:          toolset.OrganizationID,
		ProjectID:               toolset.ProjectID,
		Actor:                   m.actor(),
		ActorDisplayName:        m.ActorEmail,
		ActorSlug:               nil,
		ToolsetURN:              urn.NewToolset(toolset.ID),
		ToolsetName:             toolset.Name,
		ToolsetSlug:             toolset.Slug,
		ToolsetVersionAfter:     version,
		ExternalOAuthServerID:   new(toolset.ExternalOauthServerID.UUID.String()),
		ExternalOAuthServerSlug: slug,
	}); err != nil {
		return fmt.Errorf("audit toolset external oauth detach: %w", err)
	}
	return nil
}

func (m Mirror) logToolsetUpdate(ctx context.Context, tx pgx.Tx, toolset toolsetsrepo.Toolset) error {
	version, err := toolsetVersion(ctx, tx, toolset.ID)
	if err != nil {
		return err
	}
	if err := m.Audit.LogToolsetUpdate(ctx, tx, audit.LogToolsetUpdateEvent{
		OrganizationID:        toolset.OrganizationID,
		ProjectID:             toolset.ProjectID,
		Actor:                 m.actor(),
		ActorDisplayName:      m.ActorEmail,
		ActorSlug:             nil,
		ToolsetURN:            urn.NewToolset(toolset.ID),
		ToolsetName:           toolset.Name,
		ToolsetSlug:           toolset.Slug,
		ToolsetVersionAfter:   version,
		ToolsetSnapshotBefore: nil,
		ToolsetSnapshotAfter:  nil,
	}); err != nil {
		return fmt.Errorf("audit toolset hosting update: %w", err)
	}
	return nil
}

func toolsetVersion(ctx context.Context, tx pgx.Tx, toolsetID uuid.UUID) (int64, error) {
	latest, err := toolsetsrepo.New(tx).GetLatestToolsetVersion(ctx, toolsetID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("get latest toolset version: %w", err)
	}
	return latest.Version, nil
}
