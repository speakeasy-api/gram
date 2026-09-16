// Package identityproviderconnections provisions, rotates, and revokes the
// managed signing credential behind an identity provider connection.
package identityproviderconnections

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
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
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ProviderOkta is the identity_provider_connections.provider value for Okta.
const ProviderOkta = "okta"

// providerDisplayNames doubles as the set of known providers.
var providerDisplayNames = map[string]string{
	ProviderOkta: "Okta",
}

// signingAlgorithm is what every managed key signs with.
const signingAlgorithm = jose.RS256

// systemActorComponent names the provisioner in audit entries.
const systemActorComponent = "identity-provider-connections"

// keyCleanupTimeout bounds the best-effort disable of an abandoned KMS key.
const keyCleanupTimeout = 30 * time.Second

var (
	ErrConnectionNotFound         = errors.New("identityproviderconnections: connection not found")
	ErrConnectionProviderMismatch = errors.New("identityproviderconnections: connection provider mismatch")
	ErrUnknownProvider            = errors.New("identityproviderconnections: unknown provider")
	ErrIssuerNotFound             = errors.New("identityproviderconnections: issuer not found")
	ErrIssuerHasNoTokenEndpoint   = errors.New("identityproviderconnections: issuer has no token endpoint")
	ErrSigningCredentialUnusable  = errors.New("identityproviderconnections: signing credential unusable")
	ErrNotProvisioned             = errors.New("identityproviderconnections: connection is not provisioned")
	ErrKeyAlreadyPublished        = errors.New("identityproviderconnections: key material was already published into the set")
)

// Config carries the deployment settings provisioning needs.
type Config struct {
	// KeyRing is the Speakeasy-owned GCP KMS key ring, fully qualified.
	KeyRing string

	// SigningCredentialID is the platform-tier gcp_iam credential that signs.
	SigningCredentialID uuid.UUID

	// ServerURL is the origin managed JWKS documents are served from.
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
}

// RotateClientParams names the connection whose key rotates.
type RotateClientParams struct {
	OrganizationID string
	ConnectionID   uuid.UUID
	Provider       string
}

// ManagedClient describes a connection's managed registration.
type ManagedClient struct {
	// ClientRowID is the remote_session_clients primary key and JWKS address.
	ClientRowID uuid.UUID

	// ClientID is a placeholder until the administrator submits the real one.
	ClientID string

	IssuerID        uuid.UUID
	JSONWebKeySetID uuid.UUID

	// ExternalKeyID is the external key currently backing the set.
	ExternalKeyID uuid.UUID

	// ActiveKeyID, ActiveKid, and ActivatedAt are empty after revocation.
	ActiveKeyID uuid.NullUUID
	ActiveKid   string
	ActivatedAt time.Time

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
		return existing, nil
	}

	if _, err := p.loadIssuer(ctx, q, params.OrganizationID, params.IssuerID); err != nil {
		return nil, err
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
		p.abandonKey(ctx, logger, kms, params.ConnectionID, created.key, err)

		if adopted, ok := errors.AsType[*adoptedError](err); ok {
			return adopted.client, nil
		}
		return nil, err
	}

	return client, nil
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
		ClientID:                        params.Provider + "-pending-" + params.ConnectionID.String(),
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
	}); err != nil {
		return nil, fmt.Errorf("record managed client creation: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit provisioning transaction: %w", err)
	}

	return &ManagedClient{
		ClientRowID:      client.ID,
		ClientID:         client.ClientID,
		IssuerID:         client.RemoteSessionIssuerID,
		JSONWebKeySetID:  set.ID,
		ExternalKeyID:    externalKey.ID,
		ActiveKeyID:      conv.ToNullUUID(key.ID),
		ActiveKid:        key.Kid,
		ActivatedAt:      key.ActivatedAt.Time,
		JSONWebKeySetURL: remotesessions.ClientJSONWebKeySetURL(p.cfg.ServerURL, client.ID),
	}, nil
}

// RotateClient creates a new managed KMS key, publishes it as the set's active
// key, and retires the previous one. Retired keys stay in the JWKS document.
func (p *Provisioner) RotateClient(ctx context.Context, params RotateClientParams) (*ManagedClient, error) {
	logger := p.logger.With(attr.SlogOrganizationID(params.OrganizationID))
	q := repo.New(p.db)

	if err := validateProvider(params.Provider); err != nil {
		return nil, err
	}
	if err := requireConnection(ctx, q, params.OrganizationID, params.ConnectionID, params.Provider); err != nil {
		return nil, err
	}
	if _, err := p.lookupManagedClient(ctx, q, params.OrganizationID, params.ConnectionID); err != nil {
		return nil, err
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

	if err := p.rotateRows(ctx, params, signer, created); err != nil {
		p.abandonKey(ctx, logger, kms, params.ConnectionID, created.key, err)
		return nil, err
	}

	return p.lookupManagedClient(ctx, q, params.OrganizationID, params.ConnectionID)
}

// rotateRows publishes the new key and retires the active one in one
// transaction, in the order the key set lifecycle uses.
func (p *Provisioner) rotateRows(ctx context.Context, params RotateClientParams, signer *signingCredential, created *createdKey) error {
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

	jq := jwksrepo.New(dbtx)
	set, err := jq.LockJsonWebKeySetForKeyWrite(ctx, jwksrepo.LockJsonWebKeySetForKeyWriteParams{
		ID:             existing.JSONWebKeySetID,
		OrganizationID: params.OrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotProvisioned
	case err != nil:
		return fmt.Errorf("lock managed key set: %w", err)
	}

	kidExists, err := jq.JsonWebKeyKidExistsInSet(ctx, jwksrepo.JsonWebKeyKidExistsInSetParams{
		JsonWebKeySetID: set.ID,
		OrganizationID:  params.OrganizationID,
		Kid:             created.kid,
	})
	if err != nil {
		return fmt.Errorf("check managed kid: %w", err)
	}
	if kidExists {
		return ErrKeyAlreadyPublished
	}

	connectionName := providerDisplayNames[params.Provider] + " connection " + params.ConnectionID.String()
	marker := conv.ToNullUUID(params.ConnectionID)

	externalKey, err := p.createManagedExternalKey(ctx, dbtx, params.OrganizationID, marker, signer, connectionName, created)
	if err != nil {
		return err
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
		return fmt.Errorf("publish rotated key: %w", err)
	}
	if err := p.audit.LogJsonWebKeyPublish(ctx, dbtx, keyEvent(params.OrganizationID, key, nil, nil)); err != nil {
		return fmt.Errorf("record rotated key publication: %w", err)
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
		ExternalKeyID:  externalKey.ID,
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

// RevokeClient withdraws every key from the connection's managed set and
// returns how many were revoked. The client and set stay live so the JWKS
// document serves an empty set. Works on a soft-deleted connection too.
func (p *Provisioner) RevokeClient(ctx context.Context, organizationID string, connectionID uuid.UUID) (int, error) {
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

	existing, err := p.lookupManagedClient(ctx, repo.New(dbtx), organizationID, connectionID)
	if err != nil {
		return 0, err
	}

	jq := jwksrepo.New(dbtx)
	set, err := jq.LockJsonWebKeySetForKeyWrite(ctx, jwksrepo.LockJsonWebKeySetForKeyWriteParams{
		ID:             existing.JSONWebKeySetID,
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

	return len(live), nil
}

// ClientJSONWebKeySetURL returns the public URL of a connection's client key
// set, or ErrNotProvisioned.
func (p *Provisioner) ClientJSONWebKeySetURL(ctx context.Context, organizationID string, connectionID uuid.UUID) (string, error) {
	client, err := p.lookupManagedClient(ctx, repo.New(p.db), organizationID, connectionID)
	if err != nil {
		return "", err
	}

	return client.JSONWebKeySetURL, nil
}

// GetManagedClient returns the connection's managed registration, or ErrNotProvisioned.
func (p *Provisioner) GetManagedClient(ctx context.Context, organizationID string, connectionID uuid.UUID) (*ManagedClient, error) {
	return p.lookupManagedClient(ctx, repo.New(p.db), organizationID, connectionID)
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

	return &issuer, nil
}

// lookupManagedClient reads the connection's managed rows, or reports ErrNotProvisioned.
func (p *Provisioner) lookupManagedClient(ctx context.Context, q *repo.Queries, organizationID string, connectionID uuid.UUID) (*ManagedClient, error) {
	row, err := q.GetManagedClient(ctx, repo.GetManagedClientParams{
		OrganizationID:               conv.ToPGText(organizationID),
		IdentityProviderConnectionID: conv.ToNullUUID(connectionID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNotProvisioned
	case err != nil:
		return nil, fmt.Errorf("load managed client: %w", err)
	}

	return &ManagedClient{
		ClientRowID:      row.RemoteSessionClient.ID,
		ClientID:         row.RemoteSessionClient.ClientID,
		IssuerID:         row.RemoteSessionClient.RemoteSessionIssuerID,
		JSONWebKeySetID:  row.JsonWebKeySetID,
		ExternalKeyID:    row.ExternalKeyID,
		ActiveKeyID:      row.JsonWebKeyID,
		ActiveKid:        row.Kid.String,
		ActivatedAt:      row.ActivatedAt.Time,
		JSONWebKeySetURL: remotesessions.ClientJSONWebKeySetURL(p.cfg.ServerURL, row.RemoteSessionClient.ID),
	}, nil
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

	return &signingCredential{credentialID: row.ExternalCredential.ID, credential: credential}, nil
}

// createdKey is a KMS key plus the JWK minted from its public half.
type createdKey struct {
	key       *gcpkms.CreatedSigningKey
	kid       string
	publicJWK []byte
}

// openKMSClient builds a KMS client under Gram's own identity, which creates
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
		p.abandonKey(ctx, logger, client, connectionID, created, err)
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

// abandonKey logs a key no row will reference and best-effort disables it.
func (p *Provisioner) abandonKey(ctx context.Context, logger *slog.Logger, client gcpkms.ProvisioningClient, connectionID uuid.UUID, created *gcpkms.CreatedSigningKey, cause error) {
	attrs := []any{
		attr.SlogGcpKmsKeyVersion(created.KeyVersionName),
		attr.SlogIdentityProviderConnectionID(connectionID.String()),
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
