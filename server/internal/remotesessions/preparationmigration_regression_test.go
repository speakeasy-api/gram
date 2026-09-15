package remotesessions_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

func TestPreparationMigrationPreflightReportsBindings(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{preparationJWTGrant})
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	target := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, auth.ActiveOrganizationID, "ema-migration-target")

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, migratePreflightPayload(in.RemoteSessionIssuerID.String(), target.String()))
	require.NoError(t, err)
	require.Equal(t, int64(1), preflight.EmaBindingCount)
	require.False(t, preflight.CanMigrate)

	in.ExpectedGeneration = prepared.Generation
	_, err = ti.service.UnlinkIdentityChaining(ctx, in)
	require.NoError(t, err)
	preflight, err = ti.service.GetIssuerMigratePreflight(ctx, migratePreflightPayload(in.RemoteSessionIssuerID.String(), target.String()))
	require.NoError(t, err)
	require.Zero(t, preflight.EmaBindingCount)
	require.True(t, preflight.CanMigrate)
}
