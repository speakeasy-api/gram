package internalsessionissuers_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/internal_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
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

	readyAt, err := time.Parse(time.RFC3339Nano, *view.RotationReadyAt)
	require.NoError(t, err)
	issuer, err := jwksrepo.New(ti.conn).GetOrganizationInternalSessionIssuer(ctx, ti.orgID)
	require.NoError(t, err)
	keys, err := jwksrepo.New(ti.conn).ListJsonWebKeys(ctx, jwksrepo.ListJsonWebKeysParams{JsonWebKeySetID: issuer.JsonWebKeySetID, OrganizationID: ti.orgID, IncludeRevoked: false})
	require.NoError(t, err)
	for _, key := range keys {
		if key.State == "pending" {
			require.True(t, key.UpdatedAt.Time.Add(internalsessionissuers.ActivationDelay).Equal(readyAt), "rotation_ready_at is the exact activation deadline, never early")
		}
	}
}

func TestRotate_CompromisedContainsWithoutVerifiedDomain(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	compromised := enable(t, ti, admin)
	setVerifiedDomains(t, ctx, ti)

	view, err := ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: &compromised, Compromised: true, SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, view.Keys, "every compromised key is withdrawn, and no replacement is minted for an issuer the organization can't use")
	require.Zero(t, view.KmsCleanupPending, "the compromised versions are disabled")
	require.Equal(t, wantIssuer(ti), *view.Issuer, "the issuer URI never changes")
	require.Nil(t, view.ReplacementError, "a skipped replacement is not a failure")
}

func TestRotate_CompromisedNamesTheActiveKeyAndCoalesces(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	compromised := enable(t, ti, admin)

	_, err := ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: nil, Compromised: true, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	first, err := ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: &compromised, Compromised: true, SessionToken: nil})
	require.NoError(t, err)
	require.Nil(t, first.ReplacementError)
	again, err := ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: &compromised, Compromised: true, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, first.Keys, again.Keys, "a repeat for the replaced key mints nothing")
}

func TestRotate_ContainmentIgnoresFailingProviders(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	admin := adminCtx(t, ctx)
	compromised := enable(t, ti, admin)
	ti.providersFail.Store(true)

	retained, err := ti.service.Get(readCtx(t, ctx), &gen.GetPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "signing_paused", retained.State)
	require.False(t, retained.CanEnable)
	require.Equal(t, wantIssuer(ti), *retained.Issuer)

	view, err := ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: &compromised, Compromised: true, SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, view.Keys, "containment withdraws every key; with providers down no replacement is minted")
	require.Equal(t, "keyless", view.State, "with providers down the database state shows the issuer can't sign")

	disabled, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: false, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "disabled", disabled.State)
}

func TestRotate_ContainmentWithoutKeyManager(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, false)
	admin := adminCtx(t, ctx)
	provisionIssuer(t, ctx, ti)
	view, err := ti.service.Get(readCtx(t, ctx), &gen.GetPayload{SessionToken: nil})
	require.NoError(t, err)
	compromised := view.Keys[0].Kid

	contained, err := ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: &compromised, Compromised: true, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "keyless", contained.State)
	require.Empty(t, contained.Keys, "the compromised key is withdrawn without KMS")
	require.Equal(t, 1, contained.KmsCleanupPending, "its KMS cleanup is recorded for the sweep")

	disabled, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: false, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "disabled", disabled.State)
}

func TestMinting_FailsClosedWithoutTheRateLimiter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestServiceWithMintStore(t, true, true)
	admin := adminCtx(t, ctx)
	_, err := ti.service.SetEnabled(admin, &gen.SetEnabledPayload{Enabled: true, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeUnavailable)

	provisionIssuer(t, ctx, ti)
	view, err := ti.service.Get(readCtx(t, ctx), &gen.GetPayload{SessionToken: nil})
	require.NoError(t, err)
	compromised := view.Keys[0].Kid
	contained, err := ti.service.Rotate(admin, &gen.RotatePayload{ActiveKid: &compromised, Compromised: true, SessionToken: nil})
	require.NoError(t, err, "revocation never depends on the limiter")
	require.Equal(t, "keyless", contained.State, "the replacement waits for the limiter")
	require.Empty(t, contained.Keys)
	require.NotNil(t, contained.ReplacementError, "a deferred replacement is reported, not shown as done")
}
