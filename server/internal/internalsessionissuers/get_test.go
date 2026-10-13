package internalsessionissuers_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/internal_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
)

func TestGet_RequiresOrgRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	_, err := ti.service.Get(authztest.WithExactGrants(t, ctx), &gen.GetPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGet_ReportsState(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	read := readCtx(t, ctx)
	get := func() *gen.InternalSessionIssuer {
		t.Helper()
		view, err := ti.service.Get(read, &gen.GetPayload{SessionToken: nil})
		require.NoError(t, err)
		return view
	}

	view := get()
	require.Equal(t, "setup_required", view.State)
	require.Nil(t, view.Issuer)
	require.Empty(t, view.Keys)

	ti.flags.SetFlag(feature.FlagInternalSessionIssuer, ti.orgID, false)
	require.Equal(t, "unavailable", get().State)
	ti.flags.SetFlag(feature.FlagInternalSessionIssuer, ti.orgID, true)
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureSSO)
	require.Equal(t, "entitlement_required", get().State)
	productfeaturestest.Enable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureSSO)

	_, err := ti.service.SetEnabled(adminCtx(t, ctx), &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	require.NoError(t, err)
	view = get()
	require.Equal(t, "enabled", view.State)
	require.Equal(t, wantIssuer(ti), *view.Issuer)
	require.Equal(t, wantIssuer(ti)+"/jwks", *view.JwksURI)
	require.Len(t, view.Keys, 1)
	require.Equal(t, "active", view.Keys[0].State)
	require.Nil(t, view.RotationReadyAt)

	ti.flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, ti.orgID, true)
	require.Equal(t, "signing_paused", get().State)
	ti.flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, ti.orgID, false)

	_, err = ti.service.Rotate(adminCtx(t, ctx), &gen.RotatePayload{ActiveKid: &view.Keys[0].Kid, Compromised: false, SessionToken: nil})
	require.NoError(t, err)
	pending := get()
	require.Len(t, pending.Keys, 2)
	require.NotNil(t, pending.RotationReadyAt, "get reports when a pending rotation can activate")
}

func TestGet_WithoutProvisioning(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, false)
	view, err := ti.service.Get(readCtx(t, ctx), &gen.GetPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "unavailable", view.State)
}
