package remotesessions

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/idpmanagedresources"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// DuplicateClient creates a standalone client with a copy of another client's
// configuration in the caller's organization. The copy keeps the source's
// issuer, project, credential owner, authentication settings, key set, scopes,
// audience, and resource display, and has no user_session_issuer attachments,
// so no MCP server changes which client it signs in through. A private_key_jwt
// client holding a provider-issued client_id cannot be copied.
//
// How the copy gets its upstream credentials depends on the source and the
// payload:
//
//   - A client metadata document client gets its own hosted document, whose URL
//     embeds the new row id and is its client_id.
//   - Otherwise, without payload.ClientID, the copy reuses the source's
//     client_id and stored secret ciphertext, and keeps the source's callback
//     origin and legacy callback mode, because those are what the upstream
//     registration lists.
//   - With payload.ClientID, the copy uses a registration the caller obtained
//     through /oauth/proxy-register, which registers the callback of a client
//     created now, so the copy records that callback origin instead.
func (s *Service) DuplicateClient(ctx context.Context, payload *orgclientsgen.DuplicateClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	sourceID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogWarn(ctx, logger)
	}

	registered, err := parseDuplicateRegistration(ctx, logger, payload)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Take the remote issuer advisory lock, then the client row lock, in the
	// order UpdateClient and rotation use, and copy the row as read under them:
	// a secret replaced or a registration rotated while this waited is then the
	// one copied, and a source re-pointed by migrateIssuer is refused.
	preLock, err := txRepo.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             sourceID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session client before lock").LogError(ctx, logger)
	}
	if err := txRepo.LockRemoteSessionIssuerForClientBinding(ctx, preLock.RemoteSessionClient.RemoteSessionIssuerID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock remote session issuer for client binding").LogError(ctx, logger)
	}
	if err := lockOrganizationClientForAuthMethodWrite(ctx, logger, txRepo, sourceID, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	sourceRow, err := txRepo.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             sourceID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}
	source := sourceRow.RemoteSessionClient
	if source.RemoteSessionIssuerID != preLock.RemoteSessionClient.RemoteSessionIssuerID {
		return nil, oops.E(oops.CodeConflict, nil, "remote session client moved to another issuer; reload and retry").LogWarn(ctx, logger)
	}

	// A managed client's registration belongs to its identity provider
	// connection, so a copy would be an unmanaged client holding its secret.
	if err := idpmanagedresources.RequireUnmanaged(source.IdentityProviderConnectionID, "this remote session client"); err != nil {
		return nil, err
	}

	issuer, err := txRepo.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             source.RemoteSessionIssuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:  true,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session issuer").LogError(ctx, logger)
	}

	// Re-check the key set against the organization and its entitlement, as a
	// create naming it would, rather than trusting the source's link.
	var rawKeySetID *string
	if source.JsonWebKeySetID.Valid {
		rawKeySetID = new(source.JsonWebKeySetID.UUID.String())
	}
	keySetID, err := s.resolveCreateKeySet(ctx, logger, txRepo, rawKeySetID, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	var created repo.RemoteSessionClient
	var registration audit.RemoteSessionClientDuplicateRegistration
	if source.ClientIDMetadataUri.Valid {
		if registered != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "a client metadata document client is duplicated with its own hosted document and takes no credentials").LogWarn(ctx, logger)
		}
		if err := preflightCIMDIssuer(issuer); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "issuer does not support client id metadata documents").LogWarn(ctx, logger)
		}

		created, err = s.duplicateCIMDClient(ctx, logger, txRepo, authCtx.ActiveOrganizationID, source, keySetID)
		registration = audit.RemoteSessionClientDuplicateRegistrationMetadataDocument
	} else {
		created, registration, err = s.duplicateCredentialClient(ctx, logger, txRepo, authCtx.ActiveOrganizationID, issuer, source, keySetID, registered)
	}
	if err != nil {
		return nil, err
	}

	if source.ResourceIdentifier.Valid {
		created, err = txRepo.UpdateRemoteSessionClientResourceDisplay(ctx, repo.UpdateRemoteSessionClientResourceDisplayParams{
			ResourceIdentifier:    source.ResourceIdentifier,
			ResourceName:          conv.FromPGTextOrEmpty[string](source.ResourceName),
			ResourceDocumentation: conv.FromPGTextOrEmpty[string](source.ResourceDocumentation),
			ResourcePolicyUri:     conv.FromPGTextOrEmpty[string](source.ResourcePolicyUri),
			ResourceTosUri:        conv.FromPGTextOrEmpty[string](source.ResourceTosUri),
			ID:                    created.ID,
			ProjectID:             created.ProjectID.UUID,
			OrganizationID:        authCtx.ActiveOrganizationID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "copy remote session client resource display").LogError(ctx, logger)
		}
	}

	// Standalone client: no user_session_issuer attachments.
	snapshot, err := mv.BuildRemoteSessionClientView(created, nil)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientDuplicate(ctx, dbtx, audit.LogRemoteSessionClientDuplicateEvent{
		OrganizationID:               authCtx.ActiveOrganizationID,
		ProjectID:                    orgProjectID(created.ProjectID),
		Actor:                        urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:             authCtx.Email,
		ActorSlug:                    nil,
		RemoteSessionClientURN:       urn.NewRemoteSessionClient(created.ID),
		ClientID:                     created.ClientID,
		SourceRemoteSessionClientURN: urn.NewRemoteSessionClient(source.ID),
		Registration:                 registration,
		SnapshotAfter:                snapshot,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization admin remote session client duplication").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Standalone client: no user_session_issuer attachments.
	view, err := s.clientView(created, nil)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	return view, nil
}

// duplicateRegistration is a fresh upstream registration supplied for a
// duplicated client in place of the source's credentials.
type duplicateRegistration struct {
	// clientID is the registration's client_id.
	clientID string

	// clientSecret is the registration's secret in plaintext, empty when the
	// registration returned none.
	clientSecret string

	// tokenEndpointAuthMethod is the registration's method, nil to keep the
	// source's.
	tokenEndpointAuthMethod *string

	// provenance holds the registration's lifecycle stamps.
	provenance registrationProvenance
}

// parseDuplicateRegistration returns the payload's fresh registration, or nil
// when the payload asks to copy the source's credentials. The registration
// fields other than client_id are refused without it, so a stray secret is
// never silently dropped.
func parseDuplicateRegistration(ctx context.Context, logger *slog.Logger, payload *orgclientsgen.DuplicateClientPayload) (*duplicateRegistration, error) {
	if payload.ClientID == nil {
		if payload.ClientSecret != nil || payload.TokenEndpointAuthMethod != nil || payload.ClientIDIssuedAt != nil || payload.ClientSecretExpiresAt != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "client_secret, token_endpoint_auth_method, client_id_issued_at and client_secret_expires_at require client_id").LogWarn(ctx, logger)
		}
		return nil, nil
	}

	clientID := strings.TrimSpace(*payload.ClientID)
	if clientID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_id must not be empty").LogWarn(ctx, logger)
	}

	provenance, err := parseRegistrationProvenance(ctx, logger, payload.ClientIDIssuedAt, payload.ClientSecretExpiresAt)
	if err != nil {
		return nil, err
	}

	return &duplicateRegistration{
		clientID:                clientID,
		clientSecret:            conv.PtrValOr(payload.ClientSecret, ""),
		tokenEndpointAuthMethod: payload.TokenEndpointAuthMethod,
		provenance:              provenance,
	}, nil
}

// duplicateCIMDClient creates a client metadata document copy of source. Like
// createCimd it records the callback origin of a client created now, since
// the provider reads the redirect URIs from the new document rather than from
// a registration made for the source. The copy keeps the source's
// authentication: its document publishes the method and, with keySetID, its
// own key set URL, so a private_key_jwt copy signs with keys the provider
// reads from the copy.
func (s *Service) duplicateCIMDClient(ctx context.Context, logger *slog.Logger, txRepo *repo.Queries, organizationID string, source repo.RemoteSessionClient, keySetID uuid.NullUUID) (repo.RemoteSessionClient, error) {
	callbackBaseURL, err := s.newClientCallbackBaseURL(ctx, organizationID, CredentialOwnerSubject)
	if err != nil {
		return repo.RemoteSessionClient{}, oops.E(oops.CodeUnexpected, err, "resolve organization callback origin").LogError(ctx, logger)
	}

	// Generate the id up front so the document URL (which embeds it) is the
	// client_id on a single INSERT.
	clientID, err := uuid.NewV7()
	if err != nil {
		return repo.RemoteSessionClient{}, oops.E(oops.CodeUnexpected, err, "generate client id").LogError(ctx, logger)
	}

	created, err := txRepo.CreateRemoteSessionClientCIMD(ctx, repo.CreateRemoteSessionClientCIMDParams{
		ID:                    clientID,
		ProjectID:             source.ProjectID,
		OrganizationID:        conv.ToPGTextEmpty(organizationID),
		RemoteSessionIssuerID: source.RemoteSessionIssuerID,
		ClientIDMetadataUri:   ClientMetadataDocumentURL(s.origins.ForClient(callbackBaseURL), clientID),
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
		Scope:                 source.Scope,
		Audience:              source.Audience,
		CallbackBaseUrl:       callbackBaseURL,
	})
	if err != nil {
		return repo.RemoteSessionClient{}, oops.E(oops.CodeUnexpected, err, "duplicate organization admin remote session client").LogError(ctx, logger)
	}

	authMethod := conv.FromPGText[string](source.TokenEndpointAuthMethod)
	if err := requirePrivateKeyJWTKeySet(authMethod, keySetID); err != nil {
		return repo.RemoteSessionClient{}, err
	}

	created, err = txRepo.SetDuplicatedCIMDRemoteSessionClientAuthentication(ctx, repo.SetDuplicatedCIMDRemoteSessionClientAuthenticationParams{
		TokenEndpointAuthMethod:         source.TokenEndpointAuthMethod,
		TokenEndpointAuthAudienceFormat: source.TokenEndpointAuthAudienceFormat,
		JsonWebKeySetID:                 keySetID,
		GrantTypes:                      source.GrantTypes,
		ID:                              created.ID,
		OrganizationID:                  conv.ToPGText(organizationID),
	})
	if err != nil {
		return repo.RemoteSessionClient{}, oops.E(oops.CodeUnexpected, err, "copy remote session client authentication").LogError(ctx, logger)
	}

	return created, nil
}

// duplicateCredentialClient creates a copy of a client that holds a client_id
// issued by its provider, reusing the source's credentials when registered is
// nil and the supplied registration otherwise.
func (s *Service) duplicateCredentialClient(
	ctx context.Context,
	logger *slog.Logger,
	txRepo *repo.Queries,
	organizationID string,
	issuer repo.RemoteSessionIssuer,
	source repo.RemoteSessionClient,
	keySetID uuid.NullUUID,
	registered *duplicateRegistration,
) (repo.RemoteSessionClient, audit.RemoteSessionClientDuplicateRegistration, error) {
	owner := CredentialOwner(source.CredentialOwner)

	// The provider verifies a private_key_jwt client's assertions with the key
	// set URL registered for it, which embeds the source's row id. A copy
	// would sign with a URL the provider was never given and break when the
	// source or its key set goes away, and dynamic registration cannot
	// register one either.
	if TokenEndpointAuthMethod(source.TokenEndpointAuthMethod.String) == TokenEndpointAuthMethodPrivateKeyJWT {
		return repo.RemoteSessionClient{}, "", oops.E(oops.CodeBadRequest, nil, "a client that signs in with a key set cannot be duplicated, because the identity provider only knows the original client's key set address; create a new client instead").LogWarn(ctx, logger)
	}

	if err := requireSelfClientIssuer(owner, issuer); err != nil {
		return repo.RemoteSessionClient{}, "", err
	}

	params := repo.CreateRemoteSessionClientParams{
		ProjectID:                       source.ProjectID,
		OrganizationID:                  conv.ToPGTextEmpty(organizationID),
		RemoteSessionIssuerID:           source.RemoteSessionIssuerID,
		ClientID:                        source.ClientID,
		ClientSecretEncrypted:           source.ClientSecretEncrypted,
		ClientIDIssuedAt:                source.ClientIDIssuedAt,
		ClientSecretExpiresAt:           source.ClientSecretExpiresAt,
		TokenEndpointAuthMethod:         source.TokenEndpointAuthMethod,
		TokenEndpointAuthAudienceFormat: source.TokenEndpointAuthAudienceFormat,
		Scope:                           source.Scope,
		Audience:                        source.Audience,
		LegacyCallbackUrl:               source.LegacyCallbackUrl,
		JsonWebKeySetID:                 keySetID,
		IdentityProviderConnectionID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		CallbackBaseUrl:                 source.CallbackBaseUrl,
		GrantTypes:                      source.GrantTypes,
		CredentialOwner:                 conv.ToPGText(string(owner)),
	}
	registration := audit.RemoteSessionClientDuplicateRegistrationCopied

	if registered == nil {
		// The copy shares the source's registration, so a registration the
		// provider already refuses would be refused for the copy too.
		if source.UpstreamRejectedAt.Valid {
			return repo.RemoteSessionClient{}, "", oops.E(oops.CodeBadRequest, nil, "the identity provider no longer accepts this client's credentials; duplicate it with a new registration instead").LogWarn(ctx, logger)
		}
	} else {
		// Only a registration /oauth/proxy-register can produce replaces the
		// copied one, matching what rotateClient re-registers: a self client's
		// registration uses a grant the registration does not request.
		if owner == CredentialOwnerSelf {
			return repo.RemoteSessionClient{}, "", oops.E(oops.CodeBadRequest, nil, "a client that signs in as itself cannot be duplicated with a new registration; copy its credentials instead").LogWarn(ctx, logger)
		}

		callbackBaseURL, err := s.newClientCallbackBaseURL(ctx, organizationID, owner)
		if err != nil {
			return repo.RemoteSessionClient{}, "", oops.E(oops.CodeUnexpected, err, "resolve organization callback origin").LogError(ctx, logger)
		}

		secretCiphertext := pgtype.Text{String: "", Valid: false}
		if registered.clientSecret != "" {
			ciphertext, err := s.enc.Encrypt([]byte(registered.clientSecret))
			if err != nil {
				return repo.RemoteSessionClient{}, "", oops.E(oops.CodeUnexpected, err, "encrypt client secret").LogError(ctx, logger)
			}
			secretCiphertext = conv.ToPGText(ciphertext)
		}

		params.ClientID = registered.clientID
		params.ClientSecretEncrypted = secretCiphertext
		params.ClientIDIssuedAt = registered.provenance.clientIDIssuedAt
		params.ClientSecretExpiresAt = registered.provenance.clientSecretExpiresAt
		if registered.tokenEndpointAuthMethod != nil {
			params.TokenEndpointAuthMethod = conv.ToPGText(*registered.tokenEndpointAuthMethod)
		}
		params.LegacyCallbackUrl = false
		params.CallbackBaseUrl = callbackBaseURL
		params.GrantTypes = clientCreateGrantTypes(owner)
		registration = audit.RemoteSessionClientDuplicateRegistrationRegistered
	}

	created, err := txRepo.CreateRemoteSessionClient(ctx, params)
	if err != nil {
		return repo.RemoteSessionClient{}, "", oops.E(oops.CodeUnexpected, err, "duplicate organization admin remote session client").LogError(ctx, logger)
	}

	return created, registration, nil
}
