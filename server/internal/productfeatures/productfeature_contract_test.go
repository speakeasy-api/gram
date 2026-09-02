package productfeatures_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

func TestProductFeatureNameContract(t *testing.T) {
	t.Parallel()

	require.Equal(t, []productfeatures.Feature{
		"logs", "tool_io_logs", "session_capture",
		"authz_challenge_logging", "sso", "scim",
		"hooks_browser_login", "hooks_fail_open", "custom_model_keys",
		"skills", "skill_capture_metadata_only", "ai_platform_push_integrations",
		"platform_mcp", "customer_managed_encryption_keys",
		"remote_session_auto_refresh", "remote_session_auto_refresh_enforced",
		"consent_tool_filtering", "session_portability",
	}, []productfeatures.Feature{
		productfeatures.FeatureLogs, productfeatures.FeatureToolIOLogs, productfeatures.FeatureSessionCapture,
		productfeatures.FeatureAuthzChallengeLogging, productfeatures.FeatureSSO, productfeatures.FeatureSCIM,
		productfeatures.FeatureHooksBrowserLogin, productfeatures.FeatureHooksFailOpen, productfeatures.FeatureCustomModelKeys,
		productfeatures.FeatureSkills, productfeatures.FeatureSkillCaptureMetadataOnly, productfeatures.FeatureAIPlatformPushIntegrations,
		productfeatures.FeaturePlatformMCP, productfeatures.FeatureCustomerManagedEncryptionKeys,
		productfeatures.FeatureRemoteSessionAutoRefresh, productfeatures.FeatureRemoteSessionAutoRefreshEnforced,
		productfeatures.FeatureConsentToolFiltering, productfeatures.FeatureSessionPortability,
	})
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
		DeviceAgent:                             true,
	}, ti.client.Snapshot(ctx, orgID))
}
