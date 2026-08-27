package productfeatures_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

func TestSetProductFeatureEnableRecordsGenericAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    "logs",
		Enabled:        true,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	require.Equal(t, "organization", record.SubjectType)
	require.Equal(t, authCtx.ActiveOrganizationID, record.OrganizationID)
	require.False(t, record.ProjectID.Valid, "org-scoped event must carry no project")

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, "logs", metadata["feature_name"])
}

func TestSetProductFeatureDisableRecordsGenericAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    "logs",
		Enabled:        true,
	})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureDisabled)
	require.NoError(t, err)

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    "logs",
		Enabled:        false,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureDisabled)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureDisabled)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, "logs", metadata["feature_name"])
}

// A no-op set (same value it already holds) must not record an audit event —
// the trail reflects actual transitions, mirroring the hooks_fail_open tests.
func TestSetProductFeatureNoOpSkipsGenericAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    "logs",
		Enabled:        true,
	})
	require.NoError(t, err)

	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    "logs",
		Enabled:        true,
	})
	require.NoError(t, err)

	again, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	require.Equal(t, count, again, "a no-op set must not record a duplicate audit event")
}

// hooks_fail_open keeps its dedicated audit action; it must not also produce
// the generic product-feature event.
func TestSetProductFeatureHooksFailOpenSkipsGenericAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    string(productfeatures.FeatureHooksFailOpen),
		Enabled:        true,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// Skills enablement flows through EnableSkillsTx rather than the plain
// enable path; its transition must still land in the audit trail.
func TestSetProductFeatureSkillsEnableRecordsGenericAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	ctx = withPlatformAdmin(t, ctx)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    string(productfeatures.FeatureSkills),
		Enabled:        true,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, string(productfeatures.FeatureSkills), metadata["feature_name"])

	// A replayed enable is a no-op and must not duplicate the event.
	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    string(productfeatures.FeatureSkills),
		Enabled:        true,
	})
	require.NoError(t, err)

	again, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	require.Equal(t, after, again)
}
