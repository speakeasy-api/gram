package identityproviderconnections_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// rotatingIssuer is an enabled issuer with a published pending key, swept by
// a provisioner that approves activation through a signer.
func rotatingIssuer(t *testing.T, paused bool) (context.Context, issuerFixture, *identityproviderconnections.Provisioner, string) {
	t.Helper()
	ctx, f := newIssuerFixture(t)
	provisioner, _ := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	before := activeIssuerKid(t, ctx, f, f.ti.orgID)
	// Each provisioner mints from one in-process key, so rotate with another.
	rotator, kms := f.provisioner(t)
	var pending *internalsessionissuers.RotationPendingError
	require.ErrorAs(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, before), &pending)
	rotator.SetEMAIssuerActivationCheck(f.signer(t, kms, paused).CheckActivation)
	return ctx, f, rotator, before
}

func makeRotationReady(t *testing.T, ctx context.Context, f issuerFixture) {
	t.Helper()
	require.NoError(t, repo.New(f.ti.conn).BackdatePendingRotationPublication(ctx, repo.BackdatePendingRotationPublicationParams{JsonWebKeySetID: issuerKeySet(t, ctx, f, f.ti.orgID), OrganizationID: f.ti.orgID, AgeSeconds: int32(internalsessionissuers.ActivationDelay / time.Second)}))
}

func TestEMAIssuer_SweepActivatesOnlyAReadyPendingKey(t *testing.T) {
	t.Parallel()
	ctx, f, provisioner, before := rotatingIssuer(t, false)

	progress, err := provisioner.SweepEMAIssuerKeysFrom(ctx, "", 0)
	require.NoError(t, err)
	require.Zero(t, progress.Activated, "a key inside the activation delay waits")
	require.Equal(t, before, activeIssuerKid(t, ctx, f, f.ti.orgID))

	makeRotationReady(t, ctx, f)
	// A request actor on the caller's context must never reach the audit row.
	admin := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  f.ti.orgID,
		UserID:                "user-1",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             nil,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	}, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"))
	progress, err = provisioner.SweepEMAIssuerKeysFrom(admin, "", 0)
	require.NoError(t, err)
	require.Equal(t, 1, progress.Activated)
	after := activeIssuerKid(t, ctx, f, f.ti.orgID)
	require.NotEqual(t, before, after)
	require.ElementsMatch(t, []string{before, after}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "the old key stays published for the overlap")
	require.Equal(t, int64(1), auditCount(t, ctx, f, audit.ActionJsonWebKeyRetire))

	for _, action := range []audit.Action{audit.ActionJsonWebKeyActivate, audit.ActionJsonWebKeyRetire} {
		row, err := audittest.LatestAuditLogByAction(ctx, f.ti.conn, action)
		require.NoError(t, err)
		require.Equal(t, string(urn.PrincipalTypeSystem), row.ActorType, "%s is a system change", action)
		require.Equal(t, "System", row.ActorDisplay)
	}

	progress, err = provisioner.SweepEMAIssuerKeysFrom(ctx, "", 0)
	require.NoError(t, err)
	require.Zero(t, progress.Activated, "nothing is left to activate")
	require.Equal(t, int64(1), auditCount(t, ctx, f, audit.ActionJsonWebKeyActivate))
}

func TestEMAIssuer_SweepLeavesPendingKeyWhenTheIssuerCannotSign(t *testing.T) {
	t.Parallel()
	for name, block := range map[string]struct {
		paused bool
		apply  func(t *testing.T, ctx context.Context, f issuerFixture, active string)
	}{
		"paused": {paused: true, apply: func(*testing.T, context.Context, issuerFixture, string) {}},
		"pause flag on": {paused: false, apply: func(_ *testing.T, _ context.Context, f issuerFixture, _ string) {
			f.flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, f.ti.orgID, true)
		}},
		"rollout off": {paused: false, apply: func(_ *testing.T, _ context.Context, f issuerFixture, _ string) {
			f.flags.SetFlag(feature.FlagInternalSessionIssuer, f.ti.orgID, false)
		}},
		"no verified domain": {paused: false, apply: func(t *testing.T, ctx context.Context, f issuerFixture, _ string) {
			t.Helper()
			f.setVerifiedDomains(t, ctx)
		}},
		"disabled": {paused: false, apply: func(t *testing.T, ctx context.Context, f issuerFixture, _ string) {
			t.Helper()
			require.NoError(t, f.lifecycle().SetDisabled(ctx, f.ti.orgID, true))
		}},
		"contained": {paused: false, apply: func(t *testing.T, ctx context.Context, f issuerFixture, active string) {
			t.Helper()
			_, err := f.lifecycle().Contain(ctx, f.ti.orgID, &active)
			require.NoError(t, err)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, f, provisioner, before := rotatingIssuer(t, block.paused)
			makeRotationReady(t, ctx, f)
			block.apply(t, ctx, f, before)

			progress, err := provisioner.SweepEMAIssuerKeysFrom(ctx, "", 0)
			require.NoError(t, err)
			require.Zero(t, progress.Activated)
			require.Zero(t, auditCount(t, ctx, f, audit.ActionJsonWebKeyActivate))
		})
	}
}

func TestEMAIssuer_SweepNeverActivatesWithoutAnActivationCheck(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	creator, _ := f.provisioner(t)
	_, err := creator.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	before := activeIssuerKid(t, ctx, f, f.ti.orgID)
	provisioner, _ := f.provisioner(t)
	var pending *internalsessionissuers.RotationPendingError
	require.ErrorAs(t, provisioner.RotateEMAIssuer(ctx, f.ti.orgID, before), &pending)
	makeRotationReady(t, ctx, f)

	progress, err := provisioner.SweepEMAIssuerKeysFrom(ctx, "", 0)
	require.NoError(t, err)
	require.Zero(t, progress.Activated)
	require.Equal(t, before, activeIssuerKid(t, ctx, f, f.ti.orgID))
}
