package internalsessionissuers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/internal_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
)

func enable(t *testing.T, ti *testInstance, admin context.Context) string {
	t.Helper()
	view, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	require.NoError(t, err)
	return view.Keys[0].Kid
}

func TestRotate_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	kid := "kid"
	_, err := ti.service.Rotate(readCtx(t, ctx), &gen.RotatePayload{ActiveKid: &kid, Compromised: false, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestRotate_NeedsAnIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	_, err := ti.service.Rotate(adminCtx(t, ctx), &gen.RotatePayload{ActiveKid: nil, Compromised: true, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRotate_Routine(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	active := enable(t, ti, admin)

	_, err := ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: nil, Compromised: false, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	unknown := "never-published"
	_, err = ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: &unknown, Compromised: false, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	view, err := ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: &active, Compromised: false, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, view.Keys, 2, "the new key is published, not yet active")
	require.NotNil(t, view.RotationReadyAt)
	require.Equal(t, wantIssuer(ti), *view.Issuer)
}

func TestRotate_CompromisedWorksWithoutEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	compromised := enable(t, ti, admin)
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureSSO)

	view, err := ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: nil, Compromised: true, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, view.Keys, 1)
	require.NotEqual(t, compromised, view.Keys[0].Kid, "every compromised key is withdrawn")
	require.Equal(t, "active", view.Keys[0].State)
	require.Equal(t, wantIssuer(ti), *view.Issuer, "the issuer URI never changes")
}
