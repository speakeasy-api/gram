package productfeatures_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

func TestProductFeatureNameContract(t *testing.T) {
	t.Parallel()

	openAPI, err := os.ReadFile("../../gen/http/openapi3.yaml")
	require.NoError(t, err)
	var document struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Enum []productfeatures.Feature `yaml:"enum"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(openAPI, &document))

	require.Equal(t, []productfeatures.Feature{
		productfeatures.FeatureLogs, productfeatures.FeatureToolIOLogs, productfeatures.FeatureSessionCapture,
		productfeatures.FeatureAuthzChallengeLogging, productfeatures.FeatureSSO, productfeatures.FeatureSCIM,
		productfeatures.FeatureHooksBrowserLogin, productfeatures.FeatureHooksFailOpen, productfeatures.FeatureCustomModelKeys,
		productfeatures.FeatureSkills, productfeatures.FeatureSkillCaptureMetadataOnly, productfeatures.FeatureAIPlatformPushIntegrations,
		productfeatures.FeaturePlatformMCP, productfeatures.FeatureCustomerManagedEncryptionKeys,
		productfeatures.FeatureRemoteSessionAutoRefresh, productfeatures.FeatureRemoteSessionAutoRefreshEnforced,
		productfeatures.FeatureConsentToolFiltering, productfeatures.FeatureSessionPortability,
		productfeatures.FeatureNetworkIngress,
	}, document.Components.Schemas["SetOrganizationFeatureRequestBody"].Properties["feature_name"].Enum)
}

func TestClientSnapshot_ReturnsCompleteProductFeatureState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	require.NoError(t, ti.client.SetFeatureEnabled(ctx, orgID, productfeatures.FeatureLogs, true))
	require.NoError(t, ti.client.SetFeatureEnabled(ctx, orgID, productfeatures.FeatureSSO, true))
	require.NoError(t, agentrepo.New(ti.conn).UpsertDeviceAgentSync(ctx, agentrepo.UpsertDeviceAgentSyncParams{
		OrganizationID: orgID,
		Email:          "dev@example.com",
	}))

	require.Equal(t, productfeatures.ProductFeaturesSnapshot{
		LogsEnabled:                             true,
		ToolIoLogsEnabled:                       false,
		SessionCaptureEnabled:                   false,
		AuthzChallengeLoggingEnabled:            false,
		SsoEnabled:                              true,
		ScimEnabled:                             false,
		HooksBrowserLoginEnabled:                false,
		HooksFailOpenEnabled:                    false,
		CustomModelKeysEnabled:                  false,
		SkillsEnabled:                           true,
		SkillCaptureMetadataOnly:                false,
		AiPlatformPushIntegrationsEnabled:       false,
		PlatformMcpEnabled:                      false,
		CustomerManagedEncryptionKeysEnabled:    false,
		RemoteSessionAutoRefreshEnabled:         false,
		RemoteSessionAutoRefreshEnforcedEnabled: false,
		ConsentToolFilteringEnabled:             false,
		SessionPortabilityEnabled:               false,
		NetworkIngressEnabled:                   false,
		DeviceAgent:                             true,
	}, ti.client.Snapshot(ctx, orgID))
}

func TestClientSnapshotStrictRejectsUnavailableFeatures(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	ctx, cancel := context.WithCancel(ctx)
	cancel()
	snapshot, err := ti.client.SnapshotStrict(ctx, orgID)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, productfeatures.ProductFeaturesSnapshot{}, snapshot)
}

func TestClientSnapshotStrictReturnsFlagsWithoutDeviceActivity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	require.NoError(t, ti.client.SetFeatureEnabled(ctx, orgID, productfeatures.FeatureSSO, true))
	require.NoError(t, agentrepo.New(ti.conn).UpsertDeviceAgentSync(ctx, agentrepo.UpsertDeviceAgentSyncParams{
		OrganizationID: orgID, Email: "dev@example.com",
	}))

	snapshot, err := ti.client.SnapshotStrict(ctx, orgID)
	require.NoError(t, err)
	require.True(t, snapshot.SsoEnabled)
	require.True(t, snapshot.SkillsEnabled)
	require.False(t, snapshot.DeviceAgent)
	require.True(t, ti.client.Snapshot(ctx, orgID).DeviceAgent)
}
