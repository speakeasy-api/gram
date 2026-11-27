package redisrole_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/redisrole"
)

func TestManifestPrefixesEndInColon(t *testing.T) {
	t.Parallel()

	for _, family := range redisrole.Manifest() {
		require.True(t, strings.HasSuffix(family.Prefix, ":"), "prefix %q must end in a colon", family.Prefix)
	}
}

func TestManifestPrefixesAreUnique(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, family := range redisrole.Manifest() {
		require.False(t, seen[family.Prefix], "prefix %q is declared twice", family.Prefix)
		seen[family.Prefix] = true
	}
}

func TestManifestRolesAreKnown(t *testing.T) {
	t.Parallel()

	for _, family := range redisrole.Manifest() {
		require.Contains(t, redisrole.Roles(), family.Role, family.Prefix)
	}
}

// TestManifestNestedPrefixesAreIntended lists every family whose prefix
// extends another's. Longest match wins, so a nested prefix silently moves
// part of its parent's keys; each one here is deliberate.
func TestManifestNestedPrefixesAreIntended(t *testing.T) {
	t.Parallel()

	var nested []string
	families := redisrole.Manifest()
	for _, inner := range families {
		for _, outer := range families {
			if inner.Prefix != outer.Prefix && strings.HasPrefix(inner.Prefix, outer.Prefix) {
				nested = append(nested, inner.Prefix+" in "+outer.Prefix)
			}
		}
	}
	require.ElementsMatch(t, []string{
		"rate:ratelimit:workload_issuer_lookup: in rate:ratelimit:",
		"rate:ratelimit:workload_device_key_lookup: in rate:ratelimit:",
		"rate:ratelimit:workload_assertion_jwks_refresh: in rate:ratelimit:",
		"rate:ratelimit:workload_assertion_jwks_fetch: in rate:ratelimit:",
		"rate:ratelimit:client_assertion_jwks_refresh: in rate:ratelimit:",
		"rate:ratelimit:client_assertion_jwks_fetch: in rate:ratelimit:",
		"rate:ratelimit:idjag_jwks_refresh: in rate:ratelimit:",
		"rate:ratelimit:idjag_jwks_fetch: in rate:ratelimit:",
		"rate:ratelimit:remote_session_id_token_jwks_refresh: in rate:ratelimit:",
		"rate:ratelimit:remote_session_id_token_jwks_fetch: in rate:ratelimit:",
		"rate:ratelimit:remote_session_validate: in rate:ratelimit:",
		"rate:ratelimit:remote_session_enrichment: in rate:ratelimit:",
	}, nested)
}

// TestClassifyKnownFamilies pins one sample key of every known key family to
// its role, so a new or widened prefix cannot capture another family
// unnoticed.
func TestClassifyKnownFamilies(t *testing.T) {
	t.Parallel()

	families := map[redisrole.Role][]string{
		redisrole.RoleAuth: {
			"sessions:v2:sid:",
			"sessions:issued_at:sid",
			"sessions:valid_after:uid",
			"session_transfer:sha",
			"chat_session_revoked:jti:",
			"auth:login_nonce:nonce",
			"auth:signup_intent:nonce",
			"auth:signup_attribution:nonce",
			"auth:support_intent:nonce",
			"auth:support_handoff:sha",
			"adminSession:sid:",
			"adminSession:refresh:sid",
			"adminLoginState:state:",
			"adminMCPChallenge:id:",
			"assetsPlatformImageUpload:sha",
			"cliauth:code:code",
			"platformMCPChallenge:id:",
			"platform-mcp-workspace:user",
			"slack-directory-oauth:sha",
			"agentInstall:code:",
			"assistant-runtime-credential:cred",
			"authnChallenge:id:",
			"authnChallengeSwitched:id:",
			"connectionRecovery:id:",
			"connectionAttemptEnded:attempt",
			"consentToolInventory:state:attempt:",
			"mcpConnectionCatalog:v1:org:id",
			"agentUserSessionGrant:issuer:code:",
			"gatewayCatalogGrant:issuer:code:",
			"gatewayUserSessionGrant:issuer:code:",
			"userSessionGrant:issuer:code:",
			"userSessionRefreshReplay:issuer:hash:",
			"lock:userSessionRefreshReplay:issuer:hash",
			"client_assertion_jti:b64",
			"idjag_jti:b64",
			"workload_assertion_jti:b64",
			"remoteLogin:id:",
			"remoteLoginHop:id:",
			"bindingRefresh:id:",
			"remotesession:refresh:subject:client",
			"remotesession:refresh-cooldown:session:1",
			"remotesession:refresh-outage:session:1",
			"remotesession:refresh-address:session:1",
			"remotesession:rotate_registration:client",
			"federatedRefreshClientPause:client:hash",
			"identityChainingLock:sha",
			"identityChainingAttempt:sha",
			"identityChainingFailure:sha",
			"identityChainingConsentAccepted:sha",
			"clientCredentials:sha:",
			"clientCredentialsFailure:sha:",
			"clientCredentialsLease:sha",
			"emaEndpointResource:v1:sha",
			"federated-oidc-metadata:v3:sha",
			"userInfo:user:",
			"sessionPolicy:v2:issuer:jti:",
			"rate:ratelimit:workload_issuer_lookup:key",
			"rate:ratelimit:workload_device_key_lookup:key",
			"rate:ratelimit:workload_assertion_jwks_refresh:key",
			"rate:ratelimit:workload_assertion_jwks_fetch:key",
			"rate:ratelimit:client_assertion_jwks_refresh:key",
			"rate:ratelimit:client_assertion_jwks_fetch:key",
			"rate:ratelimit:idjag_jwks_refresh:key",
			"rate:ratelimit:idjag_jwks_fetch:key",
			"rate:ratelimit:remote_session_id_token_jwks_refresh:key",
			"rate:ratelimit:remote_session_id_token_jwks_fetch:key",
			"rate:ratelimit:remote_session_validate:key",
			"rate:ratelimit:remote_session_enrichment:key",
		},
		redisrole.RoleOps: {
			"enforce:reply:replica",
			"tunnel_routes:tunnel",
			"tunnel_connections:tunnel",
			"tunnelsess:live:tunnel",
			"tunnelsess:map:tunnel:server:sid",
			"hook:pending:sid",
			"hook:pending:v2:project:sid",
			"hook:blocked-prompt:sha",
			"hook:idempotency:token",
			"hooks:cowork-tool-ask:v1:project:call",
			"ahp-upload:project:id",
			"session:metadata:sid",
			"session:agent-variant:v2:project:sid",
			"session:mcp-list:v2:project:sid",
			"session:mcp-list-owner:v2:project:sid",
			"session:mcp-list-read:v2:project:sid",
			"session:native-prompt:v1:project:sid",
			"device_agent_versions:day:org",
			"device_agent_versions:day:orgs",
			"mcp-attr:tuple:org:request",
			"mcp-attr:promote-claim:project:id",
			"litellm:call:project:sha",
			"session:quarantine:org:project:sid",
			"spend_gate:rules:org",
			"spend_gate:actor:org:user",
			"polar_period_usage_state:org:",
			"sessionToolWitness:grant:hash:",
			"code-mode:execution:sha",
			"risk:policy-ack:id",
			"risk:policy-bypass-request:id",
			"identity-map:replace-lock",
			"tenant-dimensions:replace-lock",
			"openrouter-credits-alert:org:type:threshold",
			"openrouter-credits-alert-generation:org:type",
			"openrouter-credits-alert-recipient:key",
			"connectionAttemptCatalogExpired:attempt",
			"connectionAttemptIneligible:attempt:client:code",
			"connectionAttemptMembers:attempt",
			"rate:pk:3:ns",
			"rate:ratelimit:remote_session_recheck_host:key",
			"rate:ratelimit:tunnel-public-request:key",
			"rate:ratelimit:ahp-upload:key",
		},
		redisrole.RoleEvictable: {
			"chat:turnframes:chat-id",
			"mcpClientInfo:{project:toolset}:sha",
			"mcpClientInfoLive:{project:toolset}",
			"admin_hook_clients:30:",
			"clientCredentialsTokenCache:projectID-p-scopes-s:",
			"feature:org:feature:",
			"feature:v2:org:feature:",
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
			"shadow_mcp_policy_enabled:project:",
			"slack-bot-identity:hash:",
			"toolset:a:b:1:",
			"userInfoSnapshot:org:user:",
		},
	}
	for role, keys := range families {
		for _, key := range keys {
			t.Run(string(role)+"/"+key, func(t *testing.T) {
				t.Parallel()
				got := redisrole.Classify(key)
				require.True(t, got.Classified)
				require.False(t, got.Control)
				require.Equal(t, role, got.Role)
				require.True(t, strings.HasPrefix(key, got.Prefix), "matched %q", got.Prefix)
			})
		}
	}
}

func TestClassifyUnclassifiedKeyIsOps(t *testing.T) {
	t.Parallel()

	got := redisrole.Classify("undeclared-family:x")
	require.False(t, got.Classified)
	require.Equal(t, redisrole.RoleOps, got.Role, "an unclassified key is never stored on the auth role")
	require.Equal(t, redisrole.UnclassifiedPrefix, got.Prefix)
}

func TestClassifySwitchKeysAreControl(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"gram:redis-switch:state:auth", "gram:redis-switch:hold"} {
		got := redisrole.Classify(key)
		require.True(t, got.Control, key)
	}
}

// TestScriptedGroupsShareARole pins the families of each multi-key script,
// transaction or command to one role, since keys built inside Lua are not
// visible to the guard.
func TestScriptedGroupsShareARole(t *testing.T) {
	t.Parallel()

	groups := [][]string{
		{"sessions:issued_at:sid", "sessions:valid_after:uid"},
		{"authnChallenge:id:", "connectionRecovery:id:", "connectionAttemptEnded:attempt"},
		{"mcpClientInfo:{p:t}:sha", "mcpClientInfoLive:{p:t}"},
		{"tunnelsess:live:tunnel", "tunnelsess:map:tunnel:server:sid"},
		{"clientCredentials:sha:", "clientCredentialsFailure:sha:", "clientCredentialsLease:sha"},
		{"device_agent_versions:day:org", "device_agent_versions:day:orgs"},
	}
	for _, group := range groups {
		t.Run(strings.Join(group, " with "), func(t *testing.T) {
			t.Parallel()
			first, _ := redisrole.Of(group[0])
			for _, key := range group[1:] {
				role, _ := redisrole.Of(key)
				require.Equal(t, first, role, key)
			}
		})
	}
}
