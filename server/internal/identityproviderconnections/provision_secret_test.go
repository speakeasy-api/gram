package identityproviderconnections_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type secretFixture struct {
	provisioner  *identityproviderconnections.Provisioner
	kms          *hookedKMSClients
	connectionID uuid.UUID
	issuerID     uuid.UUID
	client       *identityproviderconnections.ManagedClient
}

func secretParams(orgID string, connectionID, issuerID uuid.UUID) identityproviderconnections.ProvisionClientParams {
	params := oktaParams(orgID, connectionID, issuerID)
	params.AuthMethod = remotesessions.TokenEndpointAuthMethodBasic
	return params
}

func provisionSecretClient(t *testing.T, ctx context.Context, ti *testInstance) *secretFixture {
	t.Helper()

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	connectionID := provisiontest.CreateConnection(t, ctx, ti.conn, ti.orgID, identityproviderconnections.ProviderOkta)
	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	provisioner := provisiontest.NewProvisioner(t, ti.conn, kms.Factory, testServerURL, credentialID, "")

	client, err := provisioner.ProvisionClient(ctx, secretParams(ti.orgID, connectionID, issuerID))
	require.NoError(t, err)
	return &secretFixture{provisioner: provisioner, kms: kms, connectionID: connectionID, issuerID: issuerID, client: client}
}

func setClientIDParams(orgID string, connectionID uuid.UUID, secretEncrypted string) identityproviderconnections.SetClientIDParams {
	return identityproviderconnections.SetClientIDParams{
		OrganizationID:        orgID,
		ConnectionID:          connectionID,
		Provider:              identityproviderconnections.ProviderOkta,
		ClientID:              testClientID,
		ClientSecretEncrypted: secretEncrypted,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, "user_test"),
		ActorDisplayName:      nil,
	}
}

// setClientID runs SetClientID in its own committed transaction.
func setClientID(t *testing.T, ctx context.Context, ti *testInstance, fx *secretFixture, secretEncrypted string) (*identityproviderconnections.ManagedClient, error) {
	t.Helper()

	dbtx := testenv.BeginTx(t, ctx, ti.conn)
	managed, err := fx.provisioner.SetClientID(ctx, dbtx, setClientIDParams(ti.orgID, fx.connectionID, secretEncrypted))
	if err != nil {
		return nil, fmt.Errorf("set client id: %w", err)
	}
	require.NoError(t, dbtx.Commit(ctx))
	return managed, nil
}

func storedSecret(t *testing.T, ctx context.Context, ti *testInstance, connectionID uuid.UUID) (string, bool) {
	t.Helper()

	row, err := repo.New(ti.conn).GetManagedClient(ctx, repo.GetManagedClientParams{
		OrganizationID:               conv.ToPGText(ti.orgID),
		IdentityProviderConnectionID: conv.ToNullUUID(connectionID),
	})
	require.NoError(t, err)
	return row.RemoteSessionClient.ClientSecretEncrypted.String, row.RemoteSessionClient.ClientSecretEncrypted.Valid
}

func TestProvisionClient_SecretClientHasNoKeyMaterial(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)
	fx := provisionSecretClient(t, ctx, ti)

	require.Equal(t, remotesessions.TokenEndpointAuthMethodBasic, fx.client.AuthMethod)
	require.Equal(t, fx.issuerID, fx.client.IssuerID)
	require.Equal(t, identityproviderconnections.PlaceholderClientID(identityproviderconnections.ProviderOkta, fx.connectionID), fx.client.ClientID)
	require.False(t, fx.client.JSONWebKeySetID.Valid)
	require.False(t, fx.client.ExternalKeyID.Valid)
	require.False(t, fx.client.ActiveKeyID.Valid)
	require.Empty(t, fx.client.JSONWebKeySetURL)
	require.Empty(t, fx.client.ClientSecretEncrypted)
	require.Zero(t, fx.kms.Opened(), "a secret client never touches KMS")
	require.Empty(t, fx.kms.Created())

	row, err := repo.New(ti.conn).GetManagedClient(ctx, repo.GetManagedClientParams{
		OrganizationID:               conv.ToPGText(ti.orgID),
		IdentityProviderConnectionID: conv.ToNullUUID(fx.connectionID),
	})
	require.NoError(t, err)
	require.Equal(t, string(remotesessions.TokenEndpointAuthMethodBasic), row.RemoteSessionClient.TokenEndpointAuthMethod.String)
	require.Equal(t, string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint), row.RemoteSessionClient.TokenEndpointAuthAudienceFormat.String)
	require.False(t, row.RemoteSessionClient.JsonWebKeySetID.Valid)
	require.False(t, row.RemoteSessionClient.ClientSecretEncrypted.Valid)

	require.Equal(t, [][2]string{
		{"system:identity-provider-connections", "remote-session-client:create"},
	}, auditActions(t, ctx, ti.conn, ti.orgID))

	loaded, err := fx.provisioner.GetManagedClient(ctx, ti.orgID, fx.connectionID)
	require.NoError(t, err)
	require.Equal(t, fx.client, loaded)
}

func TestGetManagedClient_PrivateKeyJWTWithoutKeySetIsNotProvisioned(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)
	fx := provisionSecretClient(t, ctx, ti)

	n, err := repo.New(ti.conn).ForceManagedClientAuthMethodFixture(ctx, repo.ForceManagedClientAuthMethodFixtureParams{
		TokenEndpointAuthMethod: conv.ToPGText(string(remotesessions.TokenEndpointAuthMethodPrivateKeyJWT)),
		ID:                      fx.client.ClientRowID,
		OrganizationID:          conv.ToPGText(ti.orgID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	_, err = fx.provisioner.GetManagedClient(ctx, ti.orgID, fx.connectionID)
	require.ErrorIs(t, err, identityproviderconnections.ErrNotProvisioned)
}

func TestProvisionClient_SecretClientIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)
	fx := provisionSecretClient(t, ctx, ti)

	again, err := fx.provisioner.ProvisionClient(ctx, secretParams(ti.orgID, fx.connectionID, fx.issuerID))
	require.NoError(t, err)
	require.Equal(t, fx.client, again)
	require.Zero(t, fx.kms.Opened())
	require.Len(t, auditActions(t, ctx, ti.conn, ti.orgID), 1, "adoption writes no second client")
}

func TestProvisionClient_RefusesAuthMethodMismatchOnAdopt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	secret := provisionSecretClient(t, ctx, ti)
	_, err := secret.provisioner.ProvisionClient(ctx, oktaParams(ti.orgID, secret.connectionID, secret.issuerID))
	require.ErrorIs(t, err, identityproviderconnections.ErrAuthMethodMismatch)

	// One live connection per organization, so the keyed client lives in another.
	otherOrg := createOrganization(t, ctx, ti.conn)
	issuerID := createIssuer(t, ctx, ti.conn, otherOrg, noProject, tokenEndpoint)
	keyed := provisiontest.Provision(t, ctx, ti.conn, otherOrg, issuerID, testServerURL)
	_, err = keyed.Provisioner.ProvisionClient(ctx, secretParams(otherOrg, keyed.ConnectionID, issuerID))
	require.ErrorIs(t, err, identityproviderconnections.ErrAuthMethodMismatch)
}

func TestSetClientID_SecretClientRequiresSecret(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)
	fx := provisionSecretClient(t, ctx, ti)

	_, err := setClientID(t, ctx, ti, fx, "")
	require.ErrorIs(t, err, identityproviderconnections.ErrClientSecretRequired)

	managed, err := setClientID(t, ctx, ti, fx, "ciphertext-1")
	require.NoError(t, err)
	require.Equal(t, testClientID, managed.ClientID)
	require.Equal(t, "ciphertext-1", managed.ClientSecretEncrypted)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodBasic, managed.AuthMethod)

	stored, ok := storedSecret(t, ctx, ti, fx.connectionID)
	require.True(t, ok)
	require.Equal(t, "ciphertext-1", stored)
}

func TestSetClientID_KeyClientRefusesSecret(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	keyed := provisiontest.Provision(t, ctx, ti.conn, ti.orgID, issuerID, testServerURL)
	fx := &secretFixture{provisioner: keyed.Provisioner, kms: nil, connectionID: keyed.ConnectionID, issuerID: issuerID, client: keyed.Client}

	_, err := setClientID(t, ctx, ti, fx, "ciphertext-1")
	require.ErrorIs(t, err, identityproviderconnections.ErrClientSecretNotAccepted)

	_, ok := storedSecret(t, ctx, ti, fx.connectionID)
	require.False(t, ok)
}

func TestReplaceClientSecret_SwapsCiphertext(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)
	fx := provisionSecretClient(t, ctx, ti)
	_, err := setClientID(t, ctx, ti, fx, "ciphertext-1")
	require.NoError(t, err)

	dbtx := testenv.BeginTx(t, ctx, ti.conn)
	managed, err := fx.provisioner.ReplaceClientSecret(ctx, dbtx, identityproviderconnections.ReplaceClientSecretParams{
		OrganizationID:        ti.orgID,
		ConnectionID:          fx.connectionID,
		Provider:              identityproviderconnections.ProviderOkta,
		ClientSecretEncrypted: "ciphertext-2",
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, "user_test"),
		ActorDisplayName:      nil,
	})
	require.NoError(t, err)
	require.NoError(t, dbtx.Commit(ctx))
	require.Equal(t, "ciphertext-2", managed.ClientSecretEncrypted)
	require.Equal(t, testClientID, managed.ClientID)

	stored, ok := storedSecret(t, ctx, ti, fx.connectionID)
	require.True(t, ok)
	require.Equal(t, "ciphertext-2", stored)

	actions := auditActions(t, ctx, ti.conn, ti.orgID)
	require.Equal(t, [2]string{"user:user_test", "remote-session-client:update"}, actions[len(actions)-1])
}

func TestReplaceClientSecret_RefusesKeyClient(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)

	issuerID := createIssuer(t, ctx, ti.conn, ti.orgID, noProject, tokenEndpoint)
	keyed := provisiontest.Provision(t, ctx, ti.conn, ti.orgID, issuerID, testServerURL)

	dbtx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := keyed.Provisioner.ReplaceClientSecret(ctx, dbtx, identityproviderconnections.ReplaceClientSecretParams{
		OrganizationID:        ti.orgID,
		ConnectionID:          keyed.ConnectionID,
		Provider:              identityproviderconnections.ProviderOkta,
		ClientSecretEncrypted: "ciphertext-2",
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, "user_test"),
		ActorDisplayName:      nil,
	})
	require.ErrorIs(t, err, identityproviderconnections.ErrClientSecretNotAccepted)
}

func TestRevokeClient_ClearsSecretIdempotently(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)
	fx := provisionSecretClient(t, ctx, ti)
	_, err := setClientID(t, ctx, ti, fx, "ciphertext-1")
	require.NoError(t, err)

	revoked, err := fx.provisioner.RevokeClient(ctx, ti.orgID, fx.connectionID)
	require.NoError(t, err)
	require.Zero(t, revoked)
	_, ok := storedSecret(t, ctx, ti, fx.connectionID)
	require.False(t, ok, "revocation clears the ciphertext")

	audited := len(auditActions(t, ctx, ti.conn, ti.orgID))
	again, err := fx.provisioner.RevokeClient(ctx, ti.orgID, fx.connectionID)
	require.NoError(t, err)
	require.Zero(t, again)
	require.Len(t, auditActions(t, ctx, ti.conn, ti.orgID), audited, "a cleared secret is not revoked twice")
	require.Zero(t, fx.kms.Opened())
}

func TestRotateClient_SecretClientHasNoKeySet(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)
	fx := provisionSecretClient(t, ctx, ti)

	_, err := fx.provisioner.RotateClient(ctx, identityproviderconnections.RotateClientParams{
		OrganizationID: ti.orgID,
		ConnectionID:   fx.connectionID,
		Provider:       identityproviderconnections.ProviderOkta,
		ActiveKid:      "",
	})
	require.ErrorIs(t, err, identityproviderconnections.ErrNoKeySet)

	_, err = fx.provisioner.ClientJSONWebKeySetURL(ctx, ti.orgID, fx.connectionID)
	require.ErrorIs(t, err, identityproviderconnections.ErrNoKeySet)
	require.Zero(t, fx.kms.Opened())
}

func TestClearClientSecret_ClearsInsideCallerTransaction(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDB(t)
	fx := provisionSecretClient(t, ctx, ti)
	_, err := setClientID(t, ctx, ti, fx, "ciphertext-1")
	require.NoError(t, err)
	audited := len(auditActions(t, ctx, ti.conn, ti.orgID))

	rolledBack := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, fx.provisioner.ClearClientSecret(ctx, rolledBack, ti.orgID, fx.connectionID))
	require.NoError(t, rolledBack.Rollback(ctx))
	_, ok := storedSecret(t, ctx, ti, fx.connectionID)
	require.True(t, ok, "nothing is cleared until the caller commits")

	dbtx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, fx.provisioner.ClearClientSecret(ctx, dbtx, ti.orgID, fx.connectionID))
	require.NoError(t, dbtx.Commit(ctx))
	_, ok = storedSecret(t, ctx, ti, fx.connectionID)
	require.False(t, ok)
	require.Len(t, auditActions(t, ctx, ti.conn, ti.orgID), audited+1)

	again := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, fx.provisioner.ClearClientSecret(ctx, again, ti.orgID, fx.connectionID))
	require.NoError(t, again.Commit(ctx))
	require.Len(t, auditActions(t, ctx, ti.conn, ti.orgID), audited+1, "a cleared secret is not audited again")
}
