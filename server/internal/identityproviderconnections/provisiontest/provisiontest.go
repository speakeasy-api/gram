// Package provisiontest builds the managed rows an identity provider
// connection provisions, for tests in the packages whose organization-tier
// mutations must refuse them.
package provisiontest

import (
	"context"
	"net/url"
	"strings"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	extcredrepo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	extkeysrepo "github.com/speakeasy-api/gram/server/internal/externalkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

// KeyRing is the Speakeasy-owned ring every fixture key is named in.
const KeyRing = "projects/gram-stub/locations/global/keyRings/okta-connections"

// TokenEndpoint is the token endpoint every fixture issuer discovers.
const TokenEndpoint = "https://tenant.okta.com/oauth2/default/v1/token"

// SigningServiceAccount is the platform signing credential's target, in the
// stub resolver's own project.
func SigningServiceAccount() string {
	_, domain, _ := strings.Cut(gcpauth.StubResolverPrincipal, "@")
	return "okta-signer@" + domain
}

// CreatePlatformSigningCredential writes the platform-tier gcp_iam credential
// the provisioner signs with and returns its id.
func CreatePlatformSigningCredential(t *testing.T, ctx context.Context, conn *pgxpool.Pool) uuid.UUID {
	t.Helper()

	q := extcredrepo.New(conn)
	ec, err := q.CreateExternalCredential(ctx, extcredrepo.CreateExternalCredentialParams{
		OrganizationID: pgtype.Text{String: "", Valid: false},
		Provider:       "gcp_iam",
		Name:           "okta-signing-" + uuid.NewString(),
	})
	require.NoError(t, err)

	_, err = q.CreateGcpIamCredential(ctx, extcredrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      ec.ID,
		ImpersonateServiceAccount: conv.ToPGText(SigningServiceAccount()),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
		SkipProjectVerification:   true,
	})
	require.NoError(t, err)

	return ec.ID
}

// CreateConnection writes a pending connection row for the provider and returns its id.
func CreateConnection(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, provider string) uuid.UUID {
	t.Helper()

	row, err := repo.New(conn).CreateIdentityProviderConnection(ctx, repo.CreateIdentityProviderConnectionParams{
		OrganizationID: organizationID,
		Provider:       provider,
	})
	require.NoError(t, err)

	return row.ID
}

// SoftDeleteConnection tombstones a connection row.
func SoftDeleteConnection(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, connectionID uuid.UUID) {
	t.Helper()

	_, err := repo.New(conn).SoftDeleteIdentityProviderConnection(ctx, repo.SoftDeleteIdentityProviderConnectionParams{
		ID:             connectionID,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
}

// CreateIssuer plants a remote_session_issuer the shape a discovered Okta
// tenant has. An empty tokenEndpoint records none.
func CreateIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.NullUUID, tokenEndpoint string) uuid.UUID {
	t.Helper()

	issuer, err := remotesessionsrepo.New(conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         projectID,
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              "okta-" + uuid.NewString(),
		Issuer:                            "https://" + uuid.NewString() + ".okta.com/oauth2/default",
		AuthorizationEndpoint:             conv.ToPGText("https://tenant.okta.com/oauth2/default/v1/authorize"),
		TokenEndpoint:                     conv.ToPGTextEmpty(tokenEndpoint),
		ScopesSupported:                   []string{"okta.users.read"},
		GrantTypesSupported:               []string{"client_credentials"},
		ResponseTypesSupported:            []string{"token"},
		TokenEndpointAuthMethodsSupported: []string{"private_key_jwt"},
	})
	require.NoError(t, err)

	return issuer.ID
}

// CreateGcpKmsKeyDirect writes an ES256 gcp_kms key straight through the repo,
// for credentials the key API refuses (an exempted one). Returns the key id.
func CreateGcpKmsKeyDirect(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, name, credentialID string) string {
	t.Helper()

	credID, err := uuid.Parse(credentialID)
	require.NoError(t, err)

	q := extkeysrepo.New(conn)
	ek, err := q.CreateExternalKey(ctx, extkeysrepo.CreateExternalKeyParams{
		OrganizationID:               conv.ToPGText(organizationID),
		ExternalCredentialID:         credID,
		Provider:                     "gcp_kms",
		Algorithm:                    "ES256",
		Name:                         name,
		CustomerGrantReference:       pgtype.Text{String: "", Valid: false},
		IdentityProviderConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	_, err = q.CreateGcpKmsKey(ctx, extkeysrepo.CreateGcpKmsKeyParams{
		ExternalKeyID: ek.ID,
		ResourceName:  "projects/gram/locations/global/keyRings/signing/cryptoKeys/" + uuid.NewString() + "/cryptoKeyVersions/1",
	})
	require.NoError(t, err)

	return ek.ID.String()
}

// KMSClients is the ProvisioningClientFactory the fixture provisioner uses:
// one shared in-process RS256 key.
type KMSClients struct {
	client *gcpkms.LocalSigningClient
}

// NewKMSClients generates the shared key.
func NewKMSClients(t *testing.T) *KMSClients {
	t.Helper()

	client, err := gcpkms.NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)

	return &KMSClients{client: client}
}

// Factory implements gcpkms.ProvisioningClientFactory.
func (k *KMSClients) Factory(_ context.Context, _ oauth2.TokenSource) (gcpkms.ProvisioningClient, error) {
	return k.client, nil
}

// NewProvisioner builds a provisioner over the test database with the stub
// GCP identity and the given KMS factory. signingServiceAccount pins the
// service account the credential must impersonate; empty means unpinned.
func NewProvisioner(t *testing.T, conn *pgxpool.Pool, kmsClients gcpkms.ProvisioningClientFactory, serverURL string, credentialID uuid.UUID, signingServiceAccount string) *identityproviderconnections.Provisioner {
	t.Helper()

	base, err := url.Parse(serverURL)
	require.NoError(t, err)

	provisioner, err := identityproviderconnections.NewProvisioner(
		testenv.NewLogger(t),
		conn,
		gcpauth.NewIdentity(gcpauth.NewStubResolver()),
		kmsClients,
		audit.NewLogger(),
		identityproviderconnections.Config{
			KeyRing:               KeyRing,
			SigningCredentialID:   credentialID,
			SigningServiceAccount: signingServiceAccount,
			ServerURL:             base,
		},
	)
	require.NoError(t, err)

	return provisioner
}

// Fixture is a provisioned connection and the pieces that built it.
type Fixture struct {
	Provisioner  *identityproviderconnections.Provisioner
	ConnectionID uuid.UUID
	CredentialID uuid.UUID
	Client       *identityproviderconnections.ManagedClient
}

// Provision creates a platform signing credential and an Okta connection for
// the organization, then provisions the managed client on issuerID.
func Provision(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, issuerID uuid.UUID, serverURL string) *Fixture {
	t.Helper()

	credentialID := CreatePlatformSigningCredential(t, ctx, conn)
	connectionID := CreateConnection(t, ctx, conn, organizationID, identityproviderconnections.ProviderOkta)
	provisioner := NewProvisioner(t, conn, NewKMSClients(t).Factory, serverURL, credentialID, "")

	client, err := provisioner.ProvisionClient(ctx, identityproviderconnections.ProvisionClientParams{
		OrganizationID: organizationID,
		ConnectionID:   connectionID,
		Provider:       identityproviderconnections.ProviderOkta,
		IssuerID:       issuerID,
	})
	require.NoError(t, err)

	return &Fixture{
		Provisioner:  provisioner,
		ConnectionID: connectionID,
		CredentialID: credentialID,
		Client:       client,
	}
}
