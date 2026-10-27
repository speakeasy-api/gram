package usersessions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// TrustedIdentityProviderConfigured reports whether the token endpoint would
// find a usable trusted issuer for this organization-level issuer: the same
// predicate the ID-JAG validator resolves.
func TrustedIdentityProviderConfigured(ctx context.Context, db *pgxpool.Pool, organizationID string, issuerID uuid.UUID) (bool, error) {
	store, err := idjag.NewPostgresStore(db)
	if err != nil {
		return false, fmt.Errorf("build id-jag store: %w", err)
	}
	_, err = store.TrustedIssuer(ctx, organizationID, issuerID)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, idjag.ErrNoTrustedIssuer), errors.Is(err, idjag.ErrTrustedIssuerHasNoKeys):
		return false, nil
	default:
		return false, fmt.Errorf("resolve trusted issuer: %w", err)
	}
}

// CreateJWTBearerClient registers an assertion-only client on an
// organization-owned issuer, through the same path DCR uses.
func (s *Service) CreateJWTBearerClient(ctx context.Context, payload *orggen.CreateJWTBearerClientPayload) (*orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer id").LogWarn(ctx, s.logger)
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogUserSessionIssuerID(issuerID.String()),
	)

	req := RegistrationRequest{
		ClientName:              strings.TrimSpace(payload.ClientName),
		RedirectURIs:            []string{},
		GrantTypes:              []string{oauthwire.GrantTypeJWTBearer},
		ResponseTypes:           []string{},
		TokenEndpointAuthMethod: payload.TokenEndpointAuthMethod,
		JWKS:                    nil,
		JWKSURI:                 "",
	}
	req.SetDefaults()
	if err := req.Validate(RegistrableGrantTypes, JWTBearerClientAuthMethods); err != nil {
		if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
			return nil, oops.E(oops.CodeBadRequest, err, "%s", oauthErr.Description).LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "validate client registration").LogError(ctx, logger)
	}

	// A project-specific issuer in the caller's organization is refused by
	// name; anything else outside the organization tier reads as not found.
	managed, err := repo.New(s.db).GetOrganizationManagedUserSessionIssuerByID(ctx, repo.GetOrganizationManagedUserSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}
	if managed.ProjectID.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "jwt-bearer clients can only be registered on organization-level user session issuers").LogWarn(ctx, logger)
	}

	trusted, err := TrustedIdentityProviderConfigured(ctx, s.db, authCtx.ActiveOrganizationID, issuerID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read issuer trust").LogError(ctx, logger)
	}

	// bcrypt is slow; mint before the issuer lock is taken.
	credential, err := MintClientCredential(req.TokenEndpointAuthMethod)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "mint client credential").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	txRepo := repo.New(dbtx)

	issuer, err := txRepo.LockOrganizationUserSessionIssuer(ctx, repo.LockOrganizationUserSessionIssuerParams{
		ID:             issuerID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock organization user session issuer").LogError(ctx, logger)
	}

	row, err := CreateClientRegistration(ctx, txRepo, issuer.ID, req, credential)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create user session client").LogError(ctx, logger)
	}

	view := mv.BuildUserSessionClientView(row, 0)
	if err := s.audit.LogUserSessionClientCreate(ctx, dbtx, audit.LogUserSessionClientCreateEvent{
		OrganizationID:                 authCtx.ActiveOrganizationID,
		ProjectID:                      uuid.Nil,
		Actor:                          urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:               authCtx.Email,
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

	logger.InfoContext(ctx, "jwt-bearer user session client registered",
		attr.SlogOAuthClientID(row.ClientID),
		attr.SlogOAuthRegisteredAuthMethod(req.TokenEndpointAuthMethod),
		attr.SlogOAuthClientSecretGenerated(credential.Secret != ""),
	)

	var clientSecret *string
	if credential.Secret != "" {
		clientSecret = &credential.Secret
	}
	return &orggen.CreateOrganizationUserSessionIssuerJWTBearerClientResult{
		Client:                            view,
		ClientSecret:                      clientSecret,
		Issuer:                            s.issuerView(ctx, issuer),
		TrustedIdentityProviderConfigured: trusted,
	}, nil
}
