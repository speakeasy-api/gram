package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func plantEMACredential(t *testing.T, ctx context.Context, ti *testInstance, in remotesessions.PreparationInput, provider remotesessions.EMAProviderKind, updatedAt time.Time, retired bool, internalIssuer ...uuid.UUID) {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	signer := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if len(internalIssuer) > 0 {
		signer = conv.ToNullUUID(internalIssuer[0])
	}
	_, err := repo.New(ti.conn).PlantEMACredentialFixture(ctx, repo.PlantEMACredentialFixtureParams{
		OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), ProjectID: conv.ToNullUUID(*auth.ProjectID),
		UserSessionIssuerID: conv.ToNullUUID(in.UserSessionIssuerID), RemoteSessionIssuerID: conv.ToNullUUID(in.RemoteSessionIssuerID),
		RemoteSessionClientID: conv.ToNullUUID(in.ClientID), Resource: in.Resource, SubjectUrn: urn.NewUserSubject("user-" + uuid.NewString()[:8]).String(),
		EmaProvider: string(provider), Retired: retired, UpdatedAt: conv.ToPGTimestamptz(updatedAt),
		InternalSessionIssuerID: signer,
	})
	require.NoError(t, err)
}

func TestReadEMA_ReportsVendorAndBindingAcceptance(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.SetSpeakeasySigningAvailable(true)
	internalIssuer := seedInternalSessionIssuer(t, ctx, ti, auth.ActiveOrganizationID)
	vendor, err := remotesessions.NormalizeVendorIssuer("https://idp.example.com")
	require.NoError(t, err)
	q := repo.New(ti.conn)
	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: auth.ActiveOrganizationID, VendorIssuer: vendor, RemoteSessionIssuerID: uuid.NullUUID{}, InternalSessionIssuerID: conv.ToNullUUID(internalIssuer), Position: 0}))
	prepare := func(resource string) {
		_, err := ti.service.PrepareEMA(ctx, &clientsgen.PrepareEMAPayload{
			UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
			ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: resource, Scopes: in.Scopes, Mechanism: "manual",
			ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer},
		})
		require.NoError(t, err)
	}
	prepare(in.Resource)
	other := in
	other.Resource = "https://other.example.com/"
	prepare(other.Resource)
	read := func(resource string) *clientsgen.IdentityChainingPreparation {
		out, err := ti.service.ReadEMA(ctx, &clientsgen.ReadEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: resource})
		require.NoError(t, err)
		require.NotNil(t, out.EmaProviders)
		return out
	}

	first := read(in.Resource)
	require.Nil(t, first.LastAcceptedAt)
	require.Nil(t, first.EmaProviders.Providers[0].LastAcceptedAt)

	older := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	newest := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	plantEMACredential(t, ctx, ti, other, remotesessions.EMAProviderSpeakeasy, older, false, internalIssuer)
	plantEMACredential(t, ctx, ti, other, remotesessions.EMAProviderSpeakeasy, newest, true, internalIssuer)
	plantEMACredential(t, ctx, ti, other, remotesessions.EMAProviderSpeakeasy, time.Now().UTC(), false)

	first = read(in.Resource)
	require.Nil(t, first.LastAcceptedAt, "this binding has never been accepted")
	require.NotNil(t, first.EmaProviders.Providers[0].LastAcceptedAt)
	require.Equal(t, older.Format(time.RFC3339), *first.EmaProviders.Providers[0].LastAcceptedAt,
		"the vendor trusts this Speakeasy issuer because another server's assertion was accepted; only a live, attributed credential counts")

	second := read(other.Resource)
	require.NotNil(t, second.LastAcceptedAt)
	require.Equal(t, older.Format(time.RFC3339), *second.LastAcceptedAt)

	// Moving the vendor to a provider that has never been accepted, and that
	// this sign-in can't use, leaves the binding unproven.
	external := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, auth.ActiveOrganizationID, "idp-"+uuid.NewString()[:8])
	_, err = q.DeleteEMAProviders(ctx, repo.DeleteEMAProvidersParams{OrganizationID: auth.ActiveOrganizationID, VendorIssuer: vendor})
	require.NoError(t, err)
	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: auth.ActiveOrganizationID, VendorIssuer: vendor, RemoteSessionIssuerID: conv.ToNullUUID(external), InternalSessionIssuerID: uuid.NullUUID{}, Position: 0}))
	require.Nil(t, read(other.Resource).LastAcceptedAt, "acceptance from a provider the order no longer tries proves nothing")
}
