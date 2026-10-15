package remotesessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestListEMAServices_AggregatesBindingsAndAcceptanceAcrossTheOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := auth.ActiveOrganizationID

	empty, err := ti.service.ListEMAServices(ctx, &orgissuersgen.ListEMAServicesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, empty.Items, "no binding and no saved order means no service")

	prepare := func(resource string) {
		_, err := ti.service.PrepareEMA(ctx, &clientsgen.PrepareEMAPayload{
			UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
			ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: resource, Scopes: in.Scopes, Mechanism: "manual",
			ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer},
		})
		require.NoError(t, err)
	}
	prepare(in.Resource)
	prepare("https://other.example.com/")

	listed, err := ti.service.ListEMAServices(ctx, &orgissuersgen.ListEMAServicesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1, "both bindings name the same vendor")
	svc := listed.Items[0]
	require.Equal(t, in.RemoteSessionIssuerID.String(), conv.PtrValOr(svc.RemoteSessionIssuerID, ""))
	require.Equal(t, 2, svc.ServerCount)
	require.NotNil(t, svc.EmaProviders)
	require.True(t, svc.EmaProviders.IsDefault)
	for _, p := range svc.EmaProviders.Providers {
		require.Nil(t, p.LastAcceptedAt, "nothing accepted yet for %s", p.Kind)
	}

	older := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	newest := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	plantEMACredential(t, ctx, ti, in, remotesessions.EMAProviderExternal, older, false)
	other := in
	other.Resource = "https://other.example.com/"
	plantEMACredential(t, ctx, ti, other, remotesessions.EMAProviderExternal, newest, false)
	plantEMACredential(t, ctx, ti, other, remotesessions.EMAProviderExternal, time.Now().UTC(), true)

	listed, err = ti.service.ListEMAServices(ctx, &orgissuersgen.ListEMAServicesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	var external *string
	for _, p := range listed.Items[0].EmaProviders.Providers {
		if p.Kind == "external" {
			external = p.LastAcceptedAt
		}
	}
	require.NotNil(t, external)
	require.Equal(t, newest.Format(time.RFC3339), *external, "the newest live acceptance across every binding wins; the retired one does not count")

	vendor, err := remotesessions.NormalizeVendorIssuer("https://saved-only.example.com")
	require.NoError(t, err)
	require.NoError(t, repo.New(ti.conn).InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: org, VendorIssuer: vendor, RemoteSessionIssuerID: conv.ToNullUUID(in.RemoteSessionIssuerID), InternalSessionIssuerID: uuid.NullUUID{}, Position: 0}))
	listed, err = ti.service.ListEMAServices(ctx, &orgissuersgen.ListEMAServicesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Items, 2, "a saved order lists its vendor even with no binding")
	var savedOnly *orgissuersgen.EMAService
	for _, item := range listed.Items {
		if item.VendorIssuer == vendor {
			savedOnly = item
		}
	}
	require.NotNil(t, savedOnly)
	require.Nil(t, savedOnly.RemoteSessionIssuerID, "no issuer record names a saved-only vendor")
	require.Equal(t, 0, savedOnly.ServerCount)
	require.False(t, savedOnly.EmaProviders.IsDefault)
}
