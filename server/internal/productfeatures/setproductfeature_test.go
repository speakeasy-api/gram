package productfeatures_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestProductFeaturesService_SetProductFeature(t *testing.T) {
	t.Parallel()

	t.Run("successfully enable feature", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestProductFeaturesService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			OrganizationID: requestedOrganizationID(ctx),
			FeatureName:    "logs",
			Enabled:        true,
		})
		require.NoError(t, err)

		// Verify feature is enabled in database
		queries := repo.New(ti.conn)
		isEnabled, err := queries.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    "logs",
		})
		require.NoError(t, err)
		require.True(t, isEnabled)
	})

	t.Run("successfully disable feature", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestProductFeaturesService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		// First enable the feature
		err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			OrganizationID: requestedOrganizationID(ctx),
			FeatureName:    "logs",
			Enabled:        true,
		})
		require.NoError(t, err)

		// Then disable it
		err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			OrganizationID: requestedOrganizationID(ctx),
			FeatureName:    "logs",
			Enabled:        false,
		})
		require.NoError(t, err)

		// Verify feature is disabled in database
		queries := repo.New(ti.conn)
		isEnabled, err := queries.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    "logs",
		})
		require.NoError(t, err)
		require.False(t, isEnabled)
	})

	t.Run("successfully enable and disable multiple times", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestProductFeaturesService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		queries := repo.New(ti.conn)

		// Enable
		err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			OrganizationID: requestedOrganizationID(ctx),
			FeatureName:    "logs",
			Enabled:        true,
		})
		require.NoError(t, err)

		isEnabled, err := queries.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    "logs",
		})
		require.NoError(t, err)
		require.True(t, isEnabled)

		// Disable
		err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			OrganizationID: requestedOrganizationID(ctx),
			FeatureName:    "logs",
			Enabled:        false,
		})
		require.NoError(t, err)

		isEnabled, err = queries.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    "logs",
		})
		require.NoError(t, err)
		require.False(t, isEnabled)

		// Enable again
		err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			OrganizationID: requestedOrganizationID(ctx),
			FeatureName:    "logs",
			Enabled:        true,
		})
		require.NoError(t, err)

		isEnabled, err = queries.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    "logs",
		})
		require.NoError(t, err)
		require.True(t, isEnabled)
	})

	t.Run("unauthorized without auth context", func(t *testing.T) {
		t.Parallel()
		_, ti := newTestProductFeaturesService(t)

		// Create a context without auth
		ctxWithoutAuth := t.Context()

		err := ti.service.SetProductFeature(ctxWithoutAuth, &gen.SetProductFeaturePayload{
			OrganizationID: "test-organization",
			FeatureName:    "logs",
			Enabled:        true,
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("uses requested organization without active organization ID", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestProductFeaturesService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		targetOrganizationID := authCtx.ActiveOrganizationID
		seedRequestedOrganizationRole(t, ctx, ti, targetOrganizationID, authz.SystemRoleAdmin)
		// The requested target is independent from the session active organization.
		authCtx.ActiveOrganizationID = ""
		ctxWithoutOrg := contextvalues.SetAuthContext(ctx, authCtx)

		err := ti.service.SetProductFeature(ctxWithoutOrg, &gen.SetProductFeaturePayload{
			OrganizationID: targetOrganizationID,
			FeatureName:    "logs",
			Enabled:        true,
		})
		require.NoError(t, err)
	})

	t.Run("unauthorized without organization ID", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestProductFeaturesService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		authCtx.ActiveOrganizationID = ""
		ctxWithoutOrg := contextvalues.SetAuthContext(ctx, authCtx)

		err := ti.service.SetProductFeature(ctxWithoutOrg, &gen.SetProductFeaturePayload{
			FeatureName: "logs",
			Enabled:     true,
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})
}

// The default test session is an org admin without the platform-admin bit, so
// it must be refused staff-only entitlements like SSO.
func TestProductFeaturesService_SetProductFeatureSSODeniedForOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    string(productfeatures.FeatureSSO),
		Enabled:        true,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	enabled, err := repo.New(ti.conn).IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureSSO),
	})
	require.NoError(t, err)
	require.False(t, enabled, "denied toggle must not write the feature row")
}

func TestProductFeaturesService_SetProductFeatureSSOAllowedForPlatformAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	ctx = withPlatformAdmin(t, ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    string(productfeatures.FeatureSSO),
		Enabled:        true,
	})
	require.NoError(t, err)

	enabled, err := repo.New(ti.conn).IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureSSO),
	})
	require.NoError(t, err)
	require.True(t, enabled)
}

// Org admins keep self-serve control of the logs toggle even with the
// platform-admin bit set — staff toggling logs must keep working too.
func TestProductFeaturesService_SetProductFeatureLogsAllowedForPlatformAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	ctx = withPlatformAdmin(t, ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    "logs",
		Enabled:        true,
	})
	require.NoError(t, err)

	enabled, err := repo.New(ti.conn).IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		FeatureName:    "logs",
	})
	require.NoError(t, err)
	require.True(t, enabled)
}

// TestFeature_RequiresPlatformAdmin pins the org-settable/staff-only split:
// the org-settable set mirrors the dashboard's self-serve organization
// settings, everything else — including features added in the future — fails
// closed to platform admins.
func TestFeature_RequiresPlatformAdmin(t *testing.T) {
	t.Parallel()

	orgSettable := []productfeatures.Feature{
		productfeatures.FeatureLogs,
		productfeatures.FeatureToolIOLogs,
		productfeatures.FeatureSessionCapture,
		productfeatures.FeatureHooksBrowserLogin,
		productfeatures.FeatureHooksFailOpen,
		productfeatures.FeatureSkillCaptureMetadataOnly,
		productfeatures.FeatureConsentToolFiltering,
		productfeatures.FeaturePlatformMCP,
	}
	for _, feature := range orgSettable {
		require.Falsef(t, feature.RequiresPlatformAdmin(), "feature %s must stay org-settable", feature)
	}

	staffOnly := []productfeatures.Feature{
		productfeatures.FeatureSSO,
		productfeatures.FeatureSCIM,
		productfeatures.FeatureSkills,
		productfeatures.FeatureAuthzChallengeLogging,
		productfeatures.FeatureCustomModelKeys,
		productfeatures.FeatureAIPlatformPushIntegrations,
		productfeatures.FeatureCustomerManagedEncryptionKeys,
		productfeatures.FeatureSessionPortability,
		productfeatures.FeatureRemoteSessionAutoRefresh,
		productfeatures.FeatureRemoteSessionAutoRefreshEnforced,
	}
	for _, feature := range staffOnly {
		require.Truef(t, feature.RequiresPlatformAdmin(), "feature %s must require platform admin", feature)
	}

	require.True(t, productfeatures.Feature("some_future_feature").RequiresPlatformAdmin(), "unknown features must fail closed")
}

func TestProductFeaturesService_SetRemoteSessionAutoRefreshPolicy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	q := repo.New(ti.conn)
	requirePolicy := func(visible, enforced bool) {
		t.Helper()

		visibleEnabled, err := q.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    string(productfeatures.FeatureRemoteSessionAutoRefresh),
		})
		require.NoError(t, err)
		require.Equal(t, visible, visibleEnabled)

		enforcedEnabled, err := q.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced),
		})
		require.NoError(t, err)
		require.Equal(t, enforced, enforcedEnabled)
	}

	err := ti.service.SetRemoteSessionAutoRefreshPolicy(ctx, &gen.SetRemoteSessionAutoRefreshPolicyPayload{
		OrganizationID: requestedOrganizationID(ctx),
		Policy:         "user_controlled",
		SessionToken:   nil,
	})
	require.NoError(t, err)
	requirePolicy(true, false)

	err = ti.service.SetRemoteSessionAutoRefreshPolicy(ctx, &gen.SetRemoteSessionAutoRefreshPolicyPayload{
		OrganizationID: requestedOrganizationID(ctx),
		Policy:         "enforced",
		SessionToken:   nil,
	})
	require.NoError(t, err)
	requirePolicy(false, true)

	err = ti.service.SetRemoteSessionAutoRefreshPolicy(ctx, &gen.SetRemoteSessionAutoRefreshPolicyPayload{
		OrganizationID: requestedOrganizationID(ctx),
		Policy:         "disabled",
		SessionToken:   nil,
	})
	require.NoError(t, err)
	requirePolicy(false, false)
}

func TestProductFeaturesService_SetRemoteSessionAutoRefreshPolicyRequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))

	err := ti.service.SetRemoteSessionAutoRefreshPolicy(ctx, &gen.SetRemoteSessionAutoRefreshPolicyPayload{
		OrganizationID: requestedOrganizationID(ctx),
		Policy:         "enforced",
		SessionToken:   nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestProductFeaturesClient_IsFeatureEnabled(t *testing.T) {
	t.Parallel()

	t.Run("returns false for disabled feature", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestProductFeaturesService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		// Use a separate redis client for the client to avoid cache pollution from the service
		redisClient, err := infra.NewRedisClient(t, 1)
		require.NoError(t, err)

		client := productfeatures.NewClient(
			testenv.NewLogger(t),
			testenv.NewTracerProvider(t),
			ti.conn,
			redisClient,
		)

		isEnabled, err := client.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureLogs)
		require.NoError(t, err)
		require.False(t, isEnabled)
	})

	t.Run("returns true for enabled feature", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestProductFeaturesService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		// Enable the feature first
		err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			OrganizationID: requestedOrganizationID(ctx),
			FeatureName:    "logs",
			Enabled:        true,
		})
		require.NoError(t, err)

		redisClient, err := infra.NewRedisClient(t, 0)
		require.NoError(t, err)

		client := productfeatures.NewClient(
			testenv.NewLogger(t),
			testenv.NewTracerProvider(t),
			ti.conn,
			redisClient,
		)

		isEnabled, err := client.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureLogs)
		require.NoError(t, err)
		require.True(t, isEnabled)
	})

	t.Run("caching works correctly", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestProductFeaturesService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		// Use a separate redis client for the client to avoid cache pollution from the service
		redisClient, err := infra.NewRedisClient(t, 1)
		require.NoError(t, err)

		client := productfeatures.NewClient(
			testenv.NewLogger(t),
			testenv.NewTracerProvider(t),
			ti.conn,
			redisClient,
		)

		// First call should hit database and cache the result
		isEnabled, err := client.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureLogs)
		require.NoError(t, err)
		require.False(t, isEnabled)

		// Enable the feature directly in the database
		queries := repo.New(ti.conn)
		_, err = queries.EnableFeature(ctx, repo.EnableFeatureParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    "logs",
		})
		require.NoError(t, err)

		// Second call should still return false because it's cached
		isEnabled, err = client.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureLogs)
		require.NoError(t, err)
		require.False(t, isEnabled, "should return cached value")

		// Create a new client with the same redis to verify cache is being used
		client2 := productfeatures.NewClient(
			testenv.NewLogger(t),
			testenv.NewTracerProvider(t),
			ti.conn,
			redisClient,
		)

		// This client should also get the cached false value
		isEnabled, err = client2.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureLogs)
		require.NoError(t, err)
		require.False(t, isEnabled, "should return cached value from same redis")
	})

	t.Run("returns false after feature is disabled", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestProductFeaturesService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		redisClient, err := infra.NewRedisClient(t, 0)
		require.NoError(t, err)

		client := productfeatures.NewClient(
			testenv.NewLogger(t),
			testenv.NewTracerProvider(t),
			ti.conn,
			redisClient,
		)

		// Enable the feature
		err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			OrganizationID: requestedOrganizationID(ctx),
			FeatureName:    "logs",
			Enabled:        true,
		})
		require.NoError(t, err)

		isEnabled, err := client.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureLogs)
		require.NoError(t, err)
		require.True(t, isEnabled)

		// Disable the feature
		err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			OrganizationID: requestedOrganizationID(ctx),
			FeatureName:    "logs",
			Enabled:        false,
		})
		require.NoError(t, err)

		isEnabled, err = client.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureLogs)
		require.NoError(t, err)
		require.False(t, isEnabled)
	})
}

func TestProductFeaturesClient_IsFeatureEnabledUncachedReturnsCancellation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	redisClient, err := infra.NewRedisClient(t, 1)
	require.NoError(t, err)
	client := productfeatures.NewClient(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		ti.conn,
		redisClient,
	)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	enabled, err := client.IsFeatureEnabledUncached(canceled, authCtx.ActiveOrganizationID, productfeatures.FeatureLogs)
	require.False(t, enabled)
	require.ErrorIs(t, err, context.Canceled)
}

func TestProductFeaturesClient_SkillsAlwaysEnabled(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	redisClient, err := infra.NewRedisClient(t, 1)
	require.NoError(t, err)
	client := productfeatures.NewClient(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		ti.conn,
		redisClient,
	)
	client.UpdateFeatureCache(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSkills, false)

	enabled, err := client.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSkills)
	require.NoError(t, err)
	require.True(t, enabled)
}
