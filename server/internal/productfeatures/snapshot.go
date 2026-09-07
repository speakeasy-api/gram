package productfeatures

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// ProductFeaturesSnapshot is the complete product-feature state for an organization.
type ProductFeaturesSnapshot struct {
	LogsEnabled                             bool
	ToolIoLogsEnabled                       bool
	SessionCaptureEnabled                   bool
	AuthzChallengeLoggingEnabled            bool
	SsoEnabled                              bool
	ScimEnabled                             bool
	HooksBrowserLoginEnabled                bool
	HooksFailOpenEnabled                    bool
	CustomModelKeysEnabled                  bool
	SkillsEnabled                           bool
	SkillCaptureMetadataOnly                bool
	AiPlatformPushIntegrationsEnabled       bool
	PlatformMcpEnabled                      bool
	CustomerManagedEncryptionKeysEnabled    bool
	RemoteSessionAutoRefreshEnabled         bool
	RemoteSessionAutoRefreshEnforcedEnabled bool
	ConsentToolFilteringEnabled             bool
	SessionPortabilityEnabled               bool
	DeviceAgent                             bool
}

// Snapshot returns the complete product-feature state for an organization.
// Individual read failures degrade to a disabled feature.
func (c *Client) Snapshot(ctx context.Context, organizationID string) ProductFeaturesSnapshot {
	isEnabled := func(feature Feature) bool {
		enabled, err := c.IsFeatureEnabled(ctx, organizationID, feature)
		if err != nil {
			c.logger.WarnContext(ctx, "failed to check feature flag",
				attr.SlogError(err),
				attr.SlogOrganizationID(organizationID),
				attr.SlogProductFeatureName(string(feature)),
			)
			return false
		}

		return enabled
	}

	// Device agent is derived from sync activity, not an organization feature.
	deviceAgent, err := c.repo.HasDeviceAgentSync(ctx, organizationID)
	if err != nil {
		c.logger.WarnContext(ctx, "failed to check device agent syncs",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
		deviceAgent = false
	}

	return ProductFeaturesSnapshot{
		LogsEnabled:                             isEnabled(FeatureLogs),
		ToolIoLogsEnabled:                       isEnabled(FeatureToolIOLogs),
		SessionCaptureEnabled:                   isEnabled(FeatureSessionCapture),
		AuthzChallengeLoggingEnabled:            isEnabled(FeatureAuthzChallengeLogging),
		SsoEnabled:                              isEnabled(FeatureSSO),
		ScimEnabled:                             isEnabled(FeatureSCIM),
		HooksBrowserLoginEnabled:                isEnabled(FeatureHooksBrowserLogin),
		HooksFailOpenEnabled:                    isEnabled(FeatureHooksFailOpen),
		CustomModelKeysEnabled:                  isEnabled(FeatureCustomModelKeys),
		SkillsEnabled:                           true,
		SkillCaptureMetadataOnly:                isEnabled(FeatureSkillCaptureMetadataOnly),
		AiPlatformPushIntegrationsEnabled:       isEnabled(FeatureAIPlatformPushIntegrations),
		PlatformMcpEnabled:                      isEnabled(FeaturePlatformMCP),
		CustomerManagedEncryptionKeysEnabled:    isEnabled(FeatureCustomerManagedEncryptionKeys),
		RemoteSessionAutoRefreshEnabled:         isEnabled(FeatureRemoteSessionAutoRefresh),
		RemoteSessionAutoRefreshEnforcedEnabled: isEnabled(FeatureRemoteSessionAutoRefreshEnforced),
		ConsentToolFilteringEnabled:             isEnabled(FeatureConsentToolFiltering),
		SessionPortabilityEnabled:               isEnabled(FeatureSessionPortability),
		DeviceAgent:                             deviceAgent,
	}
}
