package usersessions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// GetAuthenticationHostImpact previews moving an issuer onto or off the
// authentication host.
func (s *Service) GetAuthenticationHostImpact(ctx context.Context, payload *orggen.GetAuthenticationHostImpactPayload) (*orggen.UserSessionIssuerAuthenticationHostImpact, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogWarn(ctx, logger)
	}

	q := repo.New(s.db)
	row, err := q.GetOrganizationManagedUserSessionIssuerWithProjectByID(ctx, repo.GetOrganizationManagedUserSessionIssuerWithProjectByIDParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization user session issuer").LogError(ctx, logger)
	}
	servers, err := q.CountOrganizationManagedUserSessionIssuerMCPServers(ctx, repo.CountOrganizationManagedUserSessionIssuerMCPServersParams{
		UserSessionIssuerID: id,
		OrganizationID:      authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count user session issuer MCP servers").LogError(ctx, logger)
	}

	before := s.signInLocation(ctx, row.UserSessionIssuer)
	after := s.signInLocation(ctx, s.onAuthenticationHost(row.UserSessionIssuer, payload.UseAuthenticationHost))

	return &orggen.UserSessionIssuerAuthenticationHostImpact{
		Issuer:                    s.organizationIssuerView(ctx, row.UserSessionIssuer, row.ProjectName),
		AuthorizationServerBefore: before,
		AuthorizationServerAfter:  after,
		Changes:                   before != after,
		McpServerCount:            int(servers),
	}, nil
}

// SetAuthenticationHost moves an issuer onto or off the authentication host.
func (s *Service) SetAuthenticationHost(ctx context.Context, payload *orggen.SetAuthenticationHostPayload) (*types.UserSessionIssuer, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogWarn(ctx, logger)
	}
	if !s.sharedHosts.AuthenticationHostAvailable() {
		return nil, oops.E(oops.CodeBadRequest, nil, "this deployment has no authentication host").LogWarn(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	q := repo.New(dbtx)

	existing, err := q.GetOrganizationManagedUserSessionIssuerByIDForUpdate(ctx, repo.GetOrganizationManagedUserSessionIssuerByIDForUpdateParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock user session issuer").LogError(ctx, logger)
	}
	// Speakeasy provisions and maintains these, so their servers' sign-in is
	// not the organization's to move.
	if existing.Classification != "custom" {
		return nil, oops.E(oops.CodeConflict, nil, "project-default identity providers cannot be moved").LogWarn(ctx, logger)
	}
	owned, err := q.UserSessionIssuerIsPlatformOwned(ctx, repo.UserSessionIssuerIsPlatformOwnedParams{
		UserSessionIssuerID: conv.ToNullUUID(id),
		OrganizationID:      authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check Platform MCP issuer ownership").LogError(ctx, logger)
	}
	if owned {
		return nil, oops.E(oops.CodeConflict, nil, "Platform MCP catalog-owned issuers cannot be moved").LogWarn(ctx, logger)
	}

	moved := s.onAuthenticationHost(existing, payload.UseAuthenticationHost)
	if moved.UseAuthenticationHost == existing.UseAuthenticationHost && moved.PinnedIssuerUrl == existing.PinnedIssuerUrl {
		return s.organizationIssuerResponse(ctx, q, existing.ID, authCtx.ActiveOrganizationID)
	}

	updated, err := q.SetOrganizationManagedUserSessionIssuerAuthenticationHost(ctx, repo.SetOrganizationManagedUserSessionIssuerAuthenticationHostParams{
		UseAuthenticationHost: moved.UseAuthenticationHost,
		PinnedIssuerUrl:       moved.PinnedIssuerUrl,
		ID:                    id,
		OrganizationID:        authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "move user session issuer authentication host").LogError(ctx, logger)
	}

	if err := s.audit.LogUserSessionIssuerUpdate(ctx, dbtx, audit.LogUserSessionIssuerUpdateEvent{
		OrganizationID:                  authCtx.ActiveOrganizationID,
		ProjectID:                       projectIDValue(updated.ProjectID),
		Actor:                           urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                authCtx.Email,
		ActorSlug:                       nil,
		UserSessionIssuerURN:            urn.NewUserSessionIssuer(updated.ID),
		Slug:                            updated.Slug,
		UserSessionIssuerSnapshotBefore: UserSessionIssuerView(existing),
		UserSessionIssuerSnapshotAfter:  UserSessionIssuerView(updated),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log user session issuer authentication host move").LogError(ctx, logger)
	}

	view, err := s.organizationIssuerResponse(ctx, q, updated.ID, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return view, nil
}

// onAuthenticationHost is issuer as moving it onto or off the authentication
// host leaves it. A pinned shared authorization server is re-pinned to the
// matching host; otherwise the move takes effect through the flag alone.
func (s *Service) onAuthenticationHost(issuer repo.UserSessionIssuer, on bool) repo.UserSessionIssuer {
	moved := issuer
	moved.UseAuthenticationHost = on
	if authserver.IssuerURLPinned(issuer) {
		moved.PinnedIssuerUrl = conv.ToPGText(s.sharedHosts.SharedIssuerURLOn(issuer.ID, on))
	}
	return moved
}

// signInLocation is where an issuer's clients sign in: its shared
// authorization server's issuer URL, empty when it is not served, or else the
// origin that announces each MCP server's own authorization server.
func (s *Service) signInLocation(ctx context.Context, issuer repo.UserSessionIssuer) string {
	if !authserver.IssuerInSharedMode(issuer) {
		return s.sharedHosts.PerServerOrigin(issuer.UseAuthenticationHost)
	}
	return conv.PtrValOr(s.issuerView(ctx, issuer).IssuerURL, "")
}

// organizationIssuerResponse is the organization-level view of an issuer,
// naming a project-specific issuer's project.
func (s *Service) organizationIssuerResponse(ctx context.Context, q *repo.Queries, id uuid.UUID, organizationID string) (*types.UserSessionIssuer, error) {
	row, err := q.GetOrganizationManagedUserSessionIssuerWithProjectByID(ctx, repo.GetOrganizationManagedUserSessionIssuerWithProjectByIDParams{
		ID:             id,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read user session issuer").LogError(ctx, s.logger)
	}
	return s.organizationIssuerView(ctx, row.UserSessionIssuer, row.ProjectName), nil
}
