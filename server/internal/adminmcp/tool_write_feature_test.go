package adminmcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newFeatureWriterFixture(t *testing.T, f writeFixture) (*featureWriter, *productfeatures.Client) {
	t.Helper()
	redisContainer, newRedisClient, err := testenv.NewTestRedis(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, redisContainer.Terminate(context.WithoutCancel(t.Context()))) })
	redisClient, err := newRedisClient(t, 0)
	require.NoError(t, err)
	features := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), f.db, redisClient)
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationFeature: true}} //nolint:exhaustive // Only the tested feature operation is enabled.
	return &featureWriter{runner: f.runner, mutator: productfeatures.NewMutator(features, audit.NewLogger()), writes: writes}, features
}

func featureEnabled(t *testing.T, f writeFixture, org string, feature productfeatures.Feature) bool {
	t.Helper()
	enabled, err := featurerepo.New(f.db).IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: org, FeatureName: string(feature)})
	require.NoError(t, err)
	return enabled
}

func TestFeatureWriteChangesOnlyTheExactFeatureAndOrganization(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_feature_write")
	writer, features := newFeatureWriterFixture(t, f)
	ctx := writeContext(t, f)

	for _, unreviewed := range []string{"custom_model_keys", "remote_session_auto_refresh_enforced", "session_portability", "platform_mcp", "skills", "automatic-role-distribution", ""} {
		_, err := writer.apply(ctx, FeatureInput{OrganizationID: f.orgA, Feature: unreviewed, Enabled: true})
		require.ErrorContains(t, err, "available for this write", "unreviewed feature %q stays disabled", unreviewed)
	}
	_, err := writer.apply(ctx, FeatureInput{OrganizationID: "synthetic-a", Feature: "logs", Enabled: true})
	require.ErrorIs(t, err, ErrStaleState, "a slug is not a canonical target")

	// Sequential, not subtests: features share one organization so each run
	// can check that the others are left alone.
	writable := []productfeatures.Feature{productfeatures.FeatureLogs, productfeatures.FeatureRemoteSessionAutoRefresh, productfeatures.FeatureSSO, productfeatures.FeatureSCIM}
	for _, orgID := range []string{f.orgA, f.orgB} {
		for _, feature := range writable {
			if featureEnabled(t, f, orgID, feature) {
				_, err := featurerepo.New(f.db).DeleteFeature(t.Context(), featurerepo.DeleteFeatureParams{OrganizationID: orgID, FeatureName: string(feature)})
				require.NoError(t, err)
			}
		}
	}
	for i, feature := range writable {
		receipt, err := writer.apply(ctx, FeatureInput{OrganizationID: f.orgA, Feature: string(feature), Enabled: true})
		require.NoError(t, err)
		require.Equal(t, featureReceipt{OrganizationID: f.orgA, Name: "Synthetic A", Slug: receipt.Slug, Feature: string(feature), Before: false, After: true, Changed: true, SideEffects: writableFeatures[feature]}, receipt)
		require.True(t, featureEnabled(t, f, f.orgA, feature))
		require.False(t, featureEnabled(t, f, f.orgB, feature), "a write cannot affect a second tenant")
		for _, later := range writable[i+1:] {
			require.False(t, featureEnabled(t, f, f.orgA, later), "a write changes only the named feature")
		}
		cached, err := features.IsFeatureEnabled(ctx, f.orgA, feature)
		require.NoError(t, err)
		require.True(t, cached, "the cache follows the commit")
		require.Equal(t, i+1, countWriteEvents(t, f.db, OperationSetOrganizationFeature, f.orgA, writeEventExecuted))
	}

	repeated, err := writer.apply(ctx, FeatureInput{OrganizationID: f.orgA, Feature: "logs", Enabled: true})
	require.NoError(t, err)
	require.False(t, repeated.Changed, "repeating a set is harmless")
	require.EqualValues(t, len(writable), auditCount(t, f, audit.ActionOrganizationProductFeatureEnabled))

	entry, err := audittest.LatestAuditLogByAction(t.Context(), f.db, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	require.Equal(t, f.orgA, entry.OrganizationID)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface)
	require.Equal(t, "test-client", *entry.ActingClientID)

	f.reauth()
	_, err = writer.apply(ctx, FeatureInput{OrganizationID: f.orgB, Feature: "logs", Enabled: true})
	require.ErrorIs(t, err, ErrConnectionChanged)
	require.False(t, featureEnabled(t, f, f.orgB, productfeatures.FeatureLogs))
}

func TestFeatureWriteRefusedWhileSwitchedOff(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_feature_disabled")
	writer, _ := newFeatureWriterFixture(t, f)
	writer.writes = WriteConfig{}
	_, err := writer.apply(writeContext(t, f), FeatureInput{OrganizationID: f.orgA, Feature: "logs", Enabled: true})
	require.ErrorIs(t, err, ErrWriteDisabled)
	require.False(t, featureEnabled(t, f, f.orgA, productfeatures.FeatureLogs))
}

func TestFeatureWriteRequiresStaffWriteAuthority(t *testing.T) {
	t.Parallel()
	writer := &featureWriter{writes: WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationFeature: true}}} //nolint:exhaustive // Only feature writes are enabled.
	for _, tc := range []struct {
		name string
		ctx  func(*testing.T) context.Context
		want error
	}{
		{"unauthenticated", func(t *testing.T) context.Context { t.Helper(); return t.Context() }, ErrWriteIdentity},
		{"read only staff", func(t *testing.T) context.Context { t.Helper(); return writePrincipalContext(t, []string{ScopeRead}) }, ErrWriteScope},
		{"nonstaff principal", func(t *testing.T) context.Context {
			t.Helper()
			return context.WithValue(t.Context(), principalKey{}, Principal{Subject: "user:nonstaff", Scopes: []string{ScopeRead, ScopeWrite}})
		}, ErrWriteIdentity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := writer.apply(tc.ctx(t), FeatureInput{OrganizationID: "org_role_rollout", Feature: string(productfeatures.FeatureSSO), Enabled: true})
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestFeatureWriteRefreshLeavesEnforcementAlone(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_refresh_feature")
	writer, features := newFeatureWriterFixture(t, f)
	ctx := writeContext(t, f)
	for _, enabled := range []bool{true, false} {
		receipt, err := writer.apply(ctx, FeatureInput{OrganizationID: f.orgA, Feature: string(productfeatures.FeatureRemoteSessionAutoRefresh), Enabled: enabled})
		require.NoError(t, err)
		require.Contains(t, receipt.SideEffects, "enforcement is not changed")
		require.Equal(t, enabled, featureEnabled(t, f, f.orgA, productfeatures.FeatureRemoteSessionAutoRefresh))
		require.False(t, featureEnabled(t, f, f.orgA, productfeatures.FeatureRemoteSessionAutoRefreshEnforced))
		enforcedCache, err := features.IsFeatureEnabled(ctx, f.orgA, productfeatures.FeatureRemoteSessionAutoRefreshEnforced)
		require.NoError(t, err)
		require.False(t, enforcedCache)
	}
}

func TestFeatureWriteRefreshRefusesEnforcedOrganizations(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_refresh_enforced")
	writer, _ := newFeatureWriterFixture(t, f)
	_, err := featurerepo.New(f.db).EnableFeature(t.Context(), featurerepo.EnableFeatureParams{OrganizationID: f.orgA, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced)})
	require.NoError(t, err)
	for _, enabled := range []bool{true, false} {
		_, err := writer.apply(writeContext(t, f), FeatureInput{OrganizationID: f.orgA, Feature: string(productfeatures.FeatureRemoteSessionAutoRefresh), Enabled: enabled})
		require.ErrorContains(t, err, "cannot change enforcement")
	}
	require.True(t, featureEnabled(t, f, f.orgA, productfeatures.FeatureRemoteSessionAutoRefreshEnforced))
}

func TestFeatureWriteEntitlementsPreserveConnectionState(t *testing.T) {
	t.Parallel()
	for _, feature := range []productfeatures.Feature{productfeatures.FeatureSSO, productfeatures.FeatureSCIM} {
		t.Run(string(feature), func(t *testing.T) {
			t.Parallel()
			f := newWriteFixture(t, "admin_mcp_entitlement_"+string(feature))
			writer, _ := newFeatureWriterFixture(t, f)
			ctx := writeContext(t, f)
			orgs := orgrepo.New(f.db)
			workosID := conv.ToPGText("workos_placeholder")
			_, err := orgs.SetOrgWorkosID(ctx, orgrepo.SetOrgWorkosIDParams{OrganizationID: f.orgA, WorkosID: workosID})
			require.NoError(t, err)
			require.NoError(t, orgs.SetSSOEnabled(ctx, orgrepo.SetSSOEnabledParams{WorkosID: workosID, Enabled: conv.PtrToPGBool(new(true)), WorkosLastEventID: conv.ToPGText("event_placeholder")}))
			require.NoError(t, orgs.SetSCIMEnabled(ctx, orgrepo.SetSCIMEnabledParams{WorkosID: workosID, Enabled: conv.PtrToPGBool(new(true)), WorkosLastEventID: conv.ToPGText("event_placeholder")}))
			before, err := orgs.GetOrganizationMetadata(ctx, f.orgA)
			require.NoError(t, err)
			for _, enabled := range []bool{true, false} {
				receipt, err := writer.apply(ctx, FeatureInput{OrganizationID: f.orgA, Feature: string(feature), Enabled: enabled})
				require.NoError(t, err)
				require.Contains(t, receipt.SideEffects, "setup portal links")
				require.Equal(t, enabled, featureEnabled(t, f, f.orgA, feature))
				after, err := orgs.GetOrganizationMetadata(ctx, f.orgA)
				require.NoError(t, err)
				require.Equal(t, before, after, "entitlements must not change connection status or other organization metadata")
			}
		})
	}
}

func TestFeatureWriteAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_feature_rollback")
	writer, features := newFeatureWriterFixture(t, f)
	ctx := writeContext(t, f)
	require.NoError(t, audittest.RejectAction(ctx, f.db, audit.ActionOrganizationProductFeatureEnabled))
	_, err := writer.apply(ctx, FeatureInput{OrganizationID: f.orgA, Feature: string(productfeatures.FeatureSSO), Enabled: true})
	require.Error(t, err)
	require.False(t, featureEnabled(t, f, f.orgA, productfeatures.FeatureSSO))
	cached, err := features.IsFeatureEnabled(ctx, f.orgA, productfeatures.FeatureSSO)
	require.NoError(t, err)
	require.False(t, cached, "a failed write leaves the cache alone")
	require.Zero(t, countWriteEvents(t, f.db, OperationSetOrganizationFeature, f.orgA, writeEventExecuted))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationSetOrganizationFeature, f.orgA, writeEventFailed))
}
