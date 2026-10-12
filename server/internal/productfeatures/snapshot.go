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
	NetworkIngressEnabled                   bool
	SignalsIntelligenceEnabled              bool
	DeviceAgent                             bool
}

// Snapshot returns the complete product-feature state for an organization.
// Individual read failures degrade to a disabled feature.
func (c *Client) Snapshot(ctx context.Context, organizationID string) ProductFeaturesSnapshot {
	snapshot, _ := c.snapshot(ctx, organizationID, false)
	return snapshot
}

// SnapshotStrict fails rather than presenting unavailable feature state as disabled.
// Unlike the dashboard snapshot, it does not include device-agent activity.
func (c *Client) SnapshotStrict(ctx context.Context, organizationID string) (ProductFeaturesSnapshot, error) {
	return c.snapshot(ctx, organizationID, true)
}

func (c *Client) snapshot(ctx context.Context, organizationID string, strict bool) (ProductFeaturesSnapshot, error) {
	var readErr error
	isEnabled := func(feature Feature) bool {
		if readErr != nil {
			return false
		}
		var enabled bool
		var err error
		if strict {
			enabled, err = c.IsFeatureEnabledUncached(ctx, organizationID, feature)
		} else {
			enabled, err = c.IsFeatureEnabled(ctx, organizationID, feature)
		}
		if err != nil {
			c.logger.WarnContext(ctx, "failed to check feature flag",
				attr.SlogError(err),
				attr.SlogOrganizationID(organizationID),
				attr.SlogProductFeatureName(string(feature)),
			)
			if strict {
				readErr = err
			}
			return false
		}

		return enabled
	}

	var deviceAgent bool
	if !strict {
		// Device agent is derived from sync activity, not an organization feature.
		var err error
		deviceAgent, err = c.repo.HasDeviceAgentSync(ctx, organizationID)
		if err != nil {
			c.logger.WarnContext(ctx, "failed to check device agent syncs",
				attr.SlogError(err),
				attr.SlogOrganizationID(organizationID),
			)
			deviceAgent = false
		}
	}

	snapshot := ProductFeaturesSnapshot{
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
		NetworkIngressEnabled:                   isEnabled(FeatureNetworkIngress),
		SignalsIntelligenceEnabled:              isEnabled(FeatureSignalsIntelligence),
		DeviceAgent:                             deviceAgent,
	}
	if readErr != nil {
		return ProductFeaturesSnapshot{}, readErr
	}
	return snapshot, nil
}
