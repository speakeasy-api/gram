package productfeatures

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

type Feature string

const (
	FeatureLogs                       Feature = "logs"
	FeatureToolIOLogs                 Feature = "tool_io_logs"
	FeatureSessionCapture             Feature = "session_capture"
	FeatureAuthzChallengeLogging      Feature = "authz_challenge_logging"
	FeatureSSO                        Feature = "sso"
	FeatureSCIM                       Feature = "scim"
	FeatureHooksBrowserLogin          Feature = "hooks_browser_login"
	FeatureHooksFailOpen              Feature = "hooks_fail_open"
	FeatureCustomModelKeys            Feature = "custom_model_keys"
	FeatureSkills                     Feature = "skills"
	FeatureSkillCaptureMetadataOnly   Feature = "skill_capture_metadata_only"
	FeatureAIPlatformPushIntegrations Feature = "ai_platform_push_integrations"
	// FeaturePlatformMCP enables the organization-level Platform MCP capability.
	// The runtime also requires a separate rollout gate before admitting traffic.
	FeaturePlatformMCP Feature = "platform_mcp"
	// FeatureCustomerManagedEncryptionKeys gates the organization's ability to bring its
	// own cloud KMS keys: the external credentials Gram uses to reach them and,
	// later, the keys themselves. Distinct from FeatureCustomModelKeys, which
	// covers model provider API keys.
	FeatureCustomerManagedEncryptionKeys Feature = "customer_managed_encryption_keys"
	FeatureRemoteSessionAutoRefresh      Feature = "remote_session_auto_refresh"
)

type FeatureCache struct {
	OrganizationID string
	Feature        Feature
	Enabled        bool
}

var _ cache.CacheableObject[FeatureCache] = (*FeatureCache)(nil)

func (f FeatureCache) CacheKey() string {
	return FeatureCacheKey(f.OrganizationID, f.Feature)
}

func FeatureCacheKey(organizationID string, feature Feature) string {
	return fmt.Sprintf("feature:%s:%s", organizationID, string(feature))
}

func (f FeatureCache) TTL() time.Duration {
	return 15 * time.Minute
}
