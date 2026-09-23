//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

var errConfigurationUnavailable = errors.New("organization configuration is unavailable")

type OrganizationFeatures struct {
	OrganizationID                       string `json:"organization_id"`
	LogsEnabled                          bool   `json:"logs_enabled"`
	ToolIOLogsEnabled                    bool   `json:"tool_io_logs_enabled"`
	SessionCaptureEnabled                bool   `json:"session_capture_enabled"`
	AuthzChallengeLoggingEnabled         bool   `json:"authz_challenge_logging_enabled"`
	SSOEnabled                           bool   `json:"sso_enabled"`
	SCIMEnabled                          bool   `json:"scim_enabled"`
	HooksBrowserLoginEnabled             bool   `json:"hooks_browser_login_enabled"`
	HooksFailOpenEnabled                 bool   `json:"hooks_fail_open_enabled"`
	CustomModelKeysEnabled               bool   `json:"custom_model_keys_enabled"`
	SkillsEnabled                        bool   `json:"skills_enabled"`
	SkillCaptureMetadataOnly             bool   `json:"skill_capture_metadata_only"`
	AIPlatformPushIntegrationsEnabled    bool   `json:"ai_platform_push_integrations_enabled"`
	PlatformMCPEnabled                   bool   `json:"platform_mcp_enabled"`
	CustomerManagedEncryptionKeysEnabled bool   `json:"customer_managed_encryption_keys_enabled"`
	RemoteSessionAutoRefreshEnabled      bool   `json:"remote_session_auto_refresh_enabled"`
	RemoteSessionAutoRefreshEnforced     bool   `json:"remote_session_auto_refresh_enforced_enabled"`
	ConsentToolFilteringEnabled          bool   `json:"consent_tool_filtering_enabled"`
	SessionPortabilityEnabled            bool   `json:"session_portability_enabled"`
	NetworkIngressEnabled                bool   `json:"network_ingress_enabled"`
	DeviceAgent                          bool   `json:"device_agent"`
}

type OrganizationChatAnalysisSettings struct {
	OrganizationID         string `json:"organization_id"`
	WorkUnitsEnabled       bool   `json:"work_units_enabled"`
	WorkUnitsDailyCap      int    `json:"work_units_daily_cap"`
	BusinessMemoryEnabled  bool   `json:"business_memory_enabled"`
	BusinessMemoryDailyCap int    `json:"business_memory_daily_cap"`
	IsDefault              bool   `json:"is_default"`
}

func registerConfigurationTools(server *mcp.Server, organizations OrganizationReader, configuration ConfigurationReader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_organization_features",
		Title:       "Get Organization Features",
		Description: "Read feature and entitlement flags for an exact organization ID. These are configuration and setup signals, not usage or adoption metrics. Does not return credentials.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationIDInput) (*mcp.CallToolResult, OrganizationFeatures, error) {
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, OrganizationFeatures{}, err
		}
		if configuration == nil {
			return nil, OrganizationFeatures{}, errConfigurationUnavailable
		}
		features, err := configuration.GetOrganizationFeatures(ctx, &gen.GetOrganizationFeaturesPayload{OrganizationID: org.ID})
		if err != nil || features == nil {
			return nil, OrganizationFeatures{}, errConfigurationUnavailable
		}
		return nil, OrganizationFeatures{
			OrganizationID: org.ID, LogsEnabled: features.LogsEnabled, ToolIOLogsEnabled: features.ToolIoLogsEnabled,
			SessionCaptureEnabled: features.SessionCaptureEnabled, AuthzChallengeLoggingEnabled: features.AuthzChallengeLoggingEnabled,
			SSOEnabled: features.SsoEnabled, SCIMEnabled: features.ScimEnabled, HooksBrowserLoginEnabled: features.HooksBrowserLoginEnabled,
			HooksFailOpenEnabled: features.HooksFailOpenEnabled, CustomModelKeysEnabled: features.CustomModelKeysEnabled,
			SkillsEnabled: features.SkillsEnabled, SkillCaptureMetadataOnly: features.SkillCaptureMetadataOnly,
			AIPlatformPushIntegrationsEnabled: features.AiPlatformPushIntegrationsEnabled, PlatformMCPEnabled: features.PlatformMcpEnabled,
			CustomerManagedEncryptionKeysEnabled: features.CustomerManagedEncryptionKeysEnabled,
			RemoteSessionAutoRefreshEnabled:      features.RemoteSessionAutoRefreshEnabled,
			RemoteSessionAutoRefreshEnforced:     features.RemoteSessionAutoRefreshEnforcedEnabled,
			ConsentToolFilteringEnabled:          features.ConsentToolFilteringEnabled, SessionPortabilityEnabled: features.SessionPortabilityEnabled,
			NetworkIngressEnabled: features.NetworkIngressEnabled, DeviceAgent: features.DeviceAgent,
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_organization_chat_analysis_settings",
		Title:       "Get Organization Chat Analysis Settings",
		Description: "Read enabled state and daily caps for chat analysis judges for an exact organization ID. Does not trigger analysis or change settings.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationIDInput) (*mcp.CallToolResult, OrganizationChatAnalysisSettings, error) {
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, OrganizationChatAnalysisSettings{}, err
		}
		if configuration == nil {
			return nil, OrganizationChatAnalysisSettings{}, errConfigurationUnavailable
		}
		settings, err := configuration.GetOrganizationChatAnalysisSettings(ctx, &gen.GetOrganizationChatAnalysisSettingsPayload{OrganizationID: org.ID})
		if err != nil || settings == nil || settings.OrganizationID != org.ID {
			return nil, OrganizationChatAnalysisSettings{}, errConfigurationUnavailable
		}
		return nil, OrganizationChatAnalysisSettings{
			OrganizationID: org.ID, WorkUnitsEnabled: settings.WorkUnitsEnabled, WorkUnitsDailyCap: settings.WorkUnitsDailyCap,
			BusinessMemoryEnabled: settings.BusinessMemoryEnabled, BusinessMemoryDailyCap: settings.BusinessMemoryDailyCap,
			IsDefault: settings.IsDefault,
		}, nil
	})
}
