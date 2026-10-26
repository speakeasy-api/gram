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
	FeatureSessionPortability    Feature = "session_portability"
	FeatureAuthzChallengeLogging Feature = "authz_challenge_logging"
	FeatureSSO                   Feature = "sso"
	FeatureSCIM                  Feature = "scim"
	FeatureHooksBrowserLogin     Feature = "hooks_browser_login"
	// FeatureHooksFailOpen is the organization's single "when Speakeasy hooks
	// can't run" posture. It covers control-plane outages and timeouts, and the
	// hooks binary install-failure exit code baked at publish time.
	// Organizations that never configured it fail open; an administrator who
	// turns it off is recorded as a soft-deleted flag and fails closed.
	FeatureHooksFailOpen              Feature = "hooks_fail_open"
	FeatureCustomModelKeys            Feature = "custom_model_keys"
	FeatureSkills                     Feature = "skills"
	FeatureSkillCaptureMetadataOnly   Feature = "skill_capture_metadata_only"
	FeatureAIPlatformPushIntegrations Feature = "ai_platform_push_integrations"
	// FeaturePlatformMCP enables the organization-level Platform MCP capability,
	// including manual setup and authenticated MCP access.
	FeaturePlatformMCP Feature = "platform_mcp"

	// FeatureCustomerManagedEncryptionKeys gates the organization's ability to bring its
	// own cloud KMS keys: the external credentials Speakeasy uses to reach them and,
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
	// FeatureNetworkIngress is the durable staff-managed entitlement for private
	// network ingress. It is never included in default bundles or demo seed data.
	FeatureNetworkIngress Feature = "network_ingress"
	// FeatureSignalsIntelligence gates signals intelligence configuration access.
	FeatureSignalsIntelligence Feature = "signals_intelligence"
	// FeatureOrganizationGatewayDisabled turns the organization gateway off:
	// while set, every gateway route answers 404. Unconfigured organizations
	// also default off; a soft-deleted flag records explicit opt-in.
	FeatureOrganizationGatewayDisabled Feature = "organization_gateway_disabled"
	// FeatureCodeMode is the staff-managed entitlement for gateway Code Mode.
	// Code Mode runs customer Python on Speakeasy-operated runners, so it is
	// enabled per organization rather than for every organization on a
	// deployment with the runtime configured. Without it, Code Mode gateways
	// serve Progressive discovery.
	FeatureCodeMode Feature = "code_mode"
	// FeatureUpstreamAuthEnforced serves the organization's direct proxied
	// servers through the typed auth client resolver and requires mcp:auth on
	// the chosen auth client. Staff turn it on through the admin enforcement
	// endpoint once the organization's readiness checks pass, so it is left out
	// of the productFeatures.set enum and cannot be toggled there.
	FeatureUpstreamAuthEnforced Feature = "upstream_auth_enforced"
)

// RequiresPlatformAdmin reports whether toggling f through productFeatures.set
// requires a platform admin in addition to org:admin on the target
// organization. The org-settable cases are the toggles organization admins
// manage themselves through the dashboard's organization settings (Logs,
// Skills content upload, Platform MCP access, the
// organization gateway); every
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
		FeaturePlatformMCP,
		FeatureOrganizationGatewayDisabled:
		return false
	default:
		return true
	}
}

// EnabledUntilConfigured reports whether f reads as enabled for an
// organization that has no row for it, active or soft-deleted. Turning such a
// feature off must persist a soft-deleted row so the explicit choice survives;
// the IsFeatureEnabled queries in productfeatures, plugins, and risk encode the
// same list.
func (f Feature) EnabledUntilConfigured() bool {
	switch f {
	case FeatureOrganizationGatewayDisabled, FeatureHooksFailOpen:
		return true
	default:
		return false
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
	// Versioned so entries cached before the default changed are never read.
	if feature.EnabledUntilConfigured() {
		return fmt.Sprintf("feature:v2:%s:%s", organizationID, string(feature))
	}
	return fmt.Sprintf("feature:%s:%s", organizationID, string(feature))
}

func (f FeatureCache) TTL() time.Duration {
	return 15 * time.Minute
}
