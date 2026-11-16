package internalsessionissuers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/internal_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	require.True(t, view.CanEnable)
	require.Nil(t, view.Issuer)
	require.Empty(t, view.Keys)

	ti.flags.SetFlag(feature.FlagInternalSessionIssuer, ti.orgID, false)
	require.Equal(t, "unavailable", get().State)
	ti.flags.SetFlag(feature.FlagInternalSessionIssuer, ti.orgID, true)
	setVerifiedDomains(t, ctx, ti)
	require.Equal(t, "verified_domain_required", get().State)
	setVerifiedDomains(t, ctx, ti, "example.com")

	_, err := ti.service.SetEnabled(adminCtx(t, ctx), &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	require.NoError(t, err)
	view = get()
	require.Equal(t, "enabled", view.State)
	require.False(t, view.CanEnable)
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
	require.False(t, view.CanEnable)
}

func TestGet_DisabledEnableEligibility(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	enable(t, ti, admin)
	disabled, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: false, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "disabled", disabled.State)
	require.True(t, disabled.CanEnable)

	ti.flags.SetFlag(feature.FlagInternalSessionIssuer, ti.orgID, false)
	view, err := ti.service.Get(readCtx(t, ctx), &gen.GetPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "disabled", view.State, "retained lifecycle state does not imply enable eligibility")
	require.False(t, view.CanEnable)

	ti.flags.SetFlag(feature.FlagInternalSessionIssuer, ti.orgID, true)
	setVerifiedDomains(t, ctx, ti)
	view, err = ti.service.Get(readCtx(t, ctx), &gen.GetPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "disabled", view.State)
	require.False(t, view.CanEnable)

	setVerifiedDomains(t, ctx, ti, "example.com")
	view, err = ti.service.Get(readCtx(t, ctx), &gen.GetPayload{SessionToken: nil})
	require.NoError(t, err)
	require.True(t, view.CanEnable)
}

func TestSetEnabled_DisabledFallbackNeverOffersEnable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	enable(t, ti, admin)
	ti.providersFail.Store(true)

	view, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: false, SessionToken: nil})
	require.NoError(t, err, "containment works during a flag provider outage")
	require.Equal(t, "disabled", view.State)
	require.False(t, view.CanEnable, "database fallback cannot establish enable eligibility")
}

func TestGet_DisabledWithoutProvisioningCannotEnable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, false)
	provisionIssuer(t, ctx, ti)
	view, err := ti.service.SetEnabled(adminCtx(t, ctx), &gen.SetEnabledPayload{Enabled: false, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "disabled", view.State)
	require.False(t, view.CanEnable)
}

func TestGet_RetainsIssuerDuringAuthorityFailure(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	enable(t, ti, admin)
	ti.gate.SetSigningAuthority(func(context.Context, string, internalsessionissuers.Status) error {
		return errors.New("signing authority unavailable")
	})

	view, err := ti.service.Get(readCtx(t, ctx), &gen.GetPayload{SessionToken: nil})
	require.NoError(t, err, "org readers retain the trust anchor during a readiness outage")
	require.Equal(t, wantIssuer(ti), *view.Issuer)
	require.Equal(t, "signing_paused", view.State)
	require.False(t, view.CanEnable)

	disabled, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: false, SessionToken: nil})
	require.NoError(t, err, "the outage does not prevent containment")
	require.Equal(t, "disabled", disabled.State)
}

// With no issuer there is nothing to disable or contain, so a flag provider
// outage reads as not offered rather than an error every admin would see.
func TestGet_WithoutIssuerReadsUnavailableWhenStatusFails(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	ti.providersFail.Store(true)
	view, err := ti.service.Get(readCtx(t, ctx), &gen.GetPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "unavailable", view.State)
	require.False(t, view.CanEnable)
	require.Nil(t, view.Issuer)
}

func TestSetEnabled_RequiresEnableAuthority(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	ti.gate.SetEnableAuthority(func(string, internalsessionissuers.Status) error {
		return internalsessionissuers.ErrMisconfigured
	})
	_, err := ti.service.SetEnabled(adminCtx(t, ctx), &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeUnavailable)
	view, err := ti.service.Get(readCtx(t, ctx), &gen.GetPayload{SessionToken: nil})
	require.NoError(t, err)
	require.False(t, view.CanEnable)
	require.Nil(t, view.Issuer, "unavailable authority must not provision issuer keys")
}

func TestStatus_StalePauseFlagPausesSigning(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	enable(t, ti, adminCtx(t, ctx))

	status, err := ti.gate.Status(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, internalsessionissuers.StateEnabled, status.State, "a current off pause switch lets the issuer sign")

	ti.flagsStaleFor.Store(int64(internalsessionissuers.PauseFlagMaxAge + time.Second))
	status, err = ti.gate.Status(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, internalsessionissuers.StateSigningPaused, status.State, "an off read from definitions the provider stopped refreshing is indeterminate")
	ready, err := ti.gate.Ready(ctx, ti.orgID, status.Issuer.ID)
	require.NoError(t, err)
	require.False(t, ready, "signing is refused")
}
