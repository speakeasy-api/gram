package productfeatures_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestMutatorSetFeature_ChangesStateAndRecordsActor(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	displayName, slug := "Feature Operator", "feature-operator"
	actor := productfeatures.MutationActor{
		Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, "feature-operator-id"),
		DisplayName: &displayName,
		Slug:        &slug,
	}

	err := productfeatures.NewMutator(ti.client, audit.NewLogger()).SetFeature(
		ctx, organizationID, productfeatures.FeatureLogs, true, actor,
	)
	require.NoError(t, err)

	enabled, err := featurerepo.New(ti.conn).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureLogs),
	})
	require.NoError(t, err)
	require.True(t, enabled)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	require.Equal(t, "feature-operator-id", record.ActorID)
	require.Equal(t, "user", record.ActorType)
	require.Equal(t, displayName, record.ActorDisplay)
	require.Equal(t, slug, record.ActorSlug)
}

func TestMutatorSetFeature_RejectsRemoteSessionAutoRefreshEnforced(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	actor := productfeatures.MutationActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, "actor")}

	err := productfeatures.NewMutator(ti.client, audit.NewLogger()).SetFeature(
		ctx, organizationID, productfeatures.FeatureRemoteSessionAutoRefreshEnforced, true, actor,
	)
	require.Error(t, err)

	enabled, queryErr := featurerepo.New(ti.conn).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced),
	})
	require.NoError(t, queryErr)
	require.False(t, enabled)
}

func TestMutatorSetFeature_RefreshesCacheAfterRequestCancellation(t *testing.T) {
	t.Parallel()
	baseCtx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, baseCtx)
	ctx, cancel := context.WithCancel(baseCtx)
	ti.redisClient.AddHook(&cancelOnRedisSetHook{cancel: cancel})
	actor := productfeatures.MutationActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, "actor")}

	require.NoError(t, productfeatures.NewMutator(ti.client, audit.NewLogger()).SetFeature(
		ctx, organizationID, productfeatures.FeatureLogs, true, actor,
	))
	require.ErrorIs(t, ctx.Err(), context.Canceled)

	_, err := featurerepo.New(ti.conn).DeleteFeature(baseCtx, featurerepo.DeleteFeatureParams{
		OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureLogs),
	})
	require.NoError(t, err)
	reader := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), ti.conn, ti.redisClient)
	enabled, err := reader.IsFeatureEnabled(baseCtx, organizationID, productfeatures.FeatureLogs)
	require.NoError(t, err)
	require.True(t, enabled)
}

func TestMutatorSetFeature_NoOpDoesNotAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	mutator := productfeatures.NewMutator(ti.client, audit.NewLogger())
	actor := productfeatures.MutationActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, "actor")}
	organizationID := activeOrganizationID(t, ctx)

	require.NoError(t, mutator.SetFeature(ctx, organizationID, productfeatures.FeatureLogs, true, actor))
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	require.NoError(t, mutator.SetFeature(ctx, organizationID, productfeatures.FeatureLogs, true, actor))
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestMutatorSetFeature_EnableSkillsProvisionsRBAC(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	q := accessrepo.New(ti.conn)
	admin := systemRolePrincipal(t, ctx, q, authz.SystemRoleAdmin)
	member := systemRolePrincipal(t, ctx, q, authz.SystemRoleMember)
	deleteGrant(t, ctx, q, organizationID, admin, authz.ScopeSkillWrite, authz.WildcardResource)
	deleteGrant(t, ctx, q, organizationID, member, authz.ScopeSkillRead, authz.WildcardResource)

	err := productfeatures.NewMutator(ti.client, audit.NewLogger()).SetFeature(ctx, organizationID, productfeatures.FeatureSkills, true, productfeatures.MutationActor{
		Principal: urn.NewPrincipal(urn.PrincipalTypeUser, "actor"),
	})
	require.NoError(t, err)

	grants := organizationGrantKeys(t, ctx, q, organizationID)
	require.Equal(t, 1, grants[grantKey(admin, authz.ScopeSkillWrite, authz.WildcardResource)])
	require.Equal(t, 1, grants[grantKey(member, authz.ScopeSkillRead, authz.WildcardResource)])
}

func TestMutatorSetFeature_DisableSkillsIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	mutator := productfeatures.NewMutator(ti.client, audit.NewLogger())
	actor := productfeatures.MutationActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, "actor")}
	require.NoError(t, mutator.SetFeature(ctx, organizationID, productfeatures.FeatureSkills, true, actor))
	before, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	require.NoError(t, mutator.SetFeature(ctx, organizationID, productfeatures.FeatureSkills, false, actor))

	enabled, err := featurerepo.New(ti.conn).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureSkills),
	})
	require.NoError(t, err)
	require.True(t, enabled)
	after, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestMutatorSetRemoteSessionAutoRefreshEnabled_ClearsEnforced(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	_, err := featurerepo.New(ti.conn).EnableFeature(ctx, featurerepo.EnableFeatureParams{
		OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced),
	})
	require.NoError(t, err)
	displayName, slug := "Refresh Operator", "refresh-operator"
	actor := productfeatures.MutationActor{
		Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, "refresh-operator-id"),
		DisplayName: &displayName,
		Slug:        &slug,
	}
	mutator := productfeatures.NewMutator(ti.client, audit.NewLogger())

	require.NoError(t, mutator.SetRemoteSessionAutoRefreshEnabled(ctx, organizationID, true, actor))

	q := featurerepo.New(ti.conn)
	visible, err := q.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefresh),
	})
	require.NoError(t, err)
	require.True(t, visible)
	enforced, err := q.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced),
	})
	require.NoError(t, err)
	require.False(t, enforced)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	require.Equal(t, "refresh-operator-id", record.ActorID)
	require.Equal(t, displayName, record.ActorDisplay)
	require.Equal(t, slug, record.ActorSlug)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, string(productfeatures.FeatureRemoteSessionAutoRefresh), metadata["feature_name"])

	before, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.NoError(t, mutator.SetRemoteSessionAutoRefreshEnabled(ctx, organizationID, true, actor))
	after, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
