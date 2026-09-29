package oktaapplications_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	idprepo "github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/oktaapplications"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

const (
	basicOrgURL  = "https://basic.okta.com"
	clientSecret = "okta-client-secret"
)

// basicConnection is a verified Okta connection whose managed client
// authenticates with client_secret_basic.
type basicConnection struct {
	conn         *pgxpool.Pool
	orgID        string
	connectionID uuid.UUID
	enc          *encryption.Client
}

func newBasicConnection(t *testing.T, ctx context.Context) *basicConnection {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Okta Applications Test Org",
		Slug:        "okta-apps-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	issuerID := provisiontest.CreateIssuer(t, ctx, conn, orgID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, provisiontest.TokenEndpoint)
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, conn)
	connectionID := provisiontest.CreateConnection(t, ctx, conn, orgID, identityproviderconnections.ProviderOkta)
	provisioner := provisiontest.NewProvisioner(t, conn, provisiontest.NewKMSClients(t).Factory, "https://app.getgram.ai", credentialID, "")

	client, err := provisioner.ProvisionClient(ctx, identityproviderconnections.ProvisionClientParams{
		OrganizationID: orgID,
		ConnectionID:   connectionID,
		Provider:       identityproviderconnections.ProviderOkta,
		IssuerID:       issuerID,
		AuthMethod:     remotesessions.TokenEndpointAuthMethodBasic,
	})
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodBasic, client.AuthMethod)

	q := idprepo.New(conn)
	issuer, err := q.GetOrganizationRemoteSessionIssuerForProvisioning(ctx, idprepo.GetOrganizationRemoteSessionIssuerForProvisioningParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(orgID),
	})
	require.NoError(t, err)

	_, err = q.CreateOktaIdentityProviderConnection(ctx, idprepo.CreateOktaIdentityProviderConnectionParams{
		IdentityProviderConnectionID: connectionID,
		OrganizationID:               orgID,
		OrgUrl:                       basicOrgURL,
		IssuerUrl:                    issuer.Issuer,
		RemoteSessionIssuerID:        issuerID,
		RemoteSessionClientID:        client.ClientRowID,
		ListingMode:                  identityproviderconnections.ListingModeOIN,
	})
	require.NoError(t, err)

	enc := testenv.NewEncryptionClient(t)
	encrypted, err := enc.Encrypt([]byte(clientSecret))
	require.NoError(t, err)

	_, err = q.SetManagedClientID(ctx, idprepo.SetManagedClientIDParams{
		ClientID:                     "0oabasicclient000001",
		ClientSecretEncrypted:        conv.ToPGText(encrypted),
		ID:                           client.ClientRowID,
		OrganizationID:               conv.ToPGText(orgID),
		IdentityProviderConnectionID: conv.ToNullUUID(connectionID),
		PlaceholderClientID:          client.ClientID,
	})
	require.NoError(t, err)

	_, err = q.UpdateIdentityProviderConnectionVerification(ctx, idprepo.UpdateIdentityProviderConnectionVerificationParams{
		Status:         identityproviderconnections.StatusVerified,
		LastVerifiedAt: pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
		LastError:      pgtype.Text{String: "", Valid: false},
		ID:             connectionID,
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	return &basicConnection{conn: conn, orgID: orgID, connectionID: connectionID, enc: enc}
}

func basicFakes() *okta.FakeFactory {
	return okta.NewFakeFactory(map[string]okta.Fixtures{
		basicOrgURL: {
			Users:         nil,
			Apps:          []okta.App{{ID: "0oaappa0000000000001", Label: "Notion MCP", Name: "oidc_client", SignOnMode: "OPENID_CONNECT", Status: "ACTIVE", Features: nil, Created: time.Time{}, LastUpdated: time.Time{}}},
			AppUsers:      nil,
			AppGroups:     nil,
			Groups:        nil,
			GrantedScopes: []string{"okta.apps.read"},
		},
	})
}

func TestSyncerRunsClientSecretBasicConnection(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	bc := newBasicConnection(t, ctx)
	fakes := basicFakes()
	fakes.Fake(basicOrgURL).RequireClientSecret(clientSecret, bc.enc)

	syncer := oktaapplications.NewSyncer(testenv.NewLogger(t), testenv.NewMeterProvider(t), bc.conn, fakes)
	require.NoError(t, syncer.Run(ctx, bc.connectionID, false))

	run, err := oktaapplications.LatestRun(ctx, bc.conn, bc.orgID, bc.connectionID)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, "succeeded", run.Status)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodBasic, fakes.Fake(basicOrgURL).LastAuthMethod())

	apps, err := oktaapplications.ListSnapshot(ctx, bc.conn, bc.orgID, bc.connectionID, false, 10)
	require.NoError(t, err)
	require.Len(t, apps, 1)
}

func TestSyncerRecordsRejectedClientSecret(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	bc := newBasicConnection(t, ctx)
	fakes := basicFakes()
	fakes.Fake(basicOrgURL).RequireClientSecret("a-different-secret", bc.enc)

	syncer := oktaapplications.NewSyncer(testenv.NewLogger(t), testenv.NewMeterProvider(t), bc.conn, fakes)
	require.NoError(t, syncer.Run(ctx, bc.connectionID, false))

	run, err := oktaapplications.LatestRun(ctx, bc.conn, bc.orgID, bc.connectionID)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, "failed", run.Status)
	require.Equal(t, "credential_rejected", run.Error.String)
}
