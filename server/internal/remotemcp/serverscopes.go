package remotemcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	maxPinnedScopes     = 100
	maxPinnedScopeBytes = 256
)

// pinnableServer is a remote-backed MCP server and the protected resource its logins are for.
type pinnableServer struct {
	server      mcpserversrepo.McpServer
	resourceURL string
}

func (s *Service) GetServerScopes(ctx context.Context, payload *gen.GetServerScopesPayload) (*gen.RemoteMcpServerScopes, error) {
	authCtx, logger, mcpServerID, err := s.authorizeServerScopes(ctx, payload.McpServerID)
	if err != nil {
		return nil, err
	}
	target, err := s.loadPinnableServer(ctx, s.db, logger, *authCtx.ProjectID, mcpServerID)
	if err != nil {
		return nil, err
	}
	sharing, err := s.authorizeSharingServers(ctx, s.db, logger, authCtx, target)
	if err != nil {
		return nil, err
	}
	return s.serverScopes(ctx, logger, authCtx, target, sharedServerCount(sharing, target))
}

func (s *Service) SetServerScopePin(ctx context.Context, payload *gen.SetServerScopePinPayload) (*gen.RemoteMcpServerScopes, error) {
	authCtx, logger, mcpServerID, err := s.authorizeServerScopes(ctx, payload.McpServerID)
	if err != nil {
		return nil, err
	}
	scopes, err := normalizePinnedScopes(payload.Scopes)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error())
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	target, err := s.loadPinnableServer(ctx, dbtx, logger, *authCtx.ProjectID, mcpServerID)
	if err != nil {
		return nil, err
	}
	q := repo.New(dbtx)
	if err := q.AcquireRemoteProtectedResourceLock(ctx, repo.AcquireRemoteProtectedResourceLockParams{ProjectID: *authCtx.ProjectID, ResourceIdentifier: target.resourceURL}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock protected resource").LogError(ctx, logger)
	}
	// Authorized under the lock, so concurrent pin writes see one sharing set.
	sharing, err := s.authorizeSharingServers(ctx, dbtx, logger, authCtx, target)
	if err != nil {
		return nil, err
	}
	// After every authorization, so the refusal reveals the flag only to a caller allowed to write.
	// Logins ignore the pin without discovery, so it may only be cleared.
	if len(scopes) > 0 && !remotesessions.ResourceScopeDiscoveryEnabled(ctx, logger, s.features, authCtx.ActiveOrganizationID, authCtx.OrganizationSlug) {
		return nil, oops.E(oops.CodeBadRequest, nil, "pinned scopes are not enabled for this organization; a pin can only be cleared")
	}
	var before []string
	existing, err := q.GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: *authCtx.ProjectID, ResourceIdentifier: target.resourceURL})
	switch {
	case err == nil:
		before = existing.ScopeOverride
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return nil, oops.E(oops.CodeUnexpected, err, "get protected resource").LogError(ctx, logger)
	}

	if !slices.Equal(before, scopes) {
		row, err := q.UpsertRemoteProtectedResourceScopeOverride(ctx, repo.UpsertRemoteProtectedResourceScopeOverrideParams{
			ProjectID:          *authCtx.ProjectID,
			OrganizationID:     authCtx.ActiveOrganizationID,
			ResourceIdentifier: target.resourceURL,
			ScopeOverride:      scopes,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "pin protected resource scopes").LogError(ctx, logger)
		}

		affected := make([]string, 0, len(sharing))
		for _, id := range sharing {
			affected = append(affected, id.String())
		}
		if err := s.audit.LogMcpServerScopePinUpdate(ctx, dbtx, audit.LogMcpServerScopePinUpdateEvent{
			OrganizationID:       authCtx.ActiveOrganizationID,
			ProjectID:            *authCtx.ProjectID,
			Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:     authCtx.Email,
			ActorSlug:            nil,
			McpServerURN:         urn.NewMcpServer(target.server.ID),
			McpServerName:        conv.FromPGTextOrEmpty[string](target.server.Name),
			McpServerSlug:        conv.FromPGTextOrEmpty[string](target.server.Slug),
			ResourceURL:          target.resourceURL,
			AffectedMcpServerIDs: affected,
			ScopesBefore:         before,
			ScopesAfter:          row.ScopeOverride,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log scope pin update").LogError(ctx, logger)
		}
	}

	// Release the resource lock and connection even for a no-op before the
	// response queries the pool; waiting until the deferred rollback can deadlock
	// when other requests occupy the remaining connections waiting on this lock.
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return s.serverScopes(ctx, logger, authCtx, target, sharedServerCount(sharing, target))
}

// authorizeServerScopes requires mcp:write on the server: the read reveals connection config.
func (s *Service) authorizeServerScopes(ctx context.Context, rawID string) (*contextvalues.AuthContext, *slog.Logger, uuid.UUID, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, nil, uuid.Nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))
	mcpServerID, err := uuid.Parse(rawID)
	if err != nil {
		return nil, nil, uuid.Nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, mcpServerID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, nil, uuid.Nil, err
	}
	return authCtx, logger, mcpServerID, nil
}

// authorizeSharingServers requires mcp:write on every live server sharing the
// target's upstream URL: the pin is keyed by resource, so it applies to all of them.
func (s *Service) authorizeSharingServers(ctx context.Context, db repo.DBTX, logger *slog.Logger, authCtx *contextvalues.AuthContext, target pinnableServer) ([]uuid.UUID, error) {
	sharing, err := repo.New(db).ListMcpServerIDsByRemoteURL(ctx, repo.ListMcpServerIDsByRemoteURLParams{ProjectID: *authCtx.ProjectID, Url: target.resourceURL})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers sharing the protected resource").LogError(ctx, logger)
	}
	checks := make([]authz.Check, 0, len(sharing))
	for _, id := range sharing {
		if id == target.server.ID {
			continue
		}
		checks = append(checks, authz.MCPCheck(authz.ScopeMCPWrite, id.String(), authCtx.ProjectID.String()))
	}
	if len(checks) > 0 {
		if err := s.authz.Require(ctx, checks...); err != nil {
			return nil, err
		}
	}
	return sharing, nil
}

func sharedServerCount(sharing []uuid.UUID, target pinnableServer) int {
	n := 0
	for _, id := range sharing {
		if id != target.server.ID {
			n++
		}
	}
	return n
}

func (s *Service) loadPinnableServer(ctx context.Context, db remotesessionsrepo.DBTX, logger *slog.Logger, projectID, mcpServerID uuid.UUID) (pinnableServer, error) {
	server, err := mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: mcpServerID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return pinnableServer{}, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
	}
	if err != nil {
		return pinnableServer{}, oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, logger)
	}
	resourceURL, err := remotesessionsrepo.New(db).GetRemoteURLForMcpServer(ctx, remotesessionsrepo.GetRemoteURLForMcpServerParams{McpServerID: mcpServerID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return pinnableServer{}, oops.E(oops.CodeNotFound, err, "mcp server is not backed by a remote mcp server").LogError(ctx, logger)
	}
	if err != nil {
		return pinnableServer{}, oops.E(oops.CodeUnexpected, err, "get remote url for mcp server").LogError(ctx, logger)
	}
	return pinnableServer{server: server, resourceURL: resourceURL}, nil
}

// serverScopes resolves what a login through each bound client would request
// now from the cached resource row, as the consent card does; it never probes.
func (s *Service) serverScopes(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, target pinnableServer, sharedServers int) (*gen.RemoteMcpServerScopes, error) {
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	discover := remotesessions.ResourceScopeDiscoveryEnabled(ctx, logger, s.features, orgID, authCtx.OrganizationSlug)

	cached := remotesessions.ResourceScopes{Pin: nil, ChallengeScopes: nil, ScopesSupported: nil, Live: false, UseDiscovered: discover}
	row, err := repo.New(s.db).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: projectID, ResourceIdentifier: target.resourceURL})
	switch {
	case err == nil:
		cached.Pin = row.ScopeOverride
		cached.ChallengeScopes = row.ChallengeScopes
		cached.ScopesSupported = protectedresource.LastGoodScopes(&row, time.Now())
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return nil, oops.E(oops.CodeUnexpected, err, "get protected resource").LogError(ctx, logger)
	}

	clients := []*gen.RemoteMcpServerClientScopes{}
	if target.server.UserSessionIssuerID.Valid {
		clients, err = s.clientScopes(ctx, projectID, orgID, target, cached, discover)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "resolve client scopes").LogError(ctx, logger)
		}
	}

	return &gen.RemoteMcpServerScopes{
		ResourceURL:           target.resourceURL,
		PinnedScopes:          conv.DefaultSlice(cached.Pin, []string{}),
		AdvertisedScopesKnown: cached.ScopesSupported != nil,
		AdvertisedScopes:      cached.ScopesSupported,
		ChallengeScopes:       conv.DefaultSlice(cached.ChallengeScopes, []string{}),
		DiscoveryEnabled:      discover,
		Clients:               clients,
		SharedServerCount:     sharedServers,
	}, nil
}

func (s *Service) clientScopes(ctx context.Context, projectID uuid.UUID, orgID string, target pinnableServer, cached remotesessions.ResourceScopes, discover bool) ([]*gen.RemoteMcpServerClientScopes, error) {
	issuerID := target.server.UserSessionIssuerID.UUID
	rows, err := remotesessionsrepo.New(s.db).ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: issuerID,
		ProjectID:           conv.ToNullUUID(projectID),
		OrganizationID:      conv.ToPGText(orgID),
	})
	if err != nil {
		return nil, fmt.Errorf("list remote session clients: %w", err)
	}
	var owners map[uuid.UUID]bool
	if discover && len(rows) > 0 {
		owners, err = remotesessions.ResourceOwners(ctx, s.db, projectID, orgID, issuerID, target.resourceURL)
		if err != nil {
			return nil, fmt.Errorf("decide resource ownership: %w", err)
		}
	}
	out := make([]*gen.RemoteMcpServerClientScopes, 0, len(rows))
	for _, r := range rows {
		resource := remotesessions.ResourceScopes{Pin: nil, ChallengeScopes: nil, ScopesSupported: nil, Live: false, UseDiscovered: discover}
		if owners[r.ClientID] {
			resource = cached
		}
		resolved := (remotesessions.Client{ //nolint:exhaustruct // Only scope inputs are used by RequestedScopes.
			ID:                      r.ClientID,
			ClientScope:             r.ClientScope,
			IssuerScopesSupported:   r.ScopesSupported,
			IssuerScopeOverride:     r.ScopeOverride,
			IssuerOmitScopeFallback: r.OmitScopeFallback.Valid && r.OmitScopeFallback.Bool,
		}).RequestedScopes(resource)
		out = append(out, &gen.RemoteMcpServerClientScopes{
			ClientID:                 r.ClientID.String(),
			ScopeSource:              string(resolved.Source),
			RequestedScopes:          conv.DefaultSlice(resolved.Scopes, []string{}),
			UnadvertisedPinnedScopes: conv.DefaultSlice(resolved.Unadvertised, []string{}),
			PinWouldDecide:           discover && owners[r.ClientID] && len(r.ClientScope) == 0 && len(cached.ChallengeScopes) == 0,
		})
	}
	return out, nil
}

// normalizePinnedScopes trims, drops blanks and duplicates (keeping order), and
// rejects anything that is not an RFC 6749 §3.3 scope-token.
func normalizePinnedScopes(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	for _, scope := range raw {
		scope = strings.TrimSpace(scope)
		if scope == "" || slices.Contains(out, scope) {
			continue
		}
		if len(scope) > maxPinnedScopeBytes {
			return nil, fmt.Errorf("scope %.32q… exceeds %d characters", scope, maxPinnedScopeBytes)
		}
		for i := range len(scope) {
			if c := scope[i]; c < 0x21 || c > 0x7e || c == '"' || c == '\\' {
				return nil, fmt.Errorf("scope %q contains a character not allowed in an OAuth scope", scope)
			}
		}
		out = append(out, scope)
		if len(out) > maxPinnedScopes {
			return nil, fmt.Errorf("at most %d scopes can be pinned", maxPinnedScopes)
		}
	}
	return out, nil
}
