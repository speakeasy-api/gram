package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func plantEMACredential(t *testing.T, ctx context.Context, ti *testInstance, in remotesessions.PreparationInput, provider remotesessions.EMAProviderKind, updatedAt time.Time, retired bool) {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := repo.New(ti.conn).PlantEMACredentialFixture(ctx, repo.PlantEMACredentialFixtureParams{
		OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), ProjectID: conv.ToNullUUID(*auth.ProjectID),
		UserSessionIssuerID: conv.ToNullUUID(in.UserSessionIssuerID), RemoteSessionIssuerID: conv.ToNullUUID(in.RemoteSessionIssuerID),
		RemoteSessionClientID: conv.ToNullUUID(in.ClientID), Resource: in.Resource, SubjectUrn: urn.NewUserSubject("user-" + uuid.NewString()[:8]).String(),
		EmaProvider: string(provider), Retired: retired, UpdatedAt: conv.ToPGTimestamptz(updatedAt),
	})
	require.NoError(t, err)
}

func TestReadEMA_ReportsLastAcceptedPerProvider(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	internalIssuer := seedInternalSessionIssuer(t, ctx, ti, auth.ActiveOrganizationID)
	vendor, err := remotesessions.NormalizeVendorIssuer("https://idp.example.com")
	require.NoError(t, err)
	q := repo.New(ti.conn)
	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: auth.ActiveOrganizationID, VendorIssuer: vendor, RemoteSessionIssuerID: conv.ToNullUUID(in.RemoteSessionIssuerID), InternalSessionIssuerID: uuid.NullUUID{}, Position: 0}))
	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: auth.ActiveOrganizationID, VendorIssuer: vendor, RemoteSessionIssuerID: uuid.NullUUID{}, InternalSessionIssuerID: conv.ToNullUUID(internalIssuer), Position: 1}))
	_, err = ti.service.PrepareEMA(ctx, &clientsgen.PrepareEMAPayload{
		UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
		ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: in.Resource, Scopes: in.Scopes, Mechanism: "manual",
		ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer},
	})
	require.NoError(t, err)
	payload := &clientsgen.ReadEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: in.Resource}

	read, err := ti.service.ReadEMA(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, read.EmaProviders)
	require.Len(t, read.EmaProviders.Providers, 2)
	for _, p := range read.EmaProviders.Providers {
		require.Nil(t, p.LastAcceptedAt, "no credential has been accepted yet for %s", p.Kind)
	}

	older := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	newest := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	plantEMACredential(t, ctx, ti, in, remotesessions.EMAProviderSpeakeasy, older, false)
	plantEMACredential(t, ctx, ti, in, remotesessions.EMAProviderSpeakeasy, newest, false)
	plantEMACredential(t, ctx, ti, in, remotesessions.EMAProviderSpeakeasy, time.Now().UTC(), true)
	plantEMACredential(t, ctx, ti, in, remotesessions.EMAProviderExternal, time.Now().UTC(), false)

	read, err = ti.service.ReadEMA(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, read.EmaProviders)
	byKind := map[string]*types.EMAProvider{}
	for _, p := range read.EmaProviders.Providers {
		byKind[p.Kind] = p
	}
	require.NotNil(t, byKind["speakeasy"].LastAcceptedAt)
	require.Equal(t, newest.Format(time.RFC3339), *byKind["speakeasy"].LastAcceptedAt, "the newest live Speakeasy credential wins; the retired one does not count")
	require.Equal(t, "not_trusted_by_endpoint", conv.PtrValOr(byKind["external"].UnavailableReason, ""))
	require.Nil(t, byKind["external"].LastAcceptedAt, "an external issuer the endpoint does not trust reports no acceptance")

	other := in
	other.Resource = "https://other.example.com/"
	_, err = ti.service.PrepareEMA(ctx, &clientsgen.PrepareEMAPayload{
		UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
		ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: other.Resource, Scopes: in.Scopes, Mechanism: "manual",
		ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer},
	})
	require.NoError(t, err)
	read, err = ti.service.ReadEMA(ctx, &clientsgen.ReadEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: other.Resource})
	require.NoError(t, err)
	require.NotNil(t, read.EmaProviders)
	for _, p := range read.EmaProviders.Providers {
		require.Nil(t, p.LastAcceptedAt, "another binding's credentials do not count for %s", p.Kind)
	}
}
