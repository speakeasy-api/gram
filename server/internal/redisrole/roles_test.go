package redisrole_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/redisrole"
)

func TestEvictablePrefixesEndInColon(t *testing.T) {
	t.Parallel()

	for _, prefix := range redisrole.EvictablePrefixes() {
		require.True(t, strings.HasSuffix(prefix, ":"), "prefix %q must end in a colon", prefix)
	}
}

func TestEvictablePrefixesDoNotOverlap(t *testing.T) {
	t.Parallel()

	prefixes := redisrole.EvictablePrefixes()
	for i, a := range prefixes {
		for j, b := range prefixes {
			if i == j {
				continue
			}
			require.False(t, strings.HasPrefix(a, b), "prefix %q captures %q", b, a)
		}
	}
}

// TestOfClassifiesKnownFamilies pins one sample key of every known key
// family to its role, so a new or widened prefix cannot capture a sessions
// family unnoticed.
func TestOfClassifiesKnownFamilies(t *testing.T) {
	t.Parallel()

	evictable := []string{
		"admin_hook_clients:30:",
		"chat:turnframes:chat-id",
		"clientCredentials:sha:",
		"clientCredentialsFailure:sha:",
		"clientCredentialsLease:sha",
		"clientCredentialsTokenCache:projectID-p-scopes-s:",
		"emaEndpointResource:v1:sha",
		"federated-oidc-metadata:v3:sha",
		"feature:org:feature:",
		"feature:v2:org:feature:",
		"mcpClientInfo:{project:toolset}:sha",
		"mcpClientInfoLive:{project:toolset}",
		"mcpservers:tool_disposition:v4:project:server:",
		"openrouter:model:context_window:id:",
		"openrouter:model:pricing:id:",
		"otelUserEnrichment:v1:org:hash:",
		"plugins:has-collaborator:owner/repo",
		"plugins:live-version:owner/repo",
		"plugins:held-entries:project:sha",
		"polar_customer_state:org:",
		"polar_product:id:",
		"registry:list:v1:url:registry:",
		"registry:details:v1:url:hash:",
		"sessionPolicy:v2:issuer:jti:",
		"shadow_mcp_policy_enabled:project:",
		"slack-bot-identity:hash:",
		"toolset:a:b:1:",
		"userInfo:user:",
		"userInfoSnapshot:org:user:",
	}
	for _, key := range evictable {
		t.Run("evictable/"+key, func(t *testing.T) {
			t.Parallel()
			role, prefix := redisrole.Of(key)
			require.Equal(t, redisrole.RoleEvictable, role)
			require.True(t, strings.HasPrefix(key, prefix), "matched %q", prefix)
		})
	}

	sessions := []string{
		"adminLoginState:state:",
		"adminMCPChallenge:id:",
		"adminSession:sid:",
		"adminSession:refresh:sid",
		"agentInstall:code:",
		"agentUserSessionGrant:issuer:code:",
		"gatewayCatalogGrant:issuer:code:",
		"gatewayUserSessionGrant:issuer:code:",
		"userSessionGrant:issuer:code:",
		"ahp-upload:project:id",
		"assetsPlatformImageUpload:sha",
		"assistant-runtime-credential:cred",
		"auth:login_nonce:nonce",
		"auth:signup_intent:nonce",
		"auth:signup_attribution:nonce",
		"auth:support_intent:nonce",
		"auth:support_handoff:sha",
		"authnChallenge:id:",
		"authnChallengeSwitched:id:",
		"bindingRefresh:id:",
		"chat_session_revoked:jti:",
		"cliauth:code:code",
		"code-mode:execution:sha",
		"connectionAttemptCatalogExpired:attempt",
		"connectionAttemptEnded:attempt",
		"connectionAttemptIneligible:attempt:client:code",
		"connectionAttemptMembers:attempt",
		"connectionRecovery:id:",
		"consentToolInventory:state:attempt:",
		"device_agent_versions:day:org",
		"enforce:reply:replica",
		"federatedRefreshClientPause:client:hash",
		"hook:blocked-prompt:sha",
		"hook:idempotency:token",
		"hook:pending:sid",
		"hook:pending:v2:project:sid",
		"hooks:cowork-tool-ask:v1:project:call",
		"identity-map:replace-lock",
		"identityChainingLock:sha",
		"identityChainingAttempt:sha",
		"identityChainingFailure:sha",
		"identityChainingConsentAccepted:sha",
		"litellm:call:project:sha",
		"lock:userSessionRefreshReplay:issuer:hash",
		"mcp-attr:promote-claim:project:id",
		"mcp-attr:tuple:org:request",
		"mcpConnectionCatalog:v1:org:id",
		"openrouter-credits-alert:org:type:threshold",
		"openrouter-credits-alert-generation:org:type",
		"openrouter-credits-alert-recipient:key",
		"platform-mcp-workspace:user",
		"platformMCPChallenge:id:",
		"polar_period_usage_state:org:",
		"rate:pk:3:ns",
		"rate:ratelimit:name:key",
		"remoteLogin:id:",
		"remoteLoginHop:id:",
		"remotesession:refresh:subject:client",
		"remotesession:refresh-cooldown:session:1",
		"remotesession:rotate_registration:client",
		"risk:policy-ack:id",
		"risk:policy-bypass-request:id",
		"session:agent-variant:v2:project:sid",
		"session:mcp-list:v2:project:sid",
		"session:metadata:sid",
		"session:quarantine:org:project:sid",
		"session_transfer:sha",
		"sessionToolWitness:grant:hash:",
		"sessions:issued_at:sid",
		"sessions:v2:sid:",
		"sessions:valid_after:uid",
		"slack-directory-oauth:sha",
		"spend_gate:rules:org",
		"spend_gate:actor:org:user",
		"tenant-dimensions:replace-lock",
		"tunnel_routes:tunnel",
		"tunnel_connections:tunnel",
		"tunnelsess:live:tunnel",
		"tunnelsess:map:tunnel:server:sid",
		"userSessionRefreshReplay:issuer:hash:",
		"client_assertion_jti:b64",
		"idjag_jti:b64",
		"workload_assertion_jti:b64",
		"gram:redis-switch:state:default",
	}
	for _, key := range sessions {
		t.Run("sessions/"+key, func(t *testing.T) {
			t.Parallel()
			role, _ := redisrole.Of(key)
			require.Equal(t, redisrole.RoleSessions, role)
		})
	}
}

// TestScriptedPairsShareARole pins both families of each multi-key script or
// command to one role, since keys built inside Lua are not visible to the
// guard.
func TestScriptedPairsShareARole(t *testing.T) {
	t.Parallel()

	pairs := [][2]string{
		{"mcpClientInfo:{p:t}:sha", "mcpClientInfoLive:{p:t}"},
		{"tunnelsess:live:tunnel", "tunnelsess:map:tunnel:server:sid"},
		{"authnChallenge:id:", "connectionAttemptEnded:attempt"},
		{"connectionRecovery:id:", "connectionAttemptEnded:attempt"},
		{"sessions:issued_at:sid", "sessions:valid_after:uid"},
		{"userInfo:user", "feature:org:feature"},
	}
	for _, pair := range pairs {
		t.Run(pair[0]+" with "+pair[1], func(t *testing.T) {
			t.Parallel()
			first, _ := redisrole.Of(pair[0])
			second, _ := redisrole.Of(pair[1])
			require.Equal(t, first, second)
		})
	}
}
