package remotesessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// An issuer that trusts an identity provider for inbound ID-JAGs only, with
// no sign-in client, keeps Speakeasy as the default outbound EMA provider.
func TestEndpointEMAProviders_InboundOnlyTrustKeepsSpeakeasyDefault(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := authCtx.ActiveOrganizationID
	external := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, org, "idp-"+uuid.NewString()[:8])
	inboundOnly, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               conv.ToPGText(org),
		Slug:                         "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		TrustedRemoteSessionIssuerID: conv.ToNullUUID(external),
		TrustedRemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	internalIssuer := seedInternalSessionIssuer(t, ctx, ti, org)

	order, err := remotesessions.EndpointEMAProviders(ctx, repo.New(ti.conn), org, inboundOnly.ID, "https://vendor.example.com", true)
	require.NoError(t, err)
	require.True(t, order.IsDefault)
	require.Equal(t, []remotesessions.EMAProvider{{Kind: remotesessions.EMAProviderSpeakeasy, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: internalIssuer, Unavailable: ""}}, order.Providers)
}
