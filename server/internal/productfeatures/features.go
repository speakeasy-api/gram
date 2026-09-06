package productfeatures

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

type Feature string

const (
	FeatureLogs           Feature = "logs"
	FeatureToolIOLogs     Feature = "tool_io_logs"
	FeatureSessionCapture Feature = "session_capture"
	// FeatureSessionPortability gates agent session portability: the device
	// agent's "continue this session in another harness" flow and the agent
	// service endpoints backing it (getSessionMeta, reportSessionMoved).
	// Sibling of FeatureSessionCapture — capture records sessions, portability
	// moves them.
	FeatureSessionPortability         Feature = "session_portability"
	FeatureAuthzChallengeLogging      Feature = "authz_challenge_logging"
	FeatureSSO                        Feature = "sso"
	FeatureSCIM                       Feature = "scim"
	FeatureHooksBrowserLogin          Feature = "hooks_browser_login"
	FeatureHooksFailOpen              Feature = "hooks_fail_open"
	FeatureCustomModelKeys            Feature = "custom_model_keys"
	FeatureSkills                     Feature = "skills"
	FeatureSkillCaptureMetadataOnly   Feature = "skill_capture_metadata_only"
	FeatureAIPlatformPushIntegrations Feature = "ai_platform_push_integrations"
	// FeaturePlatformMCP enables the organization-level Platform MCP capability,
	// including manual setup and authenticated MCP access.
	FeaturePlatformMCP Feature = "platform_mcp"

	// FeatureCustomerManagedEncryptionKeys gates the organization's ability to bring its
	// own cloud KMS keys: the external credentials Gram uses to reach them and,
	// later, the keys themselves. Distinct from FeatureCustomModelKeys, which
	// covers model provider API keys.
	FeatureCustomerManagedEncryptionKeys Feature = "customer_managed_encryption_keys"
	FeatureRemoteSessionAutoRefresh      Feature = "remote_session_auto_refresh"
	// FeatureRemoteSessionAutoRefreshEnforced makes automatic remote-session
	// refresh the organization default: it is forced on for every user, the
	// consent-screen control is shown locked (users cannot opt out), and the
	// keepalive refreshes every eligible session in the organization regardless
	// of its persisted per-session preference. Distinct from
	// FeatureRemoteSessionAutoRefresh, which only governs whether the opt-in
	// control is visible and leaves the choice to each user.
	FeatureRemoteSessionAutoRefreshEnforced Feature = "remote_session_auto_refresh_enforced"
	// FeatureConsentToolFiltering lets organization admins turn on the
	// consent-screen tool picker for their organization. Enforcement of stored
	// selections is always on; this only governs whether new approvals can
	// author one.
	FeatureConsentToolFiltering Feature = "consent_tool_filtering"
)

// RequiresPlatformAdmin reports whether toggling f through productFeatures.set
// requires a platform admin in addition to org:admin on the target
// organization. The org-settable cases are the toggles organization admins
// manage themselves through the dashboard's organization settings (Logs,
// Skills content upload, consent tool filtering, Platform MCP access); every
// other settable feature is a staff-managed entitlement, and features missing
// a case fail closed so a new feature cannot silently become self-serve.
func (f Feature) RequiresPlatformAdmin() bool {
	switch f {
	case FeatureLogs,
		FeatureToolIOLogs,
		FeatureSessionCapture,
		FeatureHooksBrowserLogin,
		FeatureHooksFailOpen,
		FeatureSkillCaptureMetadataOnly,
		FeatureConsentToolFiltering,
		FeaturePlatformMCP:
		return false
	default:
		return true
	}
}

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
