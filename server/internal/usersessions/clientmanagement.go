package usersessions

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// operatorAuthMethods are the token_endpoint_auth_method values an operator
// may register a client with. Operator registration always mints a secret, so
// only the symmetric methods apply.
var operatorAuthMethods = []string{oauthwire.AuthMethodClientSecretBasic, oauthwire.AuthMethodClientSecretPost}

// requireIssuerOwnerWrite gates a change to clients of issuer on whoever owns
// the issuer: project:write on its project for a project-owned issuer, and
// org:admin for an organization-owned one. An organization-owned issuer is
// shared by every project in the organization, so project:write on the
// caller's project is not enough to change who can sign in through it.
func (s *Service) requireIssuerOwnerWrite(ctx context.Context, authCtx *contextvalues.AuthContext, issuer repo.UserSessionIssuer) error {
	if issuer.ProjectID.Valid {
		return s.authz.Require(ctx, authz.Project(issuer.ProjectID.UUID).Write())
	}

	if authCtx.APIKeyID != "" {
		return oops.E(oops.CodeForbidden, nil, "clients of an organization user session issuer can only be managed from a user session")
	}
	return s.authz.Require(ctx, authz.Org(authCtx.ActiveOrganizationID).Admin())
}

// auditActor is the request's authenticated actor, so a mutation made with an
// API key is attributed to that key rather than to an empty user. Only a user
// actor carries the session email as its display name.
func auditActor(ctx context.Context, authCtx *contextvalues.AuthContext) (urn.Principal, *string, error) {
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok {
		if authCtx.UserID == "" {
			return urn.Principal{}, nil, oops.C(oops.CodeUnauthorized)
		}
		actor = urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	}
	if actor.Type != urn.PrincipalTypeUser {
		return actor, nil, nil
	}
	return actor, authCtx.Email, nil
}

// validateRedirectFragments rejects a redirect URI carrying a fragment, which
// RFC 6749 §3.1.2 forbids. The shared validator does not check it, and these
// endpoints are new, so they hold registrations to the spec from the start.
func validateRedirectFragments(uris []string) error {
	for _, u := range uris {
		if strings.Contains(u, "#") {
			return &oauthwire.Error{Code: "invalid_redirect_uri", Description: "redirect_uri must not include a fragment"}
		}
	}
	return nil
}

// CreateUserSessionClient registers a confidential client on an operator's
// behalf. Speakeasy mints the client_id and secret; the secret is returned
// once and only its bcrypt hash is stored, exactly as /register does.
func (s *Service) CreateUserSessionClient(ctx context.Context, payload *gen.CreateUserSessionClientPayload) (*gen.CreateUserSessionClientResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	issuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogError(ctx, s.logger)
	}

	logger := s.logger.With(
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogUserSessionIssuerID(issuerID.String()),
	)

	issuer, err := repo.New(s.db).GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
		ID:             issuerID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}

	if err := s.requireIssuerOwnerWrite(ctx, authCtx, issuer); err != nil {
		return nil, err
	}

	// The same RFC 7591 rules /register applies, so an operator-registered
	// client can never hold metadata a self-registered one could not.
	req := RegistrationRequest{
		ClientName:              payload.ClientName,
		RedirectURIs:            payload.RedirectUris,
		GrantTypes:              nil,
		ResponseTypes:           nil,
		TokenEndpointAuthMethod: payload.TokenEndpointAuthMethod,
		JWKS:                    nil,
		JWKSURI:                 "",
	}
	req.SetDefaults()
	err = req.Validate(RegistrableGrantTypes, operatorAuthMethods)
	if err == nil {
		err = validateRedirectFragments(req.RedirectURIs)
	}
	if err != nil {
		if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
			return nil, oops.E(oops.CodeBadRequest, err, "%s", oauthErr.Description).LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "validate client registration").LogError(ctx, logger)
	}

	secret, secretHash, err := MintClientSecret()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "mint client secret").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateUserSessionClient(ctx, repo.CreateUserSessionClientParams{
		UserSessionIssuerID: issuer.ID,
		ClientID:            "client_" + uuid.NewString(),
		ClientSecretHash:    pgtype.Text{String: secretHash, Valid: true},
		ClientName:          req.ClientName,
		RedirectUris:        req.RedirectURIs,
		// NULL means the secret does not expire, as for DCR clients.
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
		ClientJwks:              nil,
		ClientJwksUri:           pgtype.Text{String: "", Valid: false},
	})
	if err != nil {
		// The insert re-reads the issuer under lock, so an issuer deleted
		// since the read above lands here rather than on a tombstone.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create user session client").LogError(ctx, logger)
	}

	view := mv.BuildUserSessionClientView(row, 0)

	actor, actorDisplayName, err := auditActor(ctx, authCtx)
	if err != nil {
		return nil, err
	}

	if err := s.audit.LogUserSessionClientCreate(ctx, dbtx, audit.LogUserSessionClientCreateEvent{
		OrganizationID:                 authCtx.ActiveOrganizationID,
		ProjectID:                      issuer.ProjectID.UUID,
		Actor:                          actor,
		ActorDisplayName:               actorDisplayName,
		ActorSlug:                      nil,
		UserSessionClientURN:           urn.NewUserSessionClient(row.ID),
		ClientID:                       row.ClientID,
		ClientName:                     row.ClientName,
		UserSessionClientSnapshotAfter: view,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log user session client creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return &gen.CreateUserSessionClientResult{
		Client:       view,
		ClientSecret: secret,
	}, nil
}

// UpdateUserSessionClientRedirectURIs replaces a client's redirect URIs.
// Allowed for operator-registered and DCR clients; refused for CIMD clients,
// whose metadata document is the source of their redirect URIs.
func (s *Service) UpdateUserSessionClientRedirectURIs(ctx context.Context, payload *gen.UpdateUserSessionClientRedirectURIsPayload) (*types.UserSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid client id").LogError(ctx, s.logger)
	}

	logger := s.logger.With(
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogUserSessionClientID(id.String()),
	)

	queries := repo.New(s.db)

	before, err := queries.GetUserSessionClientByID(ctx, repo.GetUserSessionClientByIDParams{
		ID:             id,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session client").LogError(ctx, logger)
	}

	issuer, err := queries.GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
		ID:             before.UserSessionIssuerID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}

	if err := s.requireIssuerOwnerWrite(ctx, authCtx, issuer); err != nil {
		return nil, err
	}

	if before.ClientIDMetadataUri.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "redirect URIs of a CIMD client come from its metadata document and cannot be edited").LogError(ctx, logger)
	}

	for _, u := range payload.RedirectUris {
		err := oauthwire.ValidateRedirectURI(u)
		if err == nil {
			err = validateRedirectFragments([]string{u})
		}
		if err != nil {
			if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
				return nil, oops.E(oops.CodeBadRequest, err, "%s", oauthErr.Description).LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "validate redirect uri").LogError(ctx, logger)
		}
	}

	counts, err := activeSessionCounts(ctx, queries, []uuid.UUID{before.ID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count active user sessions").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	after, err := repo.New(dbtx).UpdateUserSessionClientRedirectURIs(ctx, repo.UpdateUserSessionClientRedirectURIsParams{
		RedirectUris:   payload.RedirectUris,
		ID:             before.ID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Revoked between the read and this write.
			return nil, oops.E(oops.CodeNotFound, err, "user session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update user session client redirect uris").LogError(ctx, logger)
	}

	beforeView := mv.BuildUserSessionClientView(before, counts[before.ID])
	afterView := mv.BuildUserSessionClientView(after, counts[after.ID])

	actor, actorDisplayName, err := auditActor(ctx, authCtx)
	if err != nil {
		return nil, err
	}

	if err := s.audit.LogUserSessionClientUpdateRedirectURIs(ctx, dbtx, audit.LogUserSessionClientUpdateRedirectURIsEvent{
		OrganizationID:                  authCtx.ActiveOrganizationID,
		ProjectID:                       issuer.ProjectID.UUID,
		Actor:                           actor,
		ActorDisplayName:                actorDisplayName,
		ActorSlug:                       nil,
		UserSessionClientURN:            urn.NewUserSessionClient(after.ID),
		ClientID:                        after.ClientID,
		ClientName:                      after.ClientName,
		UserSessionClientSnapshotBefore: beforeView,
		UserSessionClientSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log user session client redirect uri update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}
