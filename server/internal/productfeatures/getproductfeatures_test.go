package productfeatures_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// GetProductFeatures exposes device_agent as a member-readable signal derived
// from device-agent sync activity: false until a device has polled
// agent.getPlugins for the org, then true.
func TestProductFeaturesService_GetProductFeatures_DeviceAgent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	res, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{
		OrganizationID: requestedOrganizationID(ctx)})
	require.NoError(t, err)
	require.False(t, res.DeviceAgent, "no device has synced yet")

	// device_agent_syncs FK-references organization_metadata, so seed the org
	// row before recording a sync.
	_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Device Agent Test Org",
		Slug:        "device-agent-test-" + orgID[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	require.NoError(t, agentrepo.New(ti.conn).UpsertDeviceAgentSync(ctx, agentrepo.UpsertDeviceAgentSyncParams{
		OrganizationID: orgID,
		Email:          "dev@example.com",
	}))

	res, err = ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{
		OrganizationID: requestedOrganizationID(ctx)})
	require.NoError(t, err)
	require.True(t, res.DeviceAgent, "a device has synced")
}

func TestProductFeaturesService_PlatformMCP(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	res, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{
		OrganizationID: requestedOrganizationID(ctx)})
	require.NoError(t, err)
	require.False(t, res.PlatformMcpEnabled)

	require.NoError(t, ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    gen.ProductFeatureName(productfeatures.FeaturePlatformMCP),
		Enabled:        true,
	}))
	res, err = ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{
		OrganizationID: requestedOrganizationID(ctx)})
	require.NoError(t, err)
	require.True(t, res.PlatformMcpEnabled)
}

func TestProductFeaturesService_SkillCaptureMetadataOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	res, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{
		OrganizationID: requestedOrganizationID(ctx)})
	require.NoError(t, err)
	require.False(t, res.SkillCaptureMetadataOnly)
	require.NoError(t, ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: requestedOrganizationID(ctx),
		FeatureName:    gen.ProductFeatureName(productfeatures.FeatureSkillCaptureMetadataOnly),
		Enabled:        true,
	}))
	res, err = ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{
		OrganizationID: requestedOrganizationID(ctx)})
	require.NoError(t, err)
	require.True(t, res.SkillCaptureMetadataOnly)
}

func TestProductFeaturesService_AutomaticRoleDistributionReadback(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	org := activeOrganizationID(t, ctx)
	mutator := productfeatures.NewMutator(ti.client, audit.NewLogger())
	actor := productfeatures.MutationActor{Principal: urn.NewPrincipal(urn.PrincipalTypeSystem, "rollout-test")}
	for _, enabled := range []bool{false, true, false, true} {
		// Existing organizations have no rollout entitlement. All later transitions
		// use the same mutator as the staff HTTP and Admin MCP surfaces.
		require.NoError(t, mutator.SetFeature(ctx, org, productfeatures.FeatureAutomaticRoleDistribution, enabled, actor))
		require.Equal(t, enabled, ti.client.Snapshot(ctx, org).AutomaticRoleDistribution)
		strict, err := ti.client.SnapshotStrict(ctx, org)
		require.NoError(t, err)
		require.Equal(t, enabled, strict.AutomaticRoleDistribution)
		result, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{OrganizationID: requestedOrganizationID(ctx)})
		require.NoError(t, err)
		require.Equal(t, enabled, result.AutomaticRoleDistribution)
	}
}
