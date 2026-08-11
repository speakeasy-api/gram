package productfeatures_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
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

	res, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{})
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

	res, err = ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{})
	require.NoError(t, err)
	require.True(t, res.DeviceAgent, "a device has synced")
}

func TestProductFeaturesService_SkillCaptureMetadataOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	res, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{})
	require.NoError(t, err)
	require.False(t, res.SkillCaptureMetadataOnly)
	require.NoError(t, ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureSkillCaptureMetadataOnly),
		Enabled:     true,
	}))
	res, err = ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{})
	require.NoError(t, err)
	require.True(t, res.SkillCaptureMetadataOnly)
}
