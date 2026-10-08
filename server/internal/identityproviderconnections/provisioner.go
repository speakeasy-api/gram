// Package identityproviderconnections provisions, rotates, and revokes the
// managed signing credential behind an identity provider connection.
package identityproviderconnections

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	extkeysrepo "github.com/speakeasy-api/gram/server/internal/externalkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ProviderOkta is the identity_provider_connections.provider value for Okta.
const ProviderOkta = "okta"

// providerDisplayNames doubles as the set of known providers.
var providerDisplayNames = map[string]string{
	ProviderOkta: "Okta",
}

// ManagedKeyAlgorithm is what every managed key signs with.
const ManagedKeyAlgorithm = jose.RS256

const signingAlgorithm = ManagedKeyAlgorithm

// systemActorComponent names the provisioner in audit entries.
const systemActorComponent = "identity-provider-connections"

// keyCleanupTimeout bounds the best-effort disable of an abandoned KMS key.
const keyCleanupTimeout = 30 * time.Second

var (
	ErrConnectionNotFound          = errors.New("identityproviderconnections: connection not found")
	ErrConnectionProviderMismatch  = errors.New("identityproviderconnections: connection provider mismatch")
	ErrUnknownProvider             = errors.New("identityproviderconnections: unknown provider")
	ErrIssuerNotFound              = errors.New("identityproviderconnections: issuer not found")
	ErrIssuerHasNoTokenEndpoint    = errors.New("identityproviderconnections: issuer has no token endpoint")
	ErrIssuerTokenEndpointNotHTTPS = errors.New("identityproviderconnections: issuer token endpoint must be an absolute https URL")
	ErrSigningCredentialUnusable   = errors.New("identityproviderconnections: signing credential unusable")
	ErrNotProvisioned              = errors.New("identityproviderconnections: connection is not provisioned")
	ErrKeyAlreadyPublished         = errors.New("identityproviderconnections: key material was already published into the set")
	ErrClientIDRequired            = errors.New("identityproviderconnections: client id is required")
	ErrClientIDAlreadySet          = errors.New("identityproviderconnections: client id was already submitted")
	ErrClientIDInUse               = errors.New("identityproviderconnections: client id is already registered against this issuer")
	ErrActiveKidUnknown            = errors.New("identityproviderconnections: active kid was never published into the managed set")
	ErrAuthMethodMismatch          = errors.New("identityproviderconnections: connection was provisioned with a different client authentication method")
	ErrNoKeySet                    = errors.New("identityproviderconnections: connection authenticates with a client secret and has no key set")
	ErrClientSecretRequired        = errors.New("identityproviderconnections: client secret is required")
	ErrClientSecretNotAccepted     = errors.New("identityproviderconnections: connection does not authenticate with a client secret")
)

// ManagedJWKSCacheTTL is the publish-before-sign window advertised by the JWKS endpoint.
const ManagedJWKSCacheTTL = remotesessions.ManagedClientJSONWebKeySetMaxAgeSeconds * time.Second

// RotationPendingError means publication succeeded but activation must be retried.
// ReadyAt is the earliest safe activation time; no new key is created on retry.
type RotationPendingError struct{ ReadyAt time.Time }

func (e *RotationPendingError) Error() string {
	return "identityproviderconnections: rotation pending until " + e.ReadyAt.Format(time.RFC3339Nano)
}

// Config carries the deployment settings provisioning needs.
type Config struct {
	// KeyRing is the Speakeasy-owned GCP KMS key ring, fully qualified.
	KeyRing string

	// SigningCredentialID is the platform-tier gcp_iam credential that signs.
	SigningCredentialID uuid.UUID

	// SigningServiceAccount, when set, is the only service account the signing
	// credential may impersonate; a credential repointed elsewhere is refused.
	SigningServiceAccount string

	// ServerURL is the origin managed JWKS documents are served from. It is the
	// pinned outbound callback origin: counterparties register the JWKS URL, so
	// it must not move when the server URL does.
	ServerURL *url.URL
}

// Provisioner creates, rotates, and revokes the managed rows behind a connection.
type Provisioner struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	gcpIdentity *gcpauth.Identity
	kmsClients  gcpkms.ProvisioningClientFactory
	audit       *audit.Logger
	cfg         Config
}

// NewProvisioner wires a provisioner over the shared identity resolver, KMS
// factory, and audit logger.
func NewProvisioner(logger *slog.Logger, db *pgxpool.Pool, gcpIdentity *gcpauth.Identity, kmsClients gcpkms.ProvisioningClientFactory, auditLogger *audit.Logger, cfg Config) (*Provisioner, error) {
	if err := gcpkms.ValidateKeyRingName(cfg.KeyRing); err != nil {
		return nil, fmt.Errorf("identity provider connections key ring: %w", err)
	}
	if cfg.SigningCredentialID == uuid.Nil {
		return nil, errors.New("identity provider connections signing credential id is required")
	}
	if cfg.ServerURL == nil {
		return nil, errors.New("identity provider connections server url is required")
	}
	// The JWKS URL is handed to the identity provider; it must never be plaintext.
	if !urls.IsAbsoluteHTTPSOrLoopback(cfg.ServerURL.String()) {
		return nil, errors.New("identity provider connections server url must be an absolute https URL")
	}
	if auditLogger == nil {
		return nil, errors.New("identity provider connections audit logger is required")
	}

	return &Provisioner{
		logger:      logger.With(attr.SlogComponent("identityproviderconnections")),
		db:          db,
		gcpIdentity: gcpIdentity,
		kmsClients:  kmsClients,
		audit:       auditLogger,
		cfg:         cfg,
	}, nil
}

// ProvisionClientParams names the connection to provision.
type ProvisionClientParams struct {
	OrganizationID string
	ConnectionID   uuid.UUID
	Provider       string

	// IssuerID is the organization-level remote_session_issuers row.
	IssuerID uuid.UUID

	// AuthMethod is how the client authenticates to the provider; empty means private_key_jwt.
	AuthMethod remotesessions.TokenEndpointAuthMethod
}

func (p ProvisionClientParams) authMethod() remotesessions.TokenEndpointAuthMethod {
	if p.AuthMethod == "" {
		return remotesessions.TokenEndpointAuthMethodPrivateKeyJWT
	}
	return p.AuthMethod
}

// RotateClientParams names the connection whose key rotates.
type RotateClientParams struct {
	OrganizationID string
	ConnectionID   uuid.UUID
	Provider       string

	// ActiveKid is the key the caller saw active and wants retired. Once it is
	// no longer active the rotation is complete, so a retry after a lost
	// response, or two callers retrying the same rotation, return the current
	// client without publishing another key.
	ActiveKid string
}

// ManagedClient describes a connection's managed registration.
type ManagedClient struct {
	// ClientRowID is the remote_session_clients primary key and JWKS address.
	ClientRowID uuid.UUID

	// ClientID is a placeholder until the administrator submits the real one.
	ClientID string

	IssuerID uuid.UUID

	// AuthMethod is private_key_jwt (a managed key set) or client_secret_basic (no set).
	AuthMethod remotesessions.TokenEndpointAuthMethod

	// ClientSecretEncrypted is the encrypted client secret; empty for private_key_jwt or before submission.
	ClientSecretEncrypted string

	// JSONWebKeySetID is null for client-secret clients.
	JSONWebKeySetID uuid.NullUUID

	// ExternalKeyID is the external key currently backing the set.
	ExternalKeyID uuid.NullUUID

	// ActiveKeyID, ActiveKid, and ActivatedAt are empty after revocation.
	ActiveKeyID uuid.NullUUID
	ActiveKid   string
	ActivatedAt time.Time

	// JSONWebKeySetURL is empty for client-secret clients.
	JSONWebKeySetURL string
}

// ProvisionClient creates the managed KMS key, external key, key set, and
// client for a connection, publishing the first key as active. Idempotent per
// connection. KMS calls run before the write transaction opens.
func (p *Provisioner) ProvisionClient(ctx context.Context, params ProvisionClientParams) (*ManagedClient, error) {
	logger := p.logger.With(attr.SlogOrganizationID(params.OrganizationID))
	q := repo.New(p.db)

	if err := validateProvider(params.Provider); err != nil {
		return nil, err
	}
	if err := requireConnection(ctx, q, params.OrganizationID, params.ConnectionID, params.Provider); err != nil {
		return nil, err
	}

	existing, err := p.lookupManagedClient(ctx, q, params.OrganizationID, params.ConnectionID)
	if err != nil && !errors.Is(err, ErrNotProvisioned) {
		return nil, err
	}
	if existing != nil {
		return adoptExisting(existing, params)
	}

	if _, err := p.loadIssuer(ctx, q, params.OrganizationID, params.IssuerID); err != nil {
		return nil, err
	}

	switch params.authMethod() {
	case remotesessions.TokenEndpointAuthMethodPrivateKeyJWT:
	case remotesessions.TokenEndpointAuthMethodBasic:
		return p.provisionSecretClient(ctx, params)
	case remotesessions.TokenEndpointAuthMethodPost, remotesessions.TokenEndpointAuthMethodNone:
		return nil, fmt.Errorf("unsupported client authentication method %q", params.AuthMethod)
	default:
		return nil, fmt.Errorf("unsupported client authentication method %q", params.AuthMethod)
	}

	signer, err := p.resolveSigningCredential(ctx, logger, q)
	if err != nil {
		return nil, err
	}

	kms, err := p.openKMSClient(ctx)
	if err != nil {
		return nil, err
	}
	defer o11y.LogDefer(ctx, logger, "failed to close gcp kms client", func() error { return kms.Close() })

	created, err := p.createSigningKey(ctx, logger, kms, params.Provider, params.ConnectionID, signer)
	if err != nil {
		return nil, err
	}

	client, err := p.provisionRows(ctx, params, signer, created)
	if err != nil {
		p.abandonKey(ctx, logger, kms, params.ConnectionID, created.key, signer, err)

		if adopted, ok := errors.AsType[*adoptedError](err); ok {
			return adoptExisting(adopted.client, params)
		}
		return nil, err
	}

	return client, nil
}

// adoptExisting returns an already provisioned client, refusing one minted for another method.
func adoptExisting(existing *ManagedClient, params ProvisionClientParams) (*ManagedClient, error) {
	if existing.AuthMethod != params.authMethod() {
		return nil, ErrAuthMethodMismatch
	}
	return existing, nil
}

// provisionSecretClient creates a client-secret client with no key material;
// the secret arrives with the administrator's client id.
func (p *Provisioner) provisionSecretClient(ctx context.Context, params ProvisionClientParams) (*ManagedClient, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin provisioning transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tq := repo.New(dbtx)
	if _, err := tq.LockIdentityProviderConnectionForProvisioning(ctx, repo.LockIdentityProviderConnectionForProvisioningParams{
		ID:             params.ConnectionID,
		OrganizationID: params.OrganizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConnectionNotFound
		}
		return nil, fmt.Errorf("lock connection for provisioning: %w", err)
	}

	existing, err := p.lookupManagedClient(ctx, tq, params.OrganizationID, params.ConnectionID)
	if err != nil && !errors.Is(err, ErrNotProvisioned) {
		return nil, err
	}
	if existing != nil {
		return adoptExisting(existing, params)
	}

	if err := remotesessionsrepo.New(dbtx).LockRemoteSessionIssuerForClientBinding(ctx, params.IssuerID); err != nil {
		return nil, fmt.Errorf("lock issuer for client binding: %w", err)
	}
	issuer, err := p.loadIssuer(ctx, tq, params.OrganizationID, params.IssuerID)
	if err != nil {
		return nil, err
	}

	client, err := remotesessionsrepo.New(dbtx).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:                       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:                  conv.ToPGText(params.OrganizationID),
		RemoteSessionIssuerID:           issuer.ID,
		ClientID:                        PlaceholderClientID(params.Provider, params.ConnectionID),
		ClientSecretEncrypted:           pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:                pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		ClientSecretExpiresAt:           pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod:         conv.ToPGText(string(remotesessions.TokenEndpointAuthMethodBasic)),
		TokenEndpointAuthAudienceFormat: conv.ToPGText(string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint)),
		Scope:                           nil,
		Audience:                        pgtype.Text{String: "", Valid: false},
		LegacyCallbackUrl:               false,
		CallbackBaseUrl:                 pgtype.Text{String: "", Valid: false},
		JsonWebKeySetID:                 uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		IdentityProviderConnectionID:    conv.ToNullUUID(params.ConnectionID),
		GrantTypes:                      nil,
		CredentialOwner:                 pgtype.Text{String: "", Valid: false},
	})
	if err != nil {
		return nil, fmt.Errorf("create managed remote session client: %w", err)
	}

	if err := p.audit.LogRemoteSessionClientCreate(ctx, dbtx, audit.LogRemoteSessionClientCreateEvent{
		OrganizationID:         params.OrganizationID,
		ProjectID:              uuid.Nil,
		Actor:                  systemActor(),
		ActorDisplayName:       systemActorDisplayName(),
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
		ClientID:               client.ClientID,
		SnapshotAfter:          nil,
	}); err != nil {
		return nil, fmt.Errorf("record managed client creation: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit provisioning transaction: %w", err)
	}

	return &ManagedClient{
		ClientRowID:           client.ID,
		ClientID:              client.ClientID,
		IssuerID:              client.RemoteSessionIssuerID,
		AuthMethod:            remotesessions.TokenEndpointAuthMethodBasic,
		ClientSecretEncrypted: "",
		JSONWebKeySetID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExternalKeyID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActiveKeyID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActiveKid:             "",
		ActivatedAt:           time.Time{},
		JSONWebKeySetURL:      "",
	}, nil
}

// provisionRows writes every managed row in one transaction.
func (p *Provisioner) provisionRows(ctx context.Context, params ProvisionClientParams, signer *signingCredential, created *createdKey) (*ManagedClient, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin provisioning transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tq := repo.New(dbtx)

	if _, err := tq.LockIdentityProviderConnectionForProvisioning(ctx, repo.LockIdentityProviderConnectionForProvisioningParams{
		ID:             params.ConnectionID,
		OrganizationID: params.OrganizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConnectionNotFound
		}
		return nil, fmt.Errorf("lock connection for provisioning: %w", err)
	}

	// A concurrent run may have landed between the unlocked check and this lock.
	existing, err := p.lookupManagedClient(ctx, tq, params.OrganizationID, params.ConnectionID)
	if err != nil && !errors.Is(err, ErrNotProvisioned) {
		return nil, err
	}
	if existing != nil {
		return nil, &adoptedError{client: existing}
	}

	// Serialize against issuer migration and deletion, then re-read under the lock.
	if err := remotesessionsrepo.New(dbtx).LockRemoteSessionIssuerForClientBinding(ctx, params.IssuerID); err != nil {
		return nil, fmt.Errorf("lock issuer for client binding: %w", err)
	}
	issuer, err := p.loadIssuer(ctx, tq, params.OrganizationID, params.IssuerID)
	if err != nil {
		return nil, err
	}

	connectionName := providerDisplayNames[params.Provider] + " connection " + params.ConnectionID.String()
	marker := conv.ToNullUUID(params.ConnectionID)

	externalKey, err := p.createManagedExternalKey(ctx, dbtx, params.OrganizationID, marker, signer, connectionName, created)
	if err != nil {
		return nil, err
	}
	if err := requireSigningCredentialUnchanged(ctx, tq, signer); err != nil {
		return nil, err
	}

	jq := jwksrepo.New(dbtx)
	set, err := jq.CreateJsonWebKeySet(ctx, jwksrepo.CreateJsonWebKeySetParams{
		OrganizationID:               params.OrganizationID,
		ExternalKeyID:                externalKey.ID,
		Name:                         connectionName + " keys",
		IdentityProviderConnectionID: marker,
	})
	if err != nil {
		return nil, fmt.Errorf("create managed key set: %w", err)
	}

	key, err := jq.CreateJsonWebKey(ctx, jwksrepo.CreateJsonWebKeyParams{
		OrganizationID:  params.OrganizationID,
		JsonWebKeySetID: set.ID,
		ExternalKeyID:   externalKey.ID,
		State:           "active",
		Kid:             created.kid,
		PublicJwk:       created.publicJWK,
	})
	if err != nil {
		return nil, fmt.Errorf("publish managed key: %w", err)
	}

	client, err := remotesessionsrepo.New(dbtx).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:                       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:                  conv.ToPGText(params.OrganizationID),
		RemoteSessionIssuerID:           issuer.ID,
		ClientID:                        PlaceholderClientID(params.Provider, params.ConnectionID),
		ClientSecretEncrypted:           pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:                pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		ClientSecretExpiresAt:           pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod:         conv.ToPGText(string(remotesessions.TokenEndpointAuthMethodPrivateKeyJWT)),
		TokenEndpointAuthAudienceFormat: conv.ToPGText(string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint)),
		Scope:                           nil,
		Audience:                        pgtype.Text{String: "", Valid: false},
		LegacyCallbackUrl:               false,
		JsonWebKeySetID:                 conv.ToNullUUID(set.ID),
		IdentityProviderConnectionID:    marker,
		// Connection clients authenticate with private_key_jwt and register no
		// redirect_uri; their JWKS URL stays on the pinned outbound origin.
		CallbackBaseUrl: pgtype.Text{String: "", Valid: false},
		GrantTypes:      nil,
		CredentialOwner: pgtype.Text{String: "", Valid: false},
	})
	if err != nil {
		return nil, fmt.Errorf("create managed remote session client: %w", err)
	}

	if err := p.audit.LogJsonWebKeySetCreate(ctx, dbtx, audit.LogJsonWebKeySetCreateEvent{
		OrganizationID:   params.OrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Actor:            systemActor(),
		ActorDisplayName: systemActorDisplayName(),
		ActorSlug:        nil,
		SetURN:           urn.NewJsonWebKeySet(set.ID),
		SetName:          set.Name,
	}); err != nil {
		return nil, fmt.Errorf("record managed key set creation: %w", err)
	}
	if err := p.audit.LogJsonWebKeyPublish(ctx, dbtx, keyEvent(params.OrganizationID, key, nil, nil)); err != nil {
		return nil, fmt.Errorf("record managed key publication: %w", err)
	}
	if err := p.audit.LogRemoteSessionClientCreate(ctx, dbtx, audit.LogRemoteSessionClientCreateEvent{
		OrganizationID:         params.OrganizationID,
		ProjectID:              uuid.Nil,
		Actor:                  systemActor(),
		ActorDisplayName:       systemActorDisplayName(),
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
		ClientID:               client.ClientID,
		SnapshotAfter:          nil,
	}); err != nil {
		return nil, fmt.Errorf("record managed client creation: %w", err)
	}

	if err := commitPublication(ctx, dbtx, params.OrganizationID); err != nil {
		return nil, err
	}

	return &ManagedClient{
		ClientRowID:           client.ID,
		ClientID:              client.ClientID,
		IssuerID:              client.RemoteSessionIssuerID,
		AuthMethod:            remotesessions.TokenEndpointAuthMethodPrivateKeyJWT,
		ClientSecretEncrypted: "",
		JSONWebKeySetID:       conv.ToNullUUID(set.ID),
		ExternalKeyID:         conv.ToNullUUID(externalKey.ID),
		ActiveKeyID:           conv.ToNullUUID(key.ID),
		ActiveKid:             key.Kid,
		ActivatedAt:           key.ActivatedAt.Time,
		JSONWebKeySetURL:      remotesessions.ClientJSONWebKeySetURL(p.cfg.ServerURL, client.ID),
	}, nil
}

// RotateClient first commits a pending key, leaving the current signer unchanged.
// Retry after RotationPendingError.ReadyAt with the same params to activate the
// same key; a retry after activation returns the rotated client. Publication
// and activation never share a transaction and no call waits for the cache TTL.
func (p *Provisioner) RotateClient(ctx context.Context, params RotateClientParams) (*ManagedClient, error) {
	if err := validateProvider(params.Provider); err != nil {
		return nil, err
	}
	if err := requireConnection(ctx, repo.New(p.db), params.OrganizationID, params.ConnectionID, params.Provider); err != nil {
		return nil, err
	}
	if err := p.rotateRows(ctx, params); err != nil {
		return nil, err
	}
	return p.lookupManagedClient(ctx, repo.New(p.db), params.OrganizationID, params.ConnectionID)
}

func (p *Provisioner) rotateRows(ctx context.Context, params RotateClientParams) error {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin rotation transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tq := repo.New(dbtx)

	if _, err := tq.LockIdentityProviderConnectionForProvisioning(ctx, repo.LockIdentityProviderConnectionForProvisioningParams{
		ID:             params.ConnectionID,
		OrganizationID: params.OrganizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConnectionNotFound
		}
		return fmt.Errorf("lock connection for rotation: %w", err)
	}

	existing, err := p.lookupManagedClient(ctx, tq, params.OrganizationID, params.ConnectionID)
	if err != nil {
		return err
	}
	if !existing.JSONWebKeySetID.Valid {
		return ErrNoKeySet
	}

	jq := jwksrepo.New(dbtx)
	set, err := jq.LockJsonWebKeySetForKeyWrite(ctx, jwksrepo.LockJsonWebKeySetForKeyWriteParams{
		ID:             existing.JSONWebKeySetID.UUID,
		OrganizationID: params.OrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotProvisioned
	case err != nil:
		return fmt.Errorf("lock managed key set: %w", err)
	}

	// Read under the set lock shared with revocation and every key writer.
	keys, err := jq.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: params.OrganizationID, IncludeRevoked: false})
	if err != nil {
		return fmt.Errorf("load rotation keys: %w", err)
	}
	key, active := pendingAndActive(keys)
	// A revoked client must not be resurrected by a delayed rotation request.
	if active.ID == uuid.Nil {
		return ErrNotProvisioned
	}
	if active.Kid != params.ActiveKid {
		return p.requireRotationComplete(ctx, jq, set.ID, params)
	}
	if key.ID == uuid.Nil {
		// Release the locks: the key is created outside any transaction.
		if err := dbtx.Rollback(ctx); err != nil {
			return fmt.Errorf("release rotation locks: %w", err)
		}
		return p.publishRotationKey(ctx, params, set.ID)
	}
	if key.UpdatedAt.InfinityModifier != pgtype.Finite {
		if err := dbtx.Rollback(ctx); err != nil {
			return fmt.Errorf("release rotation locks: %w", err)
		}
		published, err := repo.New(p.db).ObserveRotationPublication(ctx, repo.ObserveRotationPublicationParams{ID: key.ID, OrganizationID: params.OrganizationID})
		if err != nil {
			return fmt.Errorf("observe recovered key publication: %w", err)
		}
		return &RotationPendingError{ReadyAt: published.Time.Add(ManagedJWKSCacheTTL)}
	}
	readyAt := key.UpdatedAt.Time.Add(ManagedJWKSCacheTTL)
	ready, err := tq.RotationPublicationReady(ctx, repo.RotationPublicationReadyParams{ID: key.ID, OrganizationID: params.OrganizationID, CacheSeconds: int32(ManagedJWKSCacheTTL / time.Second)})
	if err != nil {
		return fmt.Errorf("check rotation cache window: %w", err)
	}
	if !ready {
		return &RotationPendingError{ReadyAt: readyAt}
	}

	previous, err := jq.RetireActiveJsonWebKey(ctx, jwksrepo.RetireActiveJsonWebKeyParams{
		JsonWebKeySetID: set.ID,
		OrganizationID:  params.OrganizationID,
		ExcludeID:       key.ID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return fmt.Errorf("retire active managed key: %w", err)
	default:
		before := keySnapshot(previous)
		before.State = "active"
		if err := p.audit.LogJsonWebKeyRetire(ctx, dbtx, keyEvent(params.OrganizationID, previous, before, keySnapshot(previous))); err != nil {
			return fmt.Errorf("record managed key retirement: %w", err)
		}
	}

	activated, err := jq.ActivateJsonWebKey(ctx, jwksrepo.ActivateJsonWebKeyParams{
		ID:             key.ID,
		OrganizationID: params.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("activate rotated key: %w", err)
	}
	if err := p.audit.LogJsonWebKeyActivate(ctx, dbtx, keyEvent(params.OrganizationID, activated, keySnapshot(key), keySnapshot(activated))); err != nil {
		return fmt.Errorf("record rotated key activation: %w", err)
	}

	updated, err := jq.UpdateJsonWebKeySet(ctx, jwksrepo.UpdateJsonWebKeySetParams{
		Name:           set.Name,
		ExternalKeyID:  key.ExternalKeyID,
		ID:             set.ID,
		OrganizationID: params.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("re-point managed key set: %w", err)
	}
	if err := p.audit.LogJsonWebKeySetUpdate(ctx, dbtx, audit.LogJsonWebKeySetUpdateEvent{
		OrganizationID:    params.OrganizationID,
		ProjectID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Actor:             systemActor(),
		ActorDisplayName:  systemActorDisplayName(),
		ActorSlug:         nil,
		SetURN:            urn.NewJsonWebKeySet(set.ID),
		SetName:           updated.Name,
		SetSnapshotBefore: &audit.JsonWebKeySetSnapshot{Name: set.Name, ExternalKeyID: set.ExternalKeyID.String()},
		SetSnapshotAfter:  &audit.JsonWebKeySetSnapshot{Name: updated.Name, ExternalKeyID: updated.ExternalKeyID.String()},
	}); err != nil {
		return fmt.Errorf("record managed key set update: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit rotation transaction: %w", err)
	}

	return nil
}

// pendingAndActive picks the pending and active keys, zero when absent.
func pendingAndActive(keys []jwksrepo.JsonWebKey) (pending, active jwksrepo.JsonWebKey) {
	for _, candidate := range keys {
		switch candidate.State {
		case "active":
			active = candidate
		case "pending":
			pending = candidate
		}
	}
	return pending, active
}

// requireRotationComplete treats a caller whose kid is no longer active as a
// finished rotation, unless the kid was never in the set at all.
func (p *Provisioner) requireRotationComplete(ctx context.Context, jq *jwksrepo.Queries, setID uuid.UUID, params RotateClientParams) error {
	known, err := jq.JsonWebKeyKidExistsInSet(ctx, jwksrepo.JsonWebKeyKidExistsInSetParams{JsonWebKeySetID: setID, OrganizationID: params.OrganizationID, Kid: params.ActiveKid})
	if err != nil {
		return fmt.Errorf("check rotation source kid: %w", err)
	}
	if !known {
		return ErrActiveKidUnknown
	}
	return nil
}

// publishRotationKey creates the KMS key with no row locks held, then re-locks
// and re-reads the set: a pending key published meanwhile, or a revocation, wins
// and the new key is abandoned. The transaction only writes rows.
func (p *Provisioner) publishRotationKey(ctx context.Context, params RotateClientParams, setID uuid.UUID) error {
	logger := p.logger.With(attr.SlogOrganizationID(params.OrganizationID))
	signer, err := p.resolveSigningCredential(ctx, logger, repo.New(p.db))
	if err != nil {
		return err
	}
	kms, err := p.openKMSClient(ctx)
	if err != nil {
		return err
	}
	defer o11y.LogDefer(ctx, logger, "failed to close gcp kms client", func() error { return kms.Close() })
	created, err := p.createSigningKey(ctx, logger, kms, params.Provider, params.ConnectionID, signer)
	if err != nil {
		return err
	}

	pendingErr, err := p.publishRotationRows(ctx, params, setID, signer, created)
	if err != nil {
		p.abandonKey(ctx, logger, kms, params.ConnectionID, created.key, signer, err)
		if _, ok := errors.AsType[*adoptedError](err); ok {
			return pendingErr
		}
		return err
	}
	return pendingErr
}

// publishRotationRows writes the pending key under the connection and set
// locks. It returns the RotationPendingError to surface on success; an
// adoptedError means another writer already published a pending key.
func (p *Provisioner) publishRotationRows(ctx context.Context, params RotateClientParams, setID uuid.UUID, signer *signingCredential, created *createdKey) (*RotationPendingError, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin rotation publication transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tq := repo.New(dbtx)
	if _, err := tq.LockIdentityProviderConnectionForProvisioning(ctx, repo.LockIdentityProviderConnectionForProvisioningParams{
		ID:             params.ConnectionID,
		OrganizationID: params.OrganizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConnectionNotFound
		}
		return nil, fmt.Errorf("lock connection for rotation publication: %w", err)
	}
	jq := jwksrepo.New(dbtx)
	set, err := jq.LockJsonWebKeySetForKeyWrite(ctx, jwksrepo.LockJsonWebKeySetForKeyWriteParams{ID: setID, OrganizationID: params.OrganizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNotProvisioned
	case err != nil:
		return nil, fmt.Errorf("lock managed key set: %w", err)
	}
	keys, err := jq.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: params.OrganizationID, IncludeRevoked: false})
	if err != nil {
		return nil, fmt.Errorf("re-read rotation keys: %w", err)
	}
	pending, active := pendingAndActive(keys)
	if active.ID == uuid.Nil {
		return nil, ErrNotProvisioned
	}
	if active.Kid != params.ActiveKid {
		// Another caller finished the rotation while the key was being created.
		if err := p.requireRotationComplete(ctx, jq, set.ID, params); err != nil {
			return nil, err
		}
		return nil, &adoptedError{client: nil}
	}
	if pending.ID != uuid.Nil {
		// Someone else published while the key was being created: adopt theirs.
		if err := dbtx.Rollback(ctx); err != nil {
			return nil, fmt.Errorf("release rotation locks: %w", err)
		}
		published, err := repo.New(p.db).ObserveRotationPublication(ctx, repo.ObserveRotationPublicationParams{ID: pending.ID, OrganizationID: params.OrganizationID})
		if err != nil {
			return nil, fmt.Errorf("observe adopted key publication: %w", err)
		}
		return &RotationPendingError{ReadyAt: published.Time.Add(ManagedJWKSCacheTTL)}, &adoptedError{client: nil}
	}
	kidExists, err := jq.JsonWebKeyKidExistsInSet(ctx, jwksrepo.JsonWebKeyKidExistsInSetParams{JsonWebKeySetID: set.ID, OrganizationID: params.OrganizationID, Kid: created.kid})
	if err != nil {
		return nil, fmt.Errorf("check managed kid: %w", err)
	}
	if kidExists {
		return nil, ErrKeyAlreadyPublished
	}

	connectionName := providerDisplayNames[params.Provider] + " connection " + params.ConnectionID.String()
	marker := conv.ToNullUUID(params.ConnectionID)
	externalKey, err := p.createManagedExternalKey(ctx, dbtx, params.OrganizationID, marker, signer, connectionName, created)
	if err != nil {
		return nil, err
	}
	if err := requireSigningCredentialUnchanged(ctx, tq, signer); err != nil {
		return nil, err
	}
	key, err := jq.CreateJsonWebKey(ctx, jwksrepo.CreateJsonWebKeyParams{
		OrganizationID:  params.OrganizationID,
		JsonWebKeySetID: set.ID,
		ExternalKeyID:   externalKey.ID,
		State:           "pending",
		Kid:             created.kid,
		PublicJwk:       created.publicJWK,
	})
	if err != nil {
		return nil, fmt.Errorf("publish rotated key: %w", err)
	}
	if err := p.audit.LogJsonWebKeyPublish(ctx, dbtx, keyEvent(params.OrganizationID, key, nil, nil)); err != nil {
		return nil, fmt.Errorf("record rotated key publication: %w", err)
	}
	// Infinity marks publication whose commit has not yet been observed. A
	// retry after a crash starts the TTL only after seeing the committed key.
	if err := tq.MarkRotationPublicationUnobserved(ctx, repo.MarkRotationPublicationUnobservedParams{ID: key.ID, OrganizationID: params.OrganizationID}); err != nil {
		return nil, fmt.Errorf("mark unobserved key publication: %w", err)
	}
	if err := commitPublication(ctx, dbtx, params.OrganizationID); err != nil {
		return nil, err
	}
	published, err := repo.New(p.db).ObserveRotationPublication(ctx, repo.ObserveRotationPublicationParams{ID: key.ID, OrganizationID: params.OrganizationID})
	if err != nil {
		// The key is committed and referenced: cleanup must reconcile, never disable.
		return nil, unobservedPublicationError(params.OrganizationID, fmt.Errorf("observe committed key publication: %w", err))
	}
	return &RotationPendingError{ReadyAt: published.Time.Add(ManagedJWKSCacheTTL)}, nil
}

// PlaceholderClientID is the client_id a managed client carries until the
// administrator submits the real one.
func PlaceholderClientID(provider string, connectionID uuid.UUID) string {
	return provider + "-pending-" + connectionID.String()
}

// SetClientIDParams names the managed client whose placeholder gives way to
// the administrator-submitted client id.
type SetClientIDParams struct {
	OrganizationID string
	ConnectionID   uuid.UUID
	Provider       string
	ClientID       string

	// ClientSecretEncrypted is required for client-secret clients and refused otherwise.
	ClientSecretEncrypted string

	// Actor is the administrator recorded on the client's audit entry.
	Actor            urn.Principal
	ActorDisplayName *string
}

// SetClientID replaces the placeholder client id inside the caller's transaction, once per connection.
func (p *Provisioner) SetClientID(ctx context.Context, dbtx pgx.Tx, params SetClientIDParams) (*ManagedClient, error) {
	if err := validateProvider(params.Provider); err != nil {
		return nil, err
	}
	if params.ClientID == "" {
		return nil, ErrClientIDRequired
	}
	q := repo.New(dbtx)
	if err := requireConnection(ctx, q, params.OrganizationID, params.ConnectionID, params.Provider); err != nil {
		return nil, err
	}
	row, existing, err := p.lookupManagedClientRow(ctx, q, params.OrganizationID, params.ConnectionID)
	if err != nil {
		return nil, err
	}
	placeholder := PlaceholderClientID(params.Provider, params.ConnectionID)
	if existing.ClientID != placeholder {
		return nil, ErrClientIDAlreadySet
	}
	if params.ClientID == placeholder {
		return nil, fmt.Errorf("%w: placeholder", ErrClientIDRequired)
	}
	if err := requireSecretFor(existing.AuthMethod, params.ClientSecretEncrypted); err != nil {
		return nil, err
	}

	// Serializes concurrent claims of one client id on the issuer; no index enforces it.
	if err := remotesessionsrepo.New(dbtx).LockRemoteSessionIssuerForClientBinding(ctx, existing.IssuerID); err != nil {
		return nil, fmt.Errorf("lock issuer for client id collision check: %w", err)
	}
	inUse, err := q.ManagedClientIDInUse(ctx, repo.ManagedClientIDInUseParams{
		RemoteSessionIssuerID: existing.IssuerID,
		OrganizationID:        conv.ToPGText(params.OrganizationID),
		ClientID:              params.ClientID,
		ExcludeID:             existing.ClientRowID,
	})
	if err != nil {
		return nil, fmt.Errorf("check client id collision: %w", err)
	}
	if inUse {
		return nil, ErrClientIDInUse
	}

	updated, err := q.SetManagedClientID(ctx, repo.SetManagedClientIDParams{
		ClientID:                     params.ClientID,
		ClientSecretEncrypted:        conv.ToPGTextEmpty(params.ClientSecretEncrypted),
		ID:                           existing.ClientRowID,
		OrganizationID:               conv.ToPGText(params.OrganizationID),
		IdentityProviderConnectionID: conv.ToNullUUID(params.ConnectionID),
		PlaceholderClientID:          placeholder,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrClientIDAlreadySet
	case err != nil:
		return nil, fmt.Errorf("set managed client id: %w", err)
	}

	if err := p.logClientUpdate(ctx, dbtx, params.OrganizationID, params.Actor, params.ActorDisplayName, row.RemoteSessionClient, updated); err != nil {
		return nil, err
	}

	result := *existing
	result.ClientID = updated.ClientID
	result.ClientSecretEncrypted = updated.ClientSecretEncrypted.String
	return &result, nil
}

// requireSecretFor enforces that exactly the client-secret method carries a secret.
func requireSecretFor(method remotesessions.TokenEndpointAuthMethod, secretEncrypted string) error {
	isSecret := method == remotesessions.TokenEndpointAuthMethodBasic
	switch {
	case isSecret && secretEncrypted == "":
		return ErrClientSecretRequired
	case !isSecret && secretEncrypted != "":
		return ErrClientSecretNotAccepted
	}
	return nil
}

// ReplaceClientSecretParams names the client-secret client whose secret is replaced.
type ReplaceClientSecretParams struct {
	OrganizationID        string
	ConnectionID          uuid.UUID
	Provider              string
	ClientSecretEncrypted string

	// Actor is the administrator recorded on the client's audit entry.
	Actor            urn.Principal
	ActorDisplayName *string
}

// ReplaceClientSecret swaps a client-secret client's secret inside the caller's transaction.
func (p *Provisioner) ReplaceClientSecret(ctx context.Context, dbtx pgx.Tx, params ReplaceClientSecretParams) (*ManagedClient, error) {
	if err := validateProvider(params.Provider); err != nil {
		return nil, err
	}
	if params.ClientSecretEncrypted == "" {
		return nil, ErrClientSecretRequired
	}
	q := repo.New(dbtx)
	if err := requireConnection(ctx, q, params.OrganizationID, params.ConnectionID, params.Provider); err != nil {
		return nil, err
	}
	row, existing, err := p.lookupManagedClientRow(ctx, q, params.OrganizationID, params.ConnectionID)
	if err != nil {
		return nil, err
	}
	if existing.AuthMethod != remotesessions.TokenEndpointAuthMethodBasic {
		return nil, ErrClientSecretNotAccepted
	}

	updated, err := q.SetManagedClientSecret(ctx, repo.SetManagedClientSecretParams{
		ClientSecretEncrypted:        conv.ToPGText(params.ClientSecretEncrypted),
		ID:                           existing.ClientRowID,
		OrganizationID:               conv.ToPGText(params.OrganizationID),
		IdentityProviderConnectionID: conv.ToNullUUID(params.ConnectionID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNotProvisioned
	case err != nil:
		return nil, fmt.Errorf("set managed client secret: %w", err)
	}

	if err := p.logClientUpdate(ctx, dbtx, params.OrganizationID, params.Actor, params.ActorDisplayName, row.RemoteSessionClient, updated); err != nil {
		return nil, err
	}

	result := *existing
	result.ClientSecretEncrypted = updated.ClientSecretEncrypted.String
	return &result, nil
}

// SwitchAuthMethodParams names the pending connection whose client changes method.
type SwitchAuthMethodParams struct {
	OrganizationID string
	ConnectionID   uuid.UUID
	Provider       string

	// AuthMethod is the method the client switches to.
	AuthMethod remotesessions.TokenEndpointAuthMethod

	// ListingMode is written to the connection's Okta row with the switch.
	ListingMode string

	// Actor is the administrator recorded on the client's audit entry.
	Actor            urn.Principal
	ActorDisplayName *string

	// Record writes the caller's audit entry inside the switch transaction.
	Record func(ctx context.Context, dbtx pgx.Tx, client *ManagedClient) error
}

// record runs the caller's audit hook, when set, before the switch commits.
func (params SwitchAuthMethodParams) record(ctx context.Context, dbtx pgx.Tx, client *ManagedClient) error {
	if params.Record == nil {
		return nil
	}
	return params.Record(ctx, dbtx, client)
}

// SwitchAuthMethod changes how a connection's client authenticates while its
// client id is still the placeholder. Moving to client_secret_basic detaches the
// key set, which parks it: the client's JWKS URL stops serving it at once.
// Moving back reattaches a parked set, and mints a key outside the write
// transaction only when there is none, so a connection holds at most one set.
// RetireParkedKeySets retires a parked set once the method is settled.
func (p *Provisioner) SwitchAuthMethod(ctx context.Context, params SwitchAuthMethodParams) (*ManagedClient, error) {
	if err := validateProvider(params.Provider); err != nil {
		return nil, err
	}
	switch params.AuthMethod {
	case remotesessions.TokenEndpointAuthMethodBasic:
		return p.switchToSecret(ctx, params)
	case remotesessions.TokenEndpointAuthMethodPrivateKeyJWT:
		return p.switchToKeySet(ctx, params)
	case remotesessions.TokenEndpointAuthMethodPost, remotesessions.TokenEndpointAuthMethodNone:
		return nil, fmt.Errorf("unsupported client authentication method %q", params.AuthMethod)
	default:
		return nil, fmt.Errorf("unsupported client authentication method %q", params.AuthMethod)
	}
}

// lockForSwitch locks the connection and returns its client when it may still switch methods.
func (p *Provisioner) lockForSwitch(ctx context.Context, tq *repo.Queries, params SwitchAuthMethodParams) (repo.GetManagedClientRow, *ManagedClient, error) {
	var none repo.GetManagedClientRow
	if _, err := tq.LockIdentityProviderConnectionForProvisioning(ctx, repo.LockIdentityProviderConnectionForProvisioningParams{
		ID:             params.ConnectionID,
		OrganizationID: params.OrganizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return none, nil, ErrConnectionNotFound
		}
		return none, nil, fmt.Errorf("lock connection for method switch: %w", err)
	}
	row, existing, err := p.lookupManagedClientRow(ctx, tq, params.OrganizationID, params.ConnectionID)
	if err != nil {
		return none, nil, err
	}
	if existing.ClientID != PlaceholderClientID(params.Provider, params.ConnectionID) {
		return none, nil, ErrClientIDAlreadySet
	}
	return row, existing, nil
}

func (p *Provisioner) setListingMode(ctx context.Context, tq *repo.Queries, params SwitchAuthMethodParams) error {
	if _, err := tq.SetOktaListingMode(ctx, repo.SetOktaListingModeParams{
		ListingMode:                  params.ListingMode,
		IdentityProviderConnectionID: params.ConnectionID,
		OrganizationID:               params.OrganizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConnectionNotFound
		}
		return fmt.Errorf("set okta listing mode: %w", err)
	}
	return nil
}

func (p *Provisioner) switchToSecret(ctx context.Context, params SwitchAuthMethodParams) (*ManagedClient, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin method switch transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tq := repo.New(dbtx)
	row, existing, err := p.lockForSwitch(ctx, tq, params)
	if err != nil {
		return nil, err
	}
	if err := p.setListingMode(ctx, tq, params); err != nil {
		return nil, err
	}
	if existing.AuthMethod == remotesessions.TokenEndpointAuthMethodBasic {
		if err := params.record(ctx, dbtx, existing); err != nil {
			return nil, err
		}
		if err := dbtx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit method switch transaction: %w", err)
		}
		return existing, nil
	}

	updated, err := tq.SwitchManagedClientToSecret(ctx, repo.SwitchManagedClientToSecretParams{
		ID:                           existing.ClientRowID,
		OrganizationID:               conv.ToPGText(params.OrganizationID),
		IdentityProviderConnectionID: conv.ToNullUUID(params.ConnectionID),
		PlaceholderClientID:          existing.ClientID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrClientIDAlreadySet
	case err != nil:
		return nil, fmt.Errorf("switch managed client to client secret: %w", err)
	}
	if err := p.logClientUpdate(ctx, dbtx, params.OrganizationID, params.Actor, params.ActorDisplayName, row.RemoteSessionClient, updated); err != nil {
		return nil, err
	}
	switched := &ManagedClient{
		ClientRowID:           updated.ID,
		ClientID:              updated.ClientID,
		IssuerID:              updated.RemoteSessionIssuerID,
		AuthMethod:            remotesessions.TokenEndpointAuthMethodBasic,
		ClientSecretEncrypted: "",
		JSONWebKeySetID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExternalKeyID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActiveKeyID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActiveKid:             "",
		ActivatedAt:           time.Time{},
		JSONWebKeySetURL:      "",
	}
	if err := params.record(ctx, dbtx, switched); err != nil {
		return nil, err
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit method switch transaction: %w", err)
	}
	return switched, nil
}

func (p *Provisioner) switchToKeySet(ctx context.Context, params SwitchAuthMethodParams) (*ManagedClient, error) {
	logger := p.logger.With(attr.SlogOrganizationID(params.OrganizationID), attr.SlogIdentityProviderConnectionID(params.ConnectionID.String()))
	q := repo.New(p.db)

	// Unlocked pre-check so a repeat or a refused switch mints no key.
	existing, err := p.lookupManagedClient(ctx, q, params.OrganizationID, params.ConnectionID)
	if err != nil {
		return nil, err
	}
	if existing.ClientID != PlaceholderClientID(params.Provider, params.ConnectionID) {
		return nil, ErrClientIDAlreadySet
	}
	if existing.AuthMethod == remotesessions.TokenEndpointAuthMethodPrivateKeyJWT {
		return p.switchListingOnly(ctx, params)
	}

	reattached, err := p.reattachParkedKeySet(ctx, params)
	if !errors.Is(err, errNoParkedKeySet) {
		return reattached, err
	}

	signer, err := p.resolveSigningCredential(ctx, logger, q)
	if err != nil {
		return nil, err
	}
	kms, err := p.openKMSClient(ctx)
	if err != nil {
		return nil, err
	}
	defer o11y.LogDefer(ctx, logger, "failed to close gcp kms client", func() error { return kms.Close() })

	created, err := p.createSigningKey(ctx, logger, kms, params.Provider, params.ConnectionID, signer)
	if err != nil {
		return nil, err
	}

	client, err := p.switchToKeySetRows(ctx, params, signer, created)
	if err != nil {
		p.abandonKey(ctx, logger, kms, params.ConnectionID, created.key, signer, err)
		if errors.Is(err, errAlreadyOnMethod) {
			return client, nil
		}
		return nil, err
	}
	return client, nil
}

var (
	// errNoParkedKeySet sends a switch to private_key_jwt down the minting path.
	errNoParkedKeySet = errors.New("identityproviderconnections: no parked key set")

	// errAlreadyOnMethod reports that a concurrent switch already landed the
	// requested method; the caller discards any key it minted and succeeds.
	errAlreadyOnMethod = errors.New("identityproviderconnections: client is already on the requested method")
)

// finishAlreadySwitched records the listing mode and audit for a switch a
// concurrent request already applied, and commits.
func (p *Provisioner) finishAlreadySwitched(ctx context.Context, dbtx pgx.Tx, tq *repo.Queries, params SwitchAuthMethodParams, existing *ManagedClient) error {
	if err := p.setListingMode(ctx, tq, params); err != nil {
		return err
	}
	if err := params.record(ctx, dbtx, existing); err != nil {
		return err
	}
	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit method switch transaction: %w", err)
	}
	return nil
}

// reattachParkedKeySet points the client back at its parked key set.
func (p *Provisioner) reattachParkedKeySet(ctx context.Context, params SwitchAuthMethodParams) (*ManagedClient, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin method switch transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tq := repo.New(dbtx)
	row, existing, err := p.lockForSwitch(ctx, tq, params)
	if err != nil {
		return nil, err
	}
	if existing.AuthMethod == remotesessions.TokenEndpointAuthMethodPrivateKeyJWT {
		if err := p.finishAlreadySwitched(ctx, dbtx, tq, params, existing); err != nil {
			return nil, err
		}
		return existing, nil
	}
	parked, err := tq.ListParkedConnectionKeySets(ctx, repo.ListParkedConnectionKeySetsParams{
		OrganizationID:               params.OrganizationID,
		IdentityProviderConnectionID: conv.ToNullUUID(params.ConnectionID),
	})
	if err != nil {
		return nil, fmt.Errorf("list parked key sets: %w", err)
	}
	if len(parked) == 0 {
		return nil, errNoParkedKeySet
	}

	updated, err := tq.SwitchManagedClientToKeySet(ctx, repo.SwitchManagedClientToKeySetParams{
		JsonWebKeySetID:              conv.ToNullUUID(parked[0].ID),
		ID:                           existing.ClientRowID,
		OrganizationID:               conv.ToPGText(params.OrganizationID),
		IdentityProviderConnectionID: conv.ToNullUUID(params.ConnectionID),
		PlaceholderClientID:          existing.ClientID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrClientIDAlreadySet
	case err != nil:
		return nil, fmt.Errorf("reattach parked key set: %w", err)
	}
	if err := p.setListingMode(ctx, tq, params); err != nil {
		return nil, err
	}
	if err := p.logClientUpdate(ctx, dbtx, params.OrganizationID, params.Actor, params.ActorDisplayName, row.RemoteSessionClient, updated); err != nil {
		return nil, err
	}

	reattached, err := p.lookupManagedClient(ctx, tq, params.OrganizationID, params.ConnectionID)
	if err != nil {
		return nil, err
	}
	if err := params.record(ctx, dbtx, reattached); err != nil {
		return nil, err
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit method switch transaction: %w", err)
	}
	return reattached, nil
}

// RetireParkedKeySets revokes and tombstones the key sets a connection parked
// while switching methods, then retires their KMS material best-effort.
// Returns how many keys were revoked.
func (p *Provisioner) RetireParkedKeySets(ctx context.Context, organizationID string, connectionID uuid.UUID) (int, error) {
	logger := p.logger.With(attr.SlogOrganizationID(organizationID), attr.SlogIdentityProviderConnectionID(connectionID.String()))

	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin parked key set retirement: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	revoked, err := p.retireParkedKeySets(ctx, dbtx, organizationID, connectionID)
	if err != nil {
		return 0, err
	}
	if err := dbtx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit parked key set retirement: %w", err)
	}
	if len(revoked) > 0 {
		p.retireKeyMaterial(ctx, logger, organizationID, revoked)
	}
	return len(revoked), nil
}

// retireParkedKeySets withdraws parked sets inside the caller's transaction and
// returns the revoked keys whose KMS material the caller retires after commit.
func (p *Provisioner) retireParkedKeySets(ctx context.Context, dbtx pgx.Tx, organizationID string, connectionID uuid.UUID) ([]jwksrepo.JsonWebKey, error) {
	parked, err := repo.New(dbtx).ListParkedConnectionKeySets(ctx, repo.ListParkedConnectionKeySetsParams{
		OrganizationID:               organizationID,
		IdentityProviderConnectionID: conv.ToNullUUID(connectionID),
	})
	if err != nil {
		return nil, fmt.Errorf("list parked key sets: %w", err)
	}

	jq := jwksrepo.New(dbtx)
	var revoked []jwksrepo.JsonWebKey
	for _, set := range parked {
		attached, err := repo.New(dbtx).KeySetAttached(ctx, repo.KeySetAttachedParams{
			OrganizationID:  conv.ToPGText(organizationID),
			JsonWebKeySetID: conv.ToNullUUID(set.ID),
		})
		if err != nil {
			return nil, fmt.Errorf("recheck parked key set: %w", err)
		}
		if attached {
			continue
		}
		live, err := jq.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{
			JsonWebKeySetID: set.ID,
			OrganizationID:  organizationID,
			IncludeRevoked:  false,
		})
		if err != nil {
			return nil, fmt.Errorf("list parked keys: %w", err)
		}
		for _, key := range live {
			after, err := jq.RevokeJsonWebKey(ctx, jwksrepo.RevokeJsonWebKeyParams{
				ID:             key.ID,
				OrganizationID: organizationID,
			})
			if err != nil {
				return nil, fmt.Errorf("revoke parked key: %w", err)
			}
			if err := p.audit.LogJsonWebKeyRevoke(ctx, dbtx, keyEvent(organizationID, after, keySnapshot(key), keySnapshot(after))); err != nil {
				return nil, fmt.Errorf("record parked key revocation: %w", err)
			}
			revoked = append(revoked, key)
		}
		if _, err := jq.CascadeSoftDeleteJsonWebKeys(ctx, jwksrepo.CascadeSoftDeleteJsonWebKeysParams{
			JsonWebKeySetID: set.ID,
			OrganizationID:  organizationID,
		}); err != nil {
			return nil, fmt.Errorf("tombstone parked keys: %w", err)
		}
		if _, err := jq.SoftDeleteJsonWebKeySet(ctx, jwksrepo.SoftDeleteJsonWebKeySetParams{
			ID:             set.ID,
			OrganizationID: organizationID,
		}); err != nil {
			return nil, fmt.Errorf("tombstone parked key set: %w", err)
		}
		if err := p.audit.LogJsonWebKeySetDelete(ctx, dbtx, audit.LogJsonWebKeySetDeleteEvent{
			OrganizationID:   organizationID,
			ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Actor:            systemActor(),
			ActorDisplayName: systemActorDisplayName(),
			ActorSlug:        nil,
			SetURN:           urn.NewJsonWebKeySet(set.ID),
			SetName:          set.Name,
		}); err != nil {
			return nil, fmt.Errorf("record parked key set deletion: %w", err)
		}
	}
	return revoked, nil
}

// switchListingOnly records the listing mode for a client already on its method.
func (p *Provisioner) switchListingOnly(ctx context.Context, params SwitchAuthMethodParams) (*ManagedClient, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin method switch transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tq := repo.New(dbtx)
	_, existing, err := p.lockForSwitch(ctx, tq, params)
	if err != nil {
		return nil, err
	}
	if existing.AuthMethod != params.AuthMethod {
		return nil, ErrAuthMethodMismatch
	}
	if err := p.finishAlreadySwitched(ctx, dbtx, tq, params, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (p *Provisioner) switchToKeySetRows(ctx context.Context, params SwitchAuthMethodParams, signer *signingCredential, created *createdKey) (*ManagedClient, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin method switch transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tq := repo.New(dbtx)
	row, existing, err := p.lockForSwitch(ctx, tq, params)
	if err != nil {
		return nil, err
	}
	if existing.AuthMethod == remotesessions.TokenEndpointAuthMethodPrivateKeyJWT {
		if err := p.finishAlreadySwitched(ctx, dbtx, tq, params, existing); err != nil {
			return nil, err
		}
		return existing, errAlreadyOnMethod
	}

	connectionName := providerDisplayNames[params.Provider] + " connection " + params.ConnectionID.String()
	marker := conv.ToNullUUID(params.ConnectionID)

	externalKey, err := p.createManagedExternalKey(ctx, dbtx, params.OrganizationID, marker, signer, connectionName, created)
	if err != nil {
		return nil, err
	}
	if err := requireSigningCredentialUnchanged(ctx, tq, signer); err != nil {
		return nil, err
	}

	jq := jwksrepo.New(dbtx)
	set, err := jq.CreateJsonWebKeySet(ctx, jwksrepo.CreateJsonWebKeySetParams{
		OrganizationID:               params.OrganizationID,
		ExternalKeyID:                externalKey.ID,
		Name:                         connectionName + " keys",
		IdentityProviderConnectionID: marker,
	})
	if err != nil {
		return nil, fmt.Errorf("create managed key set: %w", err)
	}
	key, err := jq.CreateJsonWebKey(ctx, jwksrepo.CreateJsonWebKeyParams{
		OrganizationID:  params.OrganizationID,
		JsonWebKeySetID: set.ID,
		ExternalKeyID:   externalKey.ID,
		State:           "active",
		Kid:             created.kid,
		PublicJwk:       created.publicJWK,
	})
	if err != nil {
		return nil, fmt.Errorf("publish managed key: %w", err)
	}

	updated, err := tq.SwitchManagedClientToKeySet(ctx, repo.SwitchManagedClientToKeySetParams{
		JsonWebKeySetID:              conv.ToNullUUID(set.ID),
		ID:                           existing.ClientRowID,
		OrganizationID:               conv.ToPGText(params.OrganizationID),
		IdentityProviderConnectionID: conv.ToNullUUID(params.ConnectionID),
		PlaceholderClientID:          existing.ClientID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrClientIDAlreadySet
	case err != nil:
		return nil, fmt.Errorf("switch managed client to key set: %w", err)
	}
	if err := p.setListingMode(ctx, tq, params); err != nil {
		return nil, err
	}

	if err := p.audit.LogJsonWebKeySetCreate(ctx, dbtx, audit.LogJsonWebKeySetCreateEvent{
		OrganizationID:   params.OrganizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Actor:            params.Actor,
		ActorDisplayName: params.ActorDisplayName,
		ActorSlug:        nil,
		SetURN:           urn.NewJsonWebKeySet(set.ID),
		SetName:          set.Name,
	}); err != nil {
		return nil, fmt.Errorf("record managed key set creation: %w", err)
	}
	if err := p.audit.LogJsonWebKeyPublish(ctx, dbtx, keyEvent(params.OrganizationID, key, nil, nil)); err != nil {
		return nil, fmt.Errorf("record managed key publication: %w", err)
	}
	if err := p.logClientUpdate(ctx, dbtx, params.OrganizationID, params.Actor, params.ActorDisplayName, row.RemoteSessionClient, updated); err != nil {
		return nil, err
	}
	switched := &ManagedClient{
		ClientRowID:           updated.ID,
		ClientID:              updated.ClientID,
		IssuerID:              updated.RemoteSessionIssuerID,
		AuthMethod:            remotesessions.TokenEndpointAuthMethodPrivateKeyJWT,
		ClientSecretEncrypted: "",
		JSONWebKeySetID:       conv.ToNullUUID(set.ID),
		ExternalKeyID:         conv.ToNullUUID(externalKey.ID),
		ActiveKeyID:           conv.ToNullUUID(key.ID),
		ActiveKid:             key.Kid,
		ActivatedAt:           key.ActivatedAt.Time,
		JSONWebKeySetURL:      remotesessions.ClientJSONWebKeySetURL(p.cfg.ServerURL, updated.ID),
	}
	if err := params.record(ctx, dbtx, switched); err != nil {
		return nil, err
	}

	if err := commitPublication(ctx, dbtx, params.OrganizationID); err != nil {
		return nil, err
	}
	return switched, nil
}

// logClientUpdate audits a managed client change; the snapshots carry no secret.
func (p *Provisioner) logClientUpdate(ctx context.Context, dbtx pgx.Tx, organizationID string, actor urn.Principal, actorDisplayName *string, before, after repo.RemoteSessionClient) error {
	beforeView, err := mv.BuildRemoteSessionClientView(remotesessionsrepo.RemoteSessionClient(before), nil)
	if err != nil {
		return fmt.Errorf("build managed client snapshot: %w", err)
	}
	afterView, err := mv.BuildRemoteSessionClientView(remotesessionsrepo.RemoteSessionClient(after), nil)
	if err != nil {
		return fmt.Errorf("build managed client snapshot: %w", err)
	}
	if err := p.audit.LogRemoteSessionClientUpdate(ctx, dbtx, audit.LogRemoteSessionClientUpdateEvent{
		OrganizationID:         organizationID,
		ProjectID:              uuid.Nil,
		Actor:                  actor,
		ActorDisplayName:       actorDisplayName,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(after.ID),
		ClientID:               after.ClientID,
		SnapshotBefore:         beforeView,
		SnapshotAfter:          afterView,
	}); err != nil {
		return fmt.Errorf("record managed client update: %w", err)
	}
	return nil
}

// RevokeClient withdraws every key from the connection's managed set and
// returns how many were revoked. The client and set stay live so the JWKS
// document serves an empty set. Works on a soft-deleted connection too. Once
// the rows are committed, each key's KMS version is disabled and the signer's
// grant on it withdrawn, best-effort. A client-secret client has its secret
// cleared instead. Key sets parked by a method switch are retired either way.
func (p *Provisioner) RevokeClient(ctx context.Context, organizationID string, connectionID uuid.UUID) (int, error) {
	logger := p.logger.With(attr.SlogOrganizationID(organizationID), attr.SlogIdentityProviderConnectionID(connectionID.String()))
	q := repo.New(p.db)

	if _, err := q.GetIdentityProviderConnectionIncludingDeleted(ctx, repo.GetIdentityProviderConnectionIncludingDeletedParams{
		ID:             connectionID,
		OrganizationID: organizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrConnectionNotFound
		}
		return 0, fmt.Errorf("load identity provider connection: %w", err)
	}

	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin revocation transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, existing, err := p.lookupManagedClientRow(ctx, repo.New(dbtx), organizationID, connectionID)
	if err != nil {
		return 0, err
	}
	parked, err := p.retireParkedKeySets(ctx, dbtx, organizationID, connectionID)
	if err != nil {
		return 0, err
	}
	if existing.AuthMethod == remotesessions.TokenEndpointAuthMethodBasic {
		if err := p.clearClientSecret(ctx, dbtx, organizationID, connectionID, row.RemoteSessionClient); err != nil {
			return 0, err
		}
		if err := dbtx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("commit revocation transaction: %w", err)
		}
		if len(parked) > 0 {
			p.retireKeyMaterial(ctx, logger, organizationID, parked)
		}
		return len(parked), nil
	}
	if !existing.JSONWebKeySetID.Valid {
		return 0, ErrNoKeySet
	}

	jq := jwksrepo.New(dbtx)
	set, err := jq.LockJsonWebKeySetForKeyWrite(ctx, jwksrepo.LockJsonWebKeySetForKeyWriteParams{
		ID:             existing.JSONWebKeySetID.UUID,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, ErrNotProvisioned
	case err != nil:
		return 0, fmt.Errorf("lock managed key set: %w", err)
	}

	live, err := jq.ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{
		JsonWebKeySetID: set.ID,
		OrganizationID:  organizationID,
		IncludeRevoked:  false,
	})
	if err != nil {
		return 0, fmt.Errorf("list managed keys: %w", err)
	}

	for _, key := range live {
		revoked, err := jq.RevokeJsonWebKey(ctx, jwksrepo.RevokeJsonWebKeyParams{
			ID:             key.ID,
			OrganizationID: organizationID,
		})
		if err != nil {
			return 0, fmt.Errorf("revoke managed key: %w", err)
		}
		if err := p.audit.LogJsonWebKeyRevoke(ctx, dbtx, keyEvent(organizationID, revoked, keySnapshot(key), keySnapshot(revoked))); err != nil {
			return 0, fmt.Errorf("record managed key revocation: %w", err)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit revocation transaction: %w", err)
	}

	retired := slices.Concat(live, parked)
	if len(retired) > 0 {
		p.retireKeyMaterial(ctx, logger, organizationID, retired)
	}

	return len(retired), nil
}

// ClearClientSecret withdraws a client-secret client's credential inside the
// caller's transaction; a secret that is already cleared is left as is.
func (p *Provisioner) ClearClientSecret(ctx context.Context, dbtx pgx.Tx, organizationID string, connectionID uuid.UUID) error {
	row, existing, err := p.lookupManagedClientRow(ctx, repo.New(dbtx), organizationID, connectionID)
	if err != nil {
		return err
	}
	if existing.AuthMethod != remotesessions.TokenEndpointAuthMethodBasic {
		return ErrClientSecretNotAccepted
	}
	return p.clearClientSecret(ctx, dbtx, organizationID, connectionID, row.RemoteSessionClient)
}

func (p *Provisioner) clearClientSecret(ctx context.Context, dbtx pgx.Tx, organizationID string, connectionID uuid.UUID, before repo.RemoteSessionClient) error {
	if !before.ClientSecretEncrypted.Valid {
		return nil
	}
	cleared, err := repo.New(dbtx).ClearManagedClientSecret(ctx, repo.ClearManagedClientSecretParams{
		ID:                           before.ID,
		OrganizationID:               conv.ToPGText(organizationID),
		IdentityProviderConnectionID: conv.ToNullUUID(connectionID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotProvisioned
	case err != nil:
		return fmt.Errorf("clear managed client secret: %w", err)
	}
	return p.logClientUpdate(ctx, dbtx, organizationID, systemActor(), systemActorDisplayName(), before, cleared)
}

// retireKeyMaterial disables the KMS version behind each revoked key and
// withdraws the signer's grant on it. Rows are already committed, so every
// failure is logged for manual cleanup rather than returned.
func (p *Provisioner) retireKeyMaterial(ctx context.Context, logger *slog.Logger, organizationID string, revoked []jwksrepo.JsonWebKey) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), keyCleanupTimeout)
	defer cancel()

	kms, err := p.openKMSClient(cleanupCtx)
	if err != nil {
		logger.ErrorContext(ctx, "failed to open kms client after revocation; disable the revoked key versions by hand", attr.SlogError(err))
		return
	}
	defer o11y.LogDefer(ctx, logger, "failed to close gcp kms client", func() error { return kms.Close() })

	eq := extkeysrepo.New(p.db)
	q := repo.New(p.db)
	for _, key := range revoked {
		row, err := eq.GetGcpKmsKey(cleanupCtx, extkeysrepo.GetGcpKmsKeyParams{
			ID:             key.ExternalKeyID,
			OrganizationID: conv.ToPGText(organizationID),
		})
		if err != nil {
			logger.ErrorContext(ctx, "failed to load revoked key resource; disable its kms version by hand", attr.SlogError(err))
			continue
		}
		attrs := []any{attr.SlogGcpKmsKeyVersion(row.GcpKmsKey.ResourceName)}

		if err := kms.DisableKeyVersion(cleanupCtx, row.GcpKmsKey.ResourceName); err != nil {
			logger.ErrorContext(ctx, "failed to disable revoked kms key version; disable it by hand", append(attrs, attr.SlogError(err))...)
		}

		keyName, err := gcpkms.KeyNameForVersion(row.GcpKmsKey.ResourceName)
		if err != nil {
			logger.ErrorContext(ctx, "failed to derive revoked kms key name; remove the signer grant by hand", append(attrs, attr.SlogError(err))...)
			continue
		}
		// The signer is the platform credential the key was minted under.
		signer, err := q.GetPlatformGcpIamCredentialForProvisioning(cleanupCtx, row.ExternalKey.ExternalCredentialID)
		if err != nil || signer.GcpIamCredential.ImpersonateServiceAccount.String == "" {
			logger.ErrorContext(ctx, "revoked key has no live signing credential; remove the signer grant by hand", append(attrs, attr.SlogError(err))...)
			continue
		}
		if err := kms.RevokeSignerVerifier(cleanupCtx, keyName, signer.GcpIamCredential.ImpersonateServiceAccount.String); err != nil {
			logger.ErrorContext(ctx, "failed to revoke signer grant on revoked kms key; remove it by hand", append(attrs, attr.SlogError(err))...)
		}
	}
}

// ClientJSONWebKeySetURL returns the public URL of a connection's client key
// set, or ErrNotProvisioned.
func (p *Provisioner) ClientJSONWebKeySetURL(ctx context.Context, organizationID string, connectionID uuid.UUID) (string, error) {
	client, err := p.lookupManagedClient(ctx, repo.New(p.db), organizationID, connectionID)
	if err != nil {
		return "", err
	}
	if !client.JSONWebKeySetID.Valid {
		return "", ErrNoKeySet
	}

	return client.JSONWebKeySetURL, nil
}

// GetManagedClient returns the connection's managed registration, or ErrNotProvisioned.
func (p *Provisioner) GetManagedClient(ctx context.Context, organizationID string, connectionID uuid.UUID) (*ManagedClient, error) {
	return p.GetManagedClientTx(ctx, p.db, organizationID, connectionID)
}

// GetManagedClientTx is GetManagedClient on the caller's connection or transaction.
func (p *Provisioner) GetManagedClientTx(ctx context.Context, dbtx repo.DBTX, organizationID string, connectionID uuid.UUID) (*ManagedClient, error) {
	return p.lookupManagedClient(ctx, repo.New(dbtx), organizationID, connectionID)
}

// adoptedError carries a concurrently provisioned client out of provisionRows.
type adoptedError struct {
	client *ManagedClient
}

func (e *adoptedError) Error() string { return "connection was provisioned concurrently" }

func validateProvider(provider string) error {
	if _, ok := providerDisplayNames[provider]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownProvider, provider)
	}

	return nil
}

// requireConnection confirms the connection is live, in the organization, and
// for the provider.
func requireConnection(ctx context.Context, q *repo.Queries, organizationID string, connectionID uuid.UUID, provider string) error {
	connection, err := q.GetIdentityProviderConnection(ctx, repo.GetIdentityProviderConnectionParams{
		ID:             connectionID,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrConnectionNotFound
	case err != nil:
		return fmt.Errorf("load identity provider connection: %w", err)
	}

	if connection.Provider != provider {
		return fmt.Errorf("%w: %s", ErrConnectionProviderMismatch, connection.Provider)
	}

	return nil
}

// loadIssuer reads the organization-level issuer and requires a token endpoint.
func (p *Provisioner) loadIssuer(ctx context.Context, q *repo.Queries, organizationID string, issuerID uuid.UUID) (*repo.RemoteSessionIssuer, error) {
	issuer, err := q.GetOrganizationRemoteSessionIssuerForProvisioning(ctx, repo.GetOrganizationRemoteSessionIssuerForProvisioningParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(organizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrIssuerNotFound
	case err != nil:
		return nil, fmt.Errorf("load issuer for provisioning: %w", err)
	}
	if issuer.TokenEndpoint.String == "" {
		return nil, ErrIssuerHasNoTokenEndpoint
	}
	if !urls.IsAbsoluteHTTPSOrLoopback(issuer.TokenEndpoint.String) {
		return nil, ErrIssuerTokenEndpointNotHTTPS
	}

	return &issuer, nil
}

// lookupManagedClient reads the connection's managed rows, or reports ErrNotProvisioned.
func (p *Provisioner) lookupManagedClient(ctx context.Context, q *repo.Queries, organizationID string, connectionID uuid.UUID) (*ManagedClient, error) {
	_, client, err := p.lookupManagedClientRow(ctx, q, organizationID, connectionID)
	return client, err
}

func (p *Provisioner) lookupManagedClientRow(ctx context.Context, q *repo.Queries, organizationID string, connectionID uuid.UUID) (repo.GetManagedClientRow, *ManagedClient, error) {
	row, err := q.GetManagedClient(ctx, repo.GetManagedClientParams{
		OrganizationID:               conv.ToPGText(organizationID),
		IdentityProviderConnectionID: conv.ToNullUUID(connectionID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return row, nil, ErrNotProvisioned
	case err != nil:
		return row, nil, fmt.Errorf("load managed client: %w", err)
	}

	method := remotesessions.TokenEndpointAuthMethod(row.RemoteSessionClient.TokenEndpointAuthMethod.String)
	jwksURL := ""
	if row.JsonWebKeySetID.Valid {
		jwksURL = remotesessions.ClientJSONWebKeySetURL(p.cfg.ServerURL, row.RemoteSessionClient.ID)
	}
	return row, &ManagedClient{
		ClientRowID:           row.RemoteSessionClient.ID,
		ClientID:              row.RemoteSessionClient.ClientID,
		IssuerID:              row.RemoteSessionClient.RemoteSessionIssuerID,
		AuthMethod:            method,
		ClientSecretEncrypted: row.RemoteSessionClient.ClientSecretEncrypted.String,
		JSONWebKeySetID:       row.JsonWebKeySetID,
		ExternalKeyID:         row.ExternalKeyID,
		ActiveKeyID:           row.JsonWebKeyID,
		ActiveKid:             row.Kid.String,
		ActivatedAt:           row.ActivatedAt.Time,
		JSONWebKeySetURL:      jwksURL,
	}, nil
}

// ProbeSigningCredential resolves and screens the platform signing credential without minting anything.
func (p *Provisioner) ProbeSigningCredential(ctx context.Context) error {
	_, err := p.resolveSigningCredential(ctx, p.logger, repo.New(p.db))
	return err
}

// createManagedExternalKey writes the external_keys and gcp_kms_keys rows for a created key.
func (p *Provisioner) createManagedExternalKey(ctx context.Context, dbtx pgx.Tx, organizationID string, marker uuid.NullUUID, signer *signingCredential, connectionName string, created *createdKey) (*extkeysrepo.ExternalKey, error) {
	eq := extkeysrepo.New(dbtx)
	externalKey, err := eq.CreateExternalKey(ctx, extkeysrepo.CreateExternalKeyParams{
		OrganizationID:               conv.ToPGText(organizationID),
		ExternalCredentialID:         signer.credentialID,
		Provider:                     "gcp_kms",
		Algorithm:                    string(signingAlgorithm),
		Name:                         connectionName + " signing key",
		CustomerGrantReference:       pgtype.Text{String: "", Valid: false},
		IdentityProviderConnectionID: marker,
	})
	if err != nil {
		return nil, fmt.Errorf("create managed external key: %w", err)
	}

	if _, err := eq.CreateGcpKmsKey(ctx, extkeysrepo.CreateGcpKmsKeyParams{
		ExternalKeyID: externalKey.ID,
		ResourceName:  created.key.KeyVersionName,
	}); err != nil {
		return nil, fmt.Errorf("create managed gcp kms key: %w", err)
	}

	return &externalKey, nil
}

// signingCredential is the platform credential as screened for use.
type signingCredential struct {
	credentialID uuid.UUID
	credential   gcpauth.Credential

	// identity is the stored row the screening ran on, re-checked under the
	// write transaction.
	identity repo.GcpIamCredential
}

// resolveSigningCredential loads the platform-tier credential and screens it.
func (p *Provisioner) resolveSigningCredential(ctx context.Context, logger *slog.Logger, q *repo.Queries) (*signingCredential, error) {
	row, err := q.GetPlatformGcpIamCredentialForProvisioning(ctx, p.cfg.SigningCredentialID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("%w: platform credential not found", ErrSigningCredentialUnusable)
	case err != nil:
		return nil, fmt.Errorf("load platform signing credential: %w", err)
	}

	if want := p.cfg.SigningServiceAccount; want != "" {
		if got := strings.TrimSpace(row.GcpIamCredential.ImpersonateServiceAccount.String); got != "" && !strings.EqualFold(got, want) {
			return nil, fmt.Errorf("%w: credential impersonates %s, configured signer is %s", ErrSigningCredentialUnusable, got, want)
		}
	}
	credential, problem, detail, err := p.gcpIdentity.ScreenStoredCredential(ctx, logger, gcpauth.StoredCredential{
		Present:                   true,
		ImpersonateServiceAccount: row.GcpIamCredential.ImpersonateServiceAccount.String,
		HasWifConfig:              row.GcpIamCredential.WifPoolID.Valid || row.GcpIamCredential.WifProviderID.Valid || row.GcpIamCredential.WifProjectNumber.Valid,
		SkipProjectVerification:   row.GcpIamCredential.SkipProjectVerification,
	})
	if err != nil {
		return nil, fmt.Errorf("screen platform signing credential: %w", err)
	}
	if problem != "" {
		return nil, fmt.Errorf("%w: %s", ErrSigningCredentialUnusable, detail)
	}
	// A screened credential can still lack the impersonation grant; prove it before creating a key.
	if _, err := p.gcpIdentity.ResolvePrincipal(ctx, credential); err != nil {
		return nil, fmt.Errorf("%w: cannot assume the signing identity: %w", ErrSigningCredentialUnusable, err)
	}

	return &signingCredential{credentialID: row.ExternalCredential.ID, credential: credential, identity: row.GcpIamCredential}, nil
}

// requireSigningCredentialUnchanged re-reads the platform credential inside the
// write transaction, after the managed key insert took FOR KEY SHARE on it, so
// a platform mutation that held the row FOR UPDATE cannot commit underneath the
// screened identity.
func requireSigningCredentialUnchanged(ctx context.Context, q *repo.Queries, signer *signingCredential) error {
	row, err := q.GetPlatformGcpIamCredentialForProvisioning(ctx, signer.credentialID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%w: platform credential was deleted during provisioning", ErrSigningCredentialUnusable)
	case err != nil:
		return fmt.Errorf("re-read platform signing credential: %w", err)
	}
	if !sameSigningIdentity(row.GcpIamCredential, signer.identity) {
		return fmt.Errorf("%w: platform credential changed during provisioning", ErrSigningCredentialUnusable)
	}

	return nil
}

func sameSigningIdentity(a, b repo.GcpIamCredential) bool {
	return a.ImpersonateServiceAccount == b.ImpersonateServiceAccount &&
		a.WifPoolID == b.WifPoolID &&
		a.WifProviderID == b.WifProviderID &&
		a.WifProjectNumber == b.WifProjectNumber &&
		a.SkipProjectVerification == b.SkipProjectVerification
}

// createdKey is a KMS key plus the JWK minted from its public half.
type createdKey struct {
	key       *gcpkms.CreatedSigningKey
	kid       string
	publicJWK []byte
}

// openKMSClient builds a KMS client under Speakeasy's own identity, which creates
// keys; the signing identity only ever holds rights on keys it was granted.
func (p *Provisioner) openKMSClient(ctx context.Context) (gcpkms.ProvisioningClient, error) {
	ambient, err := p.gcpIdentity.TokenSource(ctx, gcpauth.Credential{
		ImpersonateServiceAccount: "",
		WifPoolID:                 "",
		WifProviderID:             "",
		WifProjectNumber:          "",
	})
	if err != nil {
		return nil, fmt.Errorf("resolve gram's own identity for key creation: %w", err)
	}

	client, err := p.kmsClients(ctx, ambient)
	if err != nil {
		return nil, fmt.Errorf("build kms client for key creation: %w", err)
	}

	return client, nil
}

// createSigningKey creates one KMS key, grants the signer on it alone, and
// mints the JWK to publish. A key left unusable by a later step is disabled.
func (p *Provisioner) createSigningKey(ctx context.Context, logger *slog.Logger, client gcpkms.ProvisioningClient, provider string, connectionID uuid.UUID, signer *signingCredential) (*createdKey, error) {
	keyID, err := gcpkms.NewSigningKeyID(provider)
	if err != nil {
		return nil, fmt.Errorf("name managed signing key: %w", err)
	}

	created, err := client.CreateSigningKey(ctx, gcpkms.CreateSigningKeyParams{
		KeyRing:   p.cfg.KeyRing,
		KeyID:     keyID,
		Algorithm: signingAlgorithm,
	})
	if err != nil {
		return nil, fmt.Errorf("create managed signing key: %w", err)
	}

	result, err := p.finishSigningKey(ctx, client, created, signer)
	if err != nil {
		p.abandonKey(ctx, logger, client, connectionID, created, signer, err)
		return nil, err
	}

	return result, nil
}

func (p *Provisioner) finishSigningKey(ctx context.Context, client gcpkms.ProvisioningClient, created *gcpkms.CreatedSigningKey, signer *signingCredential) (*createdKey, error) {
	if err := client.GrantSignerVerifier(ctx, created.KeyName, signer.credential.ImpersonateServiceAccount); err != nil {
		return nil, fmt.Errorf("grant signer on managed key: %w", err)
	}

	public, err := client.GetPublicKey(ctx, created.KeyVersionName)
	if err != nil {
		return nil, fmt.Errorf("read managed key public half: %w", err)
	}
	if public.Algorithm != signingAlgorithm {
		return nil, fmt.Errorf("managed key signs with %s, expected %s", public.Algorithm, signingAlgorithm)
	}

	kid, doc, err := jsonwebkeysets.BuildPublishedJWK(public)
	if err != nil {
		return nil, fmt.Errorf("build managed key jwk: %w", err)
	}

	return &createdKey{key: created, kid: kid, publicJWK: doc}, nil
}

// publicationCommitError must never lead to unconditional KMS cleanup: COMMIT
// can succeed on the server even if its response is lost.
type publicationCommitError struct {
	organizationID string
	err            error
}

func (e *publicationCommitError) Error() string { return "commit key publication: " + e.err.Error() }
func (e *publicationCommitError) Unwrap() error { return e.err }

func commitPublication(ctx context.Context, tx pgx.Tx, organizationID string) error {
	if err := tx.Commit(ctx); err != nil {
		return &publicationCommitError{organizationID: organizationID, err: err}
	}
	return nil
}

// unobservedPublicationError classifies a failure after a successful commit
// as an uncertain publication, so abandonKey reconciles before disabling.
func unobservedPublicationError(organizationID string, err error) error {
	return &publicationCommitError{organizationID: organizationID, err: err}
}

func reconcilePublication(ctx context.Context, check func(context.Context) (bool, error)) (bool, error) {
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), keyCleanupTimeout)
	defer cancel()
	absent, err := check(checkCtx)
	if err != nil {
		return false, fmt.Errorf("reconcile publication outcome: %w", err)
	}
	return absent, nil
}

func (p *Provisioner) publicationAbsent(ctx context.Context, organizationID string, connectionID uuid.UUID, resourceName string) (bool, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin publication reconciliation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	// Wait for the original transaction to resolve before interpreting absence.
	// A missing/deleted connection is inconclusive, so the caller preserves the key.
	if _, err := q.LockIdentityProviderConnectionForProvisioning(ctx, repo.LockIdentityProviderConnectionForProvisioningParams{ID: connectionID, OrganizationID: organizationID}); err != nil {
		return false, fmt.Errorf("lock connection for publication reconciliation: %w", err)
	}
	referenced, err := q.ManagedKeyResourceExists(ctx, resourceName)
	if err != nil {
		return false, fmt.Errorf("lookup published key resource: %w", err)
	}
	return !referenced, nil
}

// abandonKey logs a key no row will reference, best-effort disables it, and
// withdraws the signer's grant on it.
func (p *Provisioner) abandonKey(ctx context.Context, logger *slog.Logger, client gcpkms.ProvisioningClient, connectionID uuid.UUID, created *gcpkms.CreatedSigningKey, signer *signingCredential, cause error) {
	attrs := []any{
		attr.SlogGcpKmsKeyVersion(created.KeyVersionName),
		attr.SlogIdentityProviderConnectionID(connectionID.String()),
	}

	if uncertain, ok := errors.AsType[*publicationCommitError](cause); ok {
		absent, err := reconcilePublication(ctx, func(checkCtx context.Context) (bool, error) {
			return p.publicationAbsent(checkCtx, uncertain.organizationID, connectionID, created.KeyVersionName)
		})
		if err != nil || !absent {
			logger.ErrorContext(ctx, "preserving kms key after uncertain publication commit; key may be referenced", append(attrs, attr.SlogError(errors.Join(cause, err)))...)
			return
		}
	}
	if _, ok := errors.AsType[*adoptedError](cause); ok {
		logger.WarnContext(ctx, "connection was provisioned concurrently; adopting the existing client and disabling the new kms key", attrs...)
	} else {
		logger.ErrorContext(ctx, "provisioning failed after creating a kms key; disabling it", append(attrs, attr.SlogError(cause))...)
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), keyCleanupTimeout)
	defer cancel()

	if err := client.DisableKeyVersion(cleanupCtx, created.KeyVersionName); err != nil {
		logger.ErrorContext(ctx, "failed to disable abandoned kms key version; disable it by hand", append(attrs, attr.SlogError(err))...)
	}
	if signer != nil {
		if err := client.RevokeSignerVerifier(cleanupCtx, created.KeyName, signer.credential.ImpersonateServiceAccount); err != nil {
			logger.ErrorContext(ctx, "failed to revoke signer grant on abandoned kms key; remove it by hand", append(attrs, attr.SlogError(err))...)
		}
	}
}

func systemActor() urn.Principal {
	return urn.NewSystemPrincipal(systemActorComponent)
}

func systemActorDisplayName() *string {
	name := "System"
	return &name
}

func keySnapshot(key jwksrepo.JsonWebKey) *audit.JsonWebKeySnapshot {
	return &audit.JsonWebKeySnapshot{
		Kid:           key.Kid,
		State:         key.State,
		ExternalKeyID: key.ExternalKeyID.String(),
	}
}

func keyEvent(organizationID string, key jwksrepo.JsonWebKey, before, after *audit.JsonWebKeySnapshot) audit.LogJsonWebKeyEvent {
	return audit.LogJsonWebKeyEvent{
		OrganizationID:    organizationID,
		ProjectID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Actor:             systemActor(),
		ActorDisplayName:  systemActorDisplayName(),
		ActorSlug:         nil,
		KeyURN:            urn.NewJsonWebKey(key.ID),
		Kid:               key.Kid,
		SetURN:            urn.NewJsonWebKeySet(key.JsonWebKeySetID),
		KeySnapshotBefore: before,
		KeySnapshotAfter:  after,
	}
}
