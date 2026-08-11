package productfeatures_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestSetProductFeatureHooksFailOpenEnableRecordsAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenEnabled)
	require.NoError(t, err)

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: "hooks_fail_open",
		Enabled:     true,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenEnabled)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenEnabled)
	require.NoError(t, err)
	require.Equal(t, "organization", record.SubjectType)
	// Subject identity comes from the organization_metadata row (seeded in
	// setup), not the auth context — the display name and slug are distinct.
	require.Equal(t, "Product Features Test Org", record.SubjectDisplay)
	require.Equal(t, authCtx.ActiveOrganizationID, record.SubjectSlug)
	require.False(t, record.ProjectID.Valid, "org-scoped event must carry no project")
}

func TestSetProductFeatureHooksFailOpenDisableRecordsAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: "hooks_fail_open",
		Enabled:     true,
	})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenDisabled)
	require.NoError(t, err)

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: "hooks_fail_open",
		Enabled:     false,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenDisabled)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

// TestSetProductFeatureHooksFailOpenNoOpSkipsAudit: setting the value it
// already holds records no event — the audit trail reflects actual changes.
func TestSetProductFeatureHooksFailOpenNoOpSkipsAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: "hooks_fail_open",
		Enabled:     true,
	})
	require.NoError(t, err)

	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenEnabled)
	require.NoError(t, err)

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: "hooks_fail_open",
		Enabled:     true,
	})
	require.NoError(t, err)

	again, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenEnabled)
	require.NoError(t, err)
	require.Equal(t, count, again, "a no-op set must not record a duplicate audit event")
}

// TestSetProductFeatureDisableWhenNeverEnabledIsNoOp: disabling a feature
// that was never enabled succeeds without recording an audit event, mirroring
// how enabling an already-enabled feature is a no-op.
func TestSetProductFeatureDisableWhenNeverEnabledIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenDisabled)
	require.NoError(t, err)

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: "hooks_fail_open",
		Enabled:     false,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenDisabled)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestSetProductFeatureOtherFeatureSkipsHooksFailOpenAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	beforeEnabled, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenEnabled)
	require.NoError(t, err)

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: "logs",
		Enabled:     true,
	})
	require.NoError(t, err)

	afterEnabled, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenEnabled)
	require.NoError(t, err)
	require.Equal(t, beforeEnabled, afterEnabled)
}

func TestGetProductFeaturesHooksFailOpen(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	result, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{})
	require.NoError(t, err)
	require.False(t, result.HooksFailOpenEnabled, "fail open must default to off")

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: "hooks_fail_open",
		Enabled:     true,
	})
	require.NoError(t, err)

	result, err = ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{})
	require.NoError(t, err)
	require.True(t, result.HooksFailOpenEnabled)
}
