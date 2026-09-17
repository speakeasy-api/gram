package identityproviderconnections_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	extcredrepo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	extkeysrepo "github.com/speakeasy-api/gram/server/internal/externalkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

const tokenEndpoint = provisiontest.TokenEndpoint

var noProject = uuid.NullUUID{UUID: uuid.Nil, Valid: false}

func oktaParams(orgID string, connectionID, issuerID uuid.UUID) identityproviderconnections.ProvisionClientParams {
	return identityproviderconnections.ProvisionClientParams{
		OrganizationID: orgID,
		ConnectionID:   connectionID,
		Provider:       identityproviderconnections.ProviderOkta,
		IssuerID:       issuerID,
	}
}

type publishedKeys struct {
	Keys []struct {
		Kid string `json:"kid"`
		Alg string `json:"alg"`
		Kty string `json:"kty"`
	} `json:"keys"`
}

func publishedKids(t *testing.T, doc []byte) []string {
	t.Helper()

	var published publishedKeys
	require.NoError(t, json.Unmarshal(doc, &published))

	kids := make([]string, 0, len(published.Keys))
	for _, key := range published.Keys {
		kids = append(kids, key.Kid)
	}

	return kids
}

// Provisioning a fresh connection yields every managed row with the marker set,
// a client registered for private_key_jwt against the token endpoint, and one
// active key whose public half is fetchable at the client's JWKS URL.
func TestProvisionClient_CreatesManagedRows(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	fx := provisiontest.Provision(t, ctx, ti.conn, ti.orgID, issuerID, testServerURL)
	client := fx.Client

	require.Equal(t, issuerID, client.IssuerID)
	require.Equal(t, "okta-pending-"+fx.ConnectionID.String(), client.ClientID)
	require.True(t, client.ActiveKeyID.Valid)
	require.NotEmpty(t, client.ActiveKid)
	require.False(t, client.ActivatedAt.IsZero())
	require.Equal(t, remotesessions.ClientJSONWebKeySetURL(mustURL(t, testServerURL), client.ClientRowID), client.JSONWebKeySetURL)

	row, err := repo.New(ti.conn).GetManagedClient(ctx, repo.GetManagedClientParams{
		OrganizationID:               conv.ToPGText(ti.orgID),
		IdentityProviderConnectionID: conv.ToNullUUID(fx.ConnectionID),
	})
	require.NoError(t, err)
	rsc := row.RemoteSessionClient
	require.Equal(t, client.ClientRowID, rsc.ID)
	require.Equal(t, ti.orgID, rsc.OrganizationID.String)
	require.False(t, rsc.ProjectID.Valid, "managed clients are organization-level")
	require.Equal(t, string(remotesessions.TokenEndpointAuthMethodPrivateKeyJWT), rsc.TokenEndpointAuthMethod.String)
	require.Equal(t, string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint), rsc.TokenEndpointAuthAudienceFormat.String)
	require.Equal(t, fx.ConnectionID, rsc.IdentityProviderConnectionID.UUID)
	require.Equal(t, client.JSONWebKeySetID, rsc.JsonWebKeySetID.UUID)
	require.False(t, rsc.ClientSecretEncrypted.Valid, "a private_key_jwt client carries no secret")

	set, err := jwksrepo.New(ti.conn).GetJsonWebKeySet(ctx, jwksrepo.GetJsonWebKeySetParams{ID: client.JSONWebKeySetID, OrganizationID: ti.orgID})
	require.NoError(t, err)
	require.Equal(t, fx.ConnectionID, set.IdentityProviderConnectionID.UUID)
	require.Equal(t, client.ExternalKeyID, set.ExternalKeyID)
	require.Equal(t, "Okta connection "+fx.ConnectionID.String()+" keys", set.Name)

	key, err := extkeysrepo.New(ti.conn).GetGcpKmsKey(ctx, extkeysrepo.GetGcpKmsKeyParams{ID: client.ExternalKeyID, OrganizationID: conv.ToPGText(ti.orgID)})
	require.NoError(t, err)
	require.Equal(t, fx.ConnectionID, key.ExternalKey.IdentityProviderConnectionID.UUID)
	require.Equal(t, fx.CredentialID, key.ExternalKey.ExternalCredentialID, "the backing credential is the platform-tier one")
	require.Equal(t, "RS256", key.ExternalKey.Algorithm)
	require.Regexp(t, "^"+provisiontest.KeyRing+`/cryptoKeys/okta-[0-9a-f]{32}/cryptoKeyVersions/1$`, key.GcpKmsKey.ResourceName)
	require.NotContains(t, key.GcpKmsKey.ResourceName, client.ClientRowID.String(), "the key name must not be derivable from the public JWKS URL")
	require.NotContains(t, key.GcpKmsKey.ResourceName, fx.ConnectionID.String())

	doc, err := remotesessionsrepo.New(ti.conn).GetRemoteSessionClientJsonWebKeySetDocument(ctx, client.ClientRowID)
	require.NoError(t, err)
	require.True(t, doc.Managed)

	var published publishedKeys
	require.NoError(t, json.Unmarshal(doc.Document, &published))
	require.Len(t, published.Keys, 1)
	require.Equal(t, client.ActiveKid, published.Keys[0].Kid)
	require.Equal(t, "RS256", published.Keys[0].Alg)
	require.Equal(t, "RSA", published.Keys[0].Kty)
}

// Provisioning audits as the system actor with the same events the
// organization-tier paths emit.
func TestProvisionClient_AuditsAsSystem(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	provisiontest.Provision(t, ctx, ti.conn, ti.orgID, issuerID, testServerURL)

	require.Equal(t, [][2]string{
		{"system:identity-provider-connections", "json_web_key_set:create"},
		{"system:identity-provider-connections", "json_web_key:publish"},
		{"system:identity-provider-connections", "remote-session-client:create"},
	}, auditActions(t, ctx, ti.conn, ti.orgID))
}

// A second run for the same connection adopts the first run's rows rather than
// creating another client, another set, or another KMS key.
func TestProvisionClient_IsIdempotentPerConnection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)
	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	provisioner := provisiontest.NewProvisioner(t, ti.conn, kms.Factory, testServerURL, credentialID)

	params := oktaParams(ti.orgID, connectionID, issuerID)

	first, err := provisioner.ProvisionClient(ctx, params)
	require.NoError(t, err)

	second, err := provisioner.ProvisionClient(ctx, params)
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.Len(t, kms.Created(), 1, "the second run must not create another kms key")

	loaded, err := provisioner.GetManagedClient(ctx, ti.orgID, connectionID)
	require.NoError(t, err)
	require.Equal(t, first, loaded)
}

func TestProvisionClient_RefusesUnknownConnection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	provisioner := provisiontest.NewProvisioner(t, ti.conn, provisiontest.NewKMSClients(t).Factory, testServerURL, credentialID)

	_, err := provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, uuid.New(), issuerID))
	require.ErrorIs(t, err, identityproviderconnections.ErrConnectionNotFound)

	// A connection in another organization is equally absent.
	otherOrg := createOrganization(t, ctx, ti.conn)
	otherConnection := provisiontest.CreateConnection(t, ctx, ti.conn, otherOrg, identityproviderconnections.ProviderOkta)
	_, err = provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, otherConnection, issuerID))
	require.ErrorIs(t, err, identityproviderconnections.ErrConnectionNotFound)

	// A deleted connection cannot be provisioned.
	deleted := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)
	provisiontest.SoftDeleteConnection(t, ctx, ti.conn, ti.orgID, deleted)
	_, err = provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, deleted, issuerID))
	require.ErrorIs(t, err, identityproviderconnections.ErrConnectionNotFound)
}

// The provider is validated against the known set and must match the row.
func TestProvisionClient_RefusesProviderMismatch(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	provisioner := provisiontest.NewProvisioner(t, ti.conn, provisiontest.NewKMSClients(t).Factory, testServerURL, credentialID)

	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)
	params := oktaParams(ti.orgID, connectionID, issuerID)
	params.Provider = "entra"
	_, err := provisioner.ProvisionClient(ctx, params)
	require.ErrorIs(t, err, identityproviderconnections.ErrUnknownProvider)

	_, err = provisioner.RotateClient(ctx, identityproviderconnections.RotateClientParams{OrganizationID: ti.orgID, ConnectionID: connectionID, Provider: ""})
	require.ErrorIs(t, err, identityproviderconnections.ErrUnknownProvider)

	_, err = provisioner.GetManagedClient(ctx, ti.orgID, connectionID)
	require.ErrorIs(t, err, identityproviderconnections.ErrNotProvisioned, "a refused run must leave nothing behind")
}

// Only the organization's own organization-level issuer with an https token
// endpoint can carry a managed client: a project-owned issuer and another
// organization's issuer both read as absent.
func TestProvisionClient_RefusesIneligibleIssuers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)
	provisioner := provisiontest.NewProvisioner(t, ti.conn, provisiontest.NewKMSClients(t).Factory, testServerURL, credentialID)

	otherOrg := createOrganization(t, ctx, ti.conn)
	foreignIssuer := createIssuer(t, ctx, ti.conn, otherOrg, noProject, tokenEndpoint)
	_, err := provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, foreignIssuer))
	require.ErrorIs(t, err, identityproviderconnections.ErrIssuerNotFound)

	_, err = provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, uuid.New()))
	require.ErrorIs(t, err, identityproviderconnections.ErrIssuerNotFound)

	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "ineligible-issuer-project",
		Slug:           "ineligible-" + uuid.NewString()[:8],
		OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	projectIssuer := createIssuer(t, ctx, ti.conn, ti.orgID, uuid.NullUUID{UUID: project.ID, Valid: true}, tokenEndpoint)
	_, err = provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, projectIssuer))
	require.ErrorIs(t, err, identityproviderconnections.ErrIssuerNotFound)

	noTokenEndpoint := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, "")
	_, err = provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, noTokenEndpoint))
	require.ErrorIs(t, err, identityproviderconnections.ErrIssuerHasNoTokenEndpoint)

	plaintextEndpoint := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, "http://tenant.okta.com/oauth2/v1/token")
	_, err = provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, plaintextEndpoint))
	require.ErrorIs(t, err, identityproviderconnections.ErrIssuerTokenEndpointNotHTTPS)

	tombstoned := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	_, err = remotesessionsrepo.New(ti.conn).DeleteOrganizationRemoteSessionIssuer(ctx, remotesessionsrepo.DeleteOrganizationRemoteSessionIssuerParams{ID: tombstoned, OrganizationID: conv.ToPGText(ti.orgID)})
	require.NoError(t, err)
	_, err = provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, tombstoned))
	require.ErrorIs(t, err, identityproviderconnections.ErrIssuerNotFound)

	_, err = provisioner.GetManagedClient(ctx, ti.orgID, connectionID)
	require.ErrorIs(t, err, identityproviderconnections.ErrNotProvisioned, "a refused run must leave nothing behind")
}

// An issuer tombstoned between the unlocked read and the issuer-binding lock
// is caught by the re-read inside the transaction; the KMS key created in
// between is disabled and no row lands.
func TestProvisionClient_RereadsIssuerUnderLockAndDisablesOrphanKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)

	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	kms.afterCreate = func(*gcpkms.CreatedSigningKey) {
		_, err := remotesessionsrepo.New(ti.conn).DeleteOrganizationRemoteSessionIssuer(ctx, remotesessionsrepo.DeleteOrganizationRemoteSessionIssuerParams{ID: issuerID, OrganizationID: conv.ToPGText(ti.orgID)})
		require.NoError(t, err)
	}
	provisioner := provisiontest.NewProvisioner(t, ti.conn, kms.Factory, testServerURL, credentialID)

	_, err := provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, issuerID))
	require.ErrorIs(t, err, identityproviderconnections.ErrIssuerNotFound)

	require.Len(t, kms.Created(), 1)
	require.Equal(t, kms.Created(), kms.Disabled(), "the orphaned key version must be disabled")

	_, err = provisioner.GetManagedClient(ctx, ti.orgID, connectionID)
	require.ErrorIs(t, err, identityproviderconnections.ErrNotProvisioned)
	require.Empty(t, auditActions(t, ctx, ti.conn, ti.orgID))
}

// A connection deleted after the key exists fails at the connection lock; the
// key is disabled rather than left enabled in the ring.
func TestProvisionClient_DisablesKeyWhenRowsFail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)

	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	kms.afterCreate = func(*gcpkms.CreatedSigningKey) {
		provisiontest.SoftDeleteConnection(t, ctx, ti.conn, ti.orgID, connectionID)
	}
	provisioner := provisiontest.NewProvisioner(t, ti.conn, kms.Factory, testServerURL, credentialID)

	_, err := provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, issuerID))
	require.ErrorIs(t, err, identityproviderconnections.ErrConnectionNotFound)
	require.Equal(t, kms.Created(), kms.Disabled())
}

// Two runs racing on the same connection: the second creates a key, finds the
// first's rows under the lock, adopts them, and disables its own key.
func TestProvisionClient_ConcurrentRunAdoptsAndDisablesItsKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)

	winner := provisiontest.NewProvisioner(t, ti.conn, provisiontest.NewKMSClients(t).Factory, testServerURL, credentialID)
	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	var first *identityproviderconnections.ManagedClient
	kms.afterCreate = func(*gcpkms.CreatedSigningKey) {
		var err error
		first, err = winner.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, issuerID))
		require.NoError(t, err)
	}
	loser := provisiontest.NewProvisioner(t, ti.conn, kms.Factory, testServerURL, credentialID)

	second, err := loser.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, issuerID))
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, kms.Created(), kms.Disabled())
}

// The signing credential must be platform-tier. A credential that lives inside
// an organization, even one a platform administrator exempted, is refused.
func TestProvisionClient_RefusesOrganizationTierCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)

	q := extcredrepo.New(ti.conn)
	orgCredential, err := q.CreateExternalCredential(ctx, extcredrepo.CreateExternalCredentialParams{
		OrganizationID: conv.ToPGText(ti.orgID),
		Provider:       "gcp_iam",
		Name:           "org-owned",
	})
	require.NoError(t, err)
	_, err = q.CreateGcpIamCredential(ctx, extcredrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      orgCredential.ID,
		ImpersonateServiceAccount: conv.ToPGText(provisiontest.SigningServiceAccount()),
		SkipProjectVerification:   true,
	})
	require.NoError(t, err)

	provisioner := provisiontest.NewProvisioner(t, ti.conn, provisiontest.NewKMSClients(t).Factory, testServerURL, orgCredential.ID)
	_, err = provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, issuerID))
	require.ErrorIs(t, err, identityproviderconnections.ErrSigningCredentialUnusable)
}

// The mint path admits the platform credential only through the managed-by
// marker: an organization key pointed at the same credential without the
// marker reads as having no credential.
func TestMintPath_AdmitsPlatformCredentialOnlyForManagedKeys(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	fx := provisiontest.Provision(t, ctx, ti.conn, ti.orgID, issuerID, testServerURL)

	q := jwksrepo.New(ti.conn)

	managed, err := q.GetExternalKeyForMint(ctx, jwksrepo.GetExternalKeyForMintParams{ID: fx.Client.ExternalKeyID, OrganizationID: conv.ToPGText(ti.orgID)})
	require.NoError(t, err)
	require.True(t, managed.CredentialID.Valid, "a managed key reaches the platform credential")
	require.Equal(t, fx.CredentialID, managed.CredentialID.UUID)
	require.True(t, managed.SkipProjectVerification.Bool)

	unmarked, err := extkeysrepo.New(ti.conn).CreateExternalKey(ctx, extkeysrepo.CreateExternalKeyParams{
		OrganizationID:       conv.ToPGText(ti.orgID),
		ExternalCredentialID: fx.CredentialID,
		Provider:             "gcp_kms",
		Algorithm:            "RS256",
		Name:                 "unmarked-behind-platform-credential",
	})
	require.NoError(t, err)

	orphan, err := q.GetExternalKeyForMint(ctx, jwksrepo.GetExternalKeyForMintParams{ID: unmarked.ID, OrganizationID: conv.ToPGText(ti.orgID)})
	require.NoError(t, err)
	require.False(t, orphan.CredentialID.Valid, "an unmarked key must not reach the platform credential")
}

// Rotation publishes a new active key on a new KMS key and retires the old
// one, which stays in the document until revoked. After revoke the document
// is empty.
func TestRotateClient_OverlapsThenRevokes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)

	// Distinct in-process keys per factory call, so the rotated key has a new kid.
	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	provisioner := provisiontest.NewProvisioner(t, ti.conn, kms.Factory, testServerURL, credentialID)
	before, err := provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, issuerID))
	require.NoError(t, err)

	rotatingKMS := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	rotator := provisiontest.NewProvisioner(t, ti.conn, rotatingKMS.Factory, testServerURL, credentialID)
	params := identityproviderconnections.RotateClientParams{OrganizationID: ti.orgID, ConnectionID: connectionID, Provider: identityproviderconnections.ProviderOkta}
	_, err = rotator.RotateClient(ctx, params)
	var pending *identityproviderconnections.RotationPendingError
	require.ErrorAs(t, err, &pending)
	docBefore, err := remotesessionsrepo.New(ti.conn).GetRemoteSessionClientJsonWebKeySetDocument(ctx, before.ClientRowID)
	require.NoError(t, err)
	require.Len(t, publishedKids(t, docBefore.Document), 2, "pending key is committed and publicly visible")
	current, err := repo.New(ti.conn).GetManagedClient(ctx, repo.GetManagedClientParams{OrganizationID: conv.ToPGText(ti.orgID), IdentityProviderConnectionID: conv.ToNullUUID(connectionID)})
	require.NoError(t, err)
	require.Equal(t, before.ExternalKeyID, current.ExternalKeyID, "publication must not change the signer")
	// Stay inside the database-clock cache window, then cross its boundary
	// below without advancing any application clock.
	err = repo.New(ti.conn).BackdatePendingRotationPublication(ctx, repo.BackdatePendingRotationPublicationParams{
		JsonWebKeySetID: before.JSONWebKeySetID,
		OrganizationID:  ti.orgID,
		AgeSeconds:      int32(identityproviderconnections.ManagedJWKSCacheTTL/time.Second) - 30,
	})
	require.NoError(t, err)
	_, err = rotator.RotateClient(ctx, params)
	require.ErrorAs(t, err, &pending)
	require.Len(t, rotatingKMS.Created(), 1, "retry must reuse the published pending key")
	err = repo.New(ti.conn).BackdatePendingRotationPublication(ctx, repo.BackdatePendingRotationPublicationParams{
		JsonWebKeySetID: before.JSONWebKeySetID,
		OrganizationID:  ti.orgID,
		AgeSeconds:      int32(identityproviderconnections.ManagedJWKSCacheTTL / time.Second),
	})
	require.NoError(t, err)
	after, err := rotator.RotateClient(ctx, identityproviderconnections.RotateClientParams{OrganizationID: ti.orgID, ConnectionID: connectionID, Provider: identityproviderconnections.ProviderOkta})
	require.NoError(t, err)

	require.Equal(t, before.ClientRowID, after.ClientRowID)
	require.Equal(t, before.JSONWebKeySetID, after.JSONWebKeySetID)
	require.NotEqual(t, before.ActiveKid, after.ActiveKid)
	require.NotEqual(t, before.ExternalKeyID, after.ExternalKeyID, "the set now backs onto the new external key")
	require.Len(t, rotatingKMS.Created(), 1)
	require.Empty(t, rotatingKMS.Disabled())

	newKey, err := extkeysrepo.New(ti.conn).GetGcpKmsKey(ctx, extkeysrepo.GetGcpKmsKeyParams{ID: after.ExternalKeyID, OrganizationID: conv.ToPGText(ti.orgID)})
	require.NoError(t, err)
	require.Equal(t, connectionID, newKey.ExternalKey.IdentityProviderConnectionID.UUID)
	require.Equal(t, rotatingKMS.Created()[0], newKey.GcpKmsKey.ResourceName)
	referenced, err := repo.New(ti.conn).ManagedKeyResourceExists(ctx, rotatingKMS.Created()[0])
	require.NoError(t, err)
	require.True(t, referenced, "ambiguous-commit reconciliation must preserve a referenced key")
	referenced, err = repo.New(ti.conn).ManagedKeyResourceExists(ctx, "missing-key-version")
	require.NoError(t, err)
	require.False(t, referenced)

	doc, err := remotesessionsrepo.New(ti.conn).GetRemoteSessionClientJsonWebKeySetDocument(ctx, after.ClientRowID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{before.ActiveKid, after.ActiveKid}, publishedKids(t, doc.Document), "old key still verifies during overlap")

	keys, err := jwksrepo.New(ti.conn).ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: after.JSONWebKeySetID, OrganizationID: ti.orgID, IncludeRevoked: false})
	require.NoError(t, err)
	states := map[string]string{}
	for _, key := range keys {
		states[key.Kid] = key.State
	}
	require.Equal(t, map[string]string{before.ActiveKid: "retired", after.ActiveKid: "active"}, states)

	revoked, err := rotator.RevokeClient(ctx, ti.orgID, connectionID)
	require.NoError(t, err)
	require.Equal(t, 2, revoked)

	doc, err = remotesessionsrepo.New(ti.conn).GetRemoteSessionClientJsonWebKeySetDocument(ctx, after.ClientRowID)
	require.NoError(t, err)
	require.JSONEq(t, `{"keys":[]}`, string(doc.Document))

	require.Equal(t, [][2]string{
		{"system:identity-provider-connections", "json_web_key_set:create"},
		{"system:identity-provider-connections", "json_web_key:publish"},
		{"system:identity-provider-connections", "remote-session-client:create"},
		{"system:identity-provider-connections", "json_web_key:publish"},
		{"system:identity-provider-connections", "json_web_key:retire"},
		{"system:identity-provider-connections", "json_web_key:activate"},
		{"system:identity-provider-connections", "json_web_key_set:update"},
		{"system:identity-provider-connections", "json_web_key:revoke"},
		{"system:identity-provider-connections", "json_web_key:revoke"},
	}, auditActions(t, ctx, ti.conn, ti.orgID))
}

// Rotating onto key material the set already holds is refused, and the key
// created for it is disabled.
func TestRotateClient_RefusesRepublishedMaterial(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)

	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	provisioner := provisiontest.NewProvisioner(t, ti.conn, kms.Factory, testServerURL, credentialID)
	_, err := provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, issuerID))
	require.NoError(t, err)

	// The same factory serves the same in-process key, so the kid repeats.
	_, err = provisioner.RotateClient(ctx, identityproviderconnections.RotateClientParams{OrganizationID: ti.orgID, ConnectionID: connectionID, Provider: identityproviderconnections.ProviderOkta})
	require.ErrorIs(t, err, identityproviderconnections.ErrKeyAlreadyPublished)
	require.Len(t, kms.Created(), 2)
	require.Equal(t, kms.Created()[1:], kms.Disabled())
}

func TestRotateClient_RequiresProvisionedConnection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)
	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	provisioner := provisiontest.NewProvisioner(t, ti.conn, kms.Factory, testServerURL, credentialID)

	_, err := provisioner.RotateClient(ctx, identityproviderconnections.RotateClientParams{OrganizationID: ti.orgID, ConnectionID: connectionID, Provider: identityproviderconnections.ProviderOkta})
	require.ErrorIs(t, err, identityproviderconnections.ErrNotProvisioned)
	require.Empty(t, kms.Created(), "no key is created for an unprovisioned connection")
}

// Revoking withdraws every key while the client and set stay live, so the
// public document serves an empty key set instead of disappearing.
func TestRevokeClient_ServesEmptyKeySet(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	fx := provisiontest.Provision(t, ctx, ti.conn, ti.orgID, issuerID, testServerURL)

	revoked, err := fx.Provisioner.RevokeClient(ctx, ti.orgID, fx.ConnectionID)
	require.NoError(t, err)
	require.Equal(t, 1, revoked)

	doc, err := remotesessionsrepo.New(ti.conn).GetRemoteSessionClientJsonWebKeySetDocument(ctx, fx.Client.ClientRowID)
	require.NoError(t, err)
	require.JSONEq(t, `{"keys":[]}`, string(doc.Document))
	require.True(t, doc.Managed)

	after, err := fx.Provisioner.GetManagedClient(ctx, ti.orgID, fx.ConnectionID)
	require.NoError(t, err)
	require.Equal(t, fx.Client.ClientRowID, after.ClientRowID, "the client row stays live")
	require.False(t, after.ActiveKeyID.Valid)
	require.Empty(t, after.ActiveKid)

	url, err := fx.Provisioner.ClientJSONWebKeySetURL(ctx, ti.orgID, fx.ConnectionID)
	require.NoError(t, err)
	require.Equal(t, fx.Client.JSONWebKeySetURL, url, "the JWKS URL survives revocation")

	keys, err := jwksrepo.New(ti.conn).ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: fx.Client.JSONWebKeySetID, OrganizationID: ti.orgID, IncludeRevoked: true})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, "revoked", keys[0].State)
	require.True(t, keys[0].RevokedAt.Valid)

	again, err := fx.Provisioner.RevokeClient(ctx, ti.orgID, fx.ConnectionID)
	require.NoError(t, err)
	require.Equal(t, 0, again, "a second revoke is a no-op")

	actions := auditActions(t, ctx, ti.conn, ti.orgID)
	require.Equal(t, [2]string{"system:identity-provider-connections", "json_web_key:revoke"}, actions[len(actions)-1])
}

// A tombstoned connection can still revoke its keys.
func TestRevokeClient_WorksOnDeletedConnection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	fx := provisiontest.Provision(t, ctx, ti.conn, ti.orgID, issuerID, testServerURL)
	provisiontest.SoftDeleteConnection(t, ctx, ti.conn, ti.orgID, fx.ConnectionID)

	revoked, err := fx.Provisioner.RevokeClient(ctx, ti.orgID, fx.ConnectionID)
	require.NoError(t, err)
	require.Equal(t, 1, revoked)

	doc, err := remotesessionsrepo.New(ti.conn).GetRemoteSessionClientJsonWebKeySetDocument(ctx, fx.Client.ClientRowID)
	require.NoError(t, err)
	require.JSONEq(t, `{"keys":[]}`, string(doc.Document))

	_, err = fx.Provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, fx.ConnectionID, issuerID))
	require.ErrorIs(t, err, identityproviderconnections.ErrConnectionNotFound, "provisioning still needs a live connection")
}

func TestRevokeClient_RequiresProvisionedConnection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)
	provisioner := provisiontest.NewProvisioner(t, ti.conn, provisiontest.NewKMSClients(t).Factory, testServerURL, credentialID)

	_, err := provisioner.RevokeClient(ctx, ti.orgID, connectionID)
	require.ErrorIs(t, err, identityproviderconnections.ErrNotProvisioned)

	_, err = provisioner.ClientJSONWebKeySetURL(ctx, ti.orgID, connectionID)
	require.ErrorIs(t, err, identityproviderconnections.ErrNotProvisioned)

	_, err = provisioner.RevokeClient(ctx, ti.orgID, uuid.New())
	require.ErrorIs(t, err, identityproviderconnections.ErrConnectionNotFound)
}

// The marker the owning packages' guards read is the one written on every row.
func TestProvisionClient_MarksEveryRow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	fx := provisiontest.Provision(t, ctx, ti.conn, ti.orgID, issuerID, testServerURL)

	locked, err := jwksrepo.New(ti.conn).LockExternalKeyForJwksWrite(ctx, jwksrepo.LockExternalKeyForJwksWriteParams{ID: fx.Client.ExternalKeyID, OrganizationID: conv.ToPGText(ti.orgID)})
	require.NoError(t, err)
	require.Equal(t, fx.ConnectionID, locked.IdentityProviderConnectionID.UUID)

	deleteLock, err := extkeysrepo.New(ti.conn).LockExternalKeyForDelete(ctx, extkeysrepo.LockExternalKeyForDeleteParams{ID: fx.Client.ExternalKeyID, OrganizationID: conv.ToPGText(ti.orgID), Provider: "gcp_kms"})
	require.NoError(t, err)
	require.Equal(t, fx.ConnectionID, deleteLock.IdentityProviderConnectionID.UUID)

	managed, err := remotesessionsrepo.New(ti.conn).CountManagedRemoteSessionClientsByIssuerID(ctx, remotesessionsrepo.CountManagedRemoteSessionClientsByIssuerIDParams{RemoteSessionIssuerID: issuerID, OrganizationID: conv.ToPGText(ti.orgID)})
	require.NoError(t, err)
	require.Equal(t, int64(1), managed)
}

// Parallel retries converge on a single committed pending key (extra KMS keys
// are disabled), including recovery after a crash between publication commit
// and recording its observed time.
func TestRotateClient_ConcurrentRetriesAndRevocation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)
	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)
	initial := provisiontest.NewProvisioner(t, ti.conn, provisiontest.NewKMSClients(t).Factory, testServerURL, credentialID)
	before, err := initial.ProvisionClient(ctx, oktaParams(ti.orgID, connectionID, issuerID))
	require.NoError(t, err)
	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	rotator := provisiontest.NewProvisioner(t, ti.conn, kms.Factory, testServerURL, credentialID)
	params := identityproviderconnections.RotateClientParams{OrganizationID: ti.orgID, ConnectionID: connectionID, Provider: identityproviderconnections.ProviderOkta}
	results := make(chan error, 4)
	for range 4 {
		go func() { _, err := rotator.RotateClient(ctx, params); results <- err }()
	}
	var pending *identityproviderconnections.RotationPendingError
	var readyAt time.Time
	for range 4 {
		require.ErrorAs(t, <-results, &pending)
		if readyAt.IsZero() {
			readyAt = pending.ReadyAt
		}
		require.Equal(t, readyAt, pending.ReadyAt)
	}
	// Keys are minted before the lock, so losers disable theirs; one survives.
	require.NotEmpty(t, kms.Created())
	require.Len(t, kms.Disabled(), len(kms.Created())-1)
	require.Subset(t, kms.Created(), kms.Disabled())
	minted := len(kms.Created())
	keys, err := jwksrepo.New(ti.conn).ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: before.JSONWebKeySetID, OrganizationID: ti.orgID, IncludeRevoked: false})
	require.NoError(t, err)
	require.Len(t, keys, 2)
	for _, key := range keys {
		if key.State == "pending" {
			require.NoError(t, repo.New(ti.conn).MarkRotationPublicationUnobserved(ctx, repo.MarkRotationPublicationUnobservedParams{ID: key.ID, OrganizationID: ti.orgID}))
		}
	}
	// An unobserved committed publication must start a fresh cache window.
	_, err = rotator.RotateClient(ctx, params)
	require.ErrorAs(t, err, &pending)
	require.False(t, pending.ReadyAt.IsZero())
	require.Len(t, kms.Created(), minted, "an unobserved publication is reused, not re-minted")
	revoked, err := rotator.RevokeClient(ctx, ti.orgID, connectionID)
	require.NoError(t, err)
	require.Equal(t, 2, revoked)
	_, err = rotator.RotateClient(ctx, params)
	require.ErrorIs(t, err, identityproviderconnections.ErrNotProvisioned)
	require.Len(t, kms.Created(), minted, "a retry must not resurrect revoked keys")
	doc, err := remotesessionsrepo.New(ti.conn).GetRemoteSessionClientJsonWebKeySetDocument(ctx, before.ClientRowID)
	require.NoError(t, err)
	require.JSONEq(t, `{"keys":[]}`, string(doc.Document))
}

// The advertised JWKS URL must be fetchable over TLS, so a plaintext server
// URL is refused at construction; loopback http stays allowed for local dev.
func TestNewProvisioner_RejectsPlaintextServerURL(t *testing.T) {
	t.Parallel()
	_, ti := newTestDB(t)

	build := func(serverURL string) error {
		_, err := identityproviderconnections.NewProvisioner(
			testenv.NewLogger(t),
			ti.conn,
			gcpauth.NewIdentity(gcpauth.NewStubResolver()),
			provisiontest.NewKMSClients(t).Factory,
			audit.NewLogger(),
			identityproviderconnections.Config{
				KeyRing:             provisiontest.KeyRing,
				SigningCredentialID: uuid.New(),
				ServerURL:           mustURL(t, serverURL),
			},
		)
		return err
	}

	require.Error(t, build("http://app.getgram.ai"))
	require.NoError(t, build("https://app.getgram.ai"))
	require.NoError(t, build("http://localhost:8080"))
}
