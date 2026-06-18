package productfeatures_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestClient_IsUserSessionCaptureExcluded(t *testing.T) {
	t.Parallel()

	// Use the existing service harness to get a real DB + seeded user.
	// The redis client from setup is on db=0; use db=2 for this test to
	// avoid any cross-test cache pollution.
	ctx, ti := newTestProductFeaturesService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	redisClient, err := infra.NewRedisClient(t, 2)
	require.NoError(t, err)

	client := productfeatures.NewClient(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		ti.conn,
		redisClient,
	)

	queries := repo.New(ti.conn)

	// ── empty-id short-circuit ─────────────────────────────────────────────
	// Passing an empty organizationID must return false without touching DB.
	got, err := client.IsUserSessionCaptureExcluded(ctx, "", authCtx.UserID)
	require.NoError(t, err)
	require.False(t, got, "empty organizationID must short-circuit to false")

	// Passing an empty userID must return false without touching DB.
	got, err = client.IsUserSessionCaptureExcluded(ctx, uuid.NewString(), "")
	require.NoError(t, err)
	require.False(t, got, "empty userID must short-circuit to false")

	// ── not excluded ───────────────────────────────────────────────────────
	// Use a distinct fresh org ID so this scenario's cache entry is isolated.
	notExcludedOrgID := uuid.NewString()
	got, err = client.IsUserSessionCaptureExcluded(ctx, notExcludedOrgID, authCtx.UserID)
	require.NoError(t, err)
	require.False(t, got, "user not in exclusion list must return false")

	// ── excluded ───────────────────────────────────────────────────────────
	// Use ANOTHER distinct org ID to avoid hitting the cache populated above.
	excludedOrgID := uuid.NewString()

	// Insert the exclusion FIRST (authCtx.UserID satisfies the FK on users.id).
	_, err = queries.AddSessionCaptureExclusion(ctx, repo.AddSessionCaptureExclusionParams{
		OrganizationID: excludedOrgID,
		UserID:         authCtx.UserID,
	})
	require.NoError(t, err)

	// Now the first read for this org should go to DB, find the exclusion,
	// cache it, and return true.
	got, err = client.IsUserSessionCaptureExcluded(ctx, excludedOrgID, authCtx.UserID)
	require.NoError(t, err)
	require.True(t, got, "user in exclusion list must return true")
}
