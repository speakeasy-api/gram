// Package redisrole classifies Redis keys into the three roles the server
// stores them in.
//
//   - The auth role holds state whose loss logs users out, breaks an in-flight
//     sign-in or OAuth flow, or weakens a security control. Its instance never
//     evicts.
//   - The ops role holds operational state: queues, routes, hook buffers,
//     dedupe markers, locks, gates and non-auth rate limits. Its instance only
//     evicts keys that carry an expiry, and only under memory pressure.
//   - The evictable role holds families that rebuild on a miss. Its instance
//     evicts any key under memory pressure.
//
// The manifest below is the single list of key families and their roles. A key
// that matches no family is unclassified: tests and local development refuse
// it, and production stores it with the ops role and counts it, so a family
// someone forgot to declare can never land on the auth instance by default.
package redisrole

import (
	"slices"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Role names the Redis instance a key family is stored on.
type Role string

const (
	// RoleAuth is the instance for authentication and session state.
	RoleAuth Role = "auth"

	// RoleOps is the instance for operational state. It is also where an
	// unclassified key is stored in production.
	RoleOps Role = "ops"

	// RoleEvictable is the instance for families that rebuild on a miss.
	RoleEvictable Role = "evictable"
)

// Roles lists every role.
func Roles() []Role {
	return []Role{RoleAuth, RoleOps, RoleEvictable}
}

// UnclassifiedPrefix is the metric and log label for a key that matched no
// family in the manifest.
const UnclassifiedPrefix = "unclassified"

// unclassifiedRole is the role an unclassified key is stored with in
// production. Never the auth role: its instance does not evict, so an
// undeclared family growing without bound would fill it.
const unclassifiedRole = RoleOps

// Family is one key family of the manifest.
type Family struct {
	// Prefix is the start of every key of the family. It ends in ":" so that
	// one family cannot capture another whose name extends it
	// (clientCredentials: and clientCredentialsTokenCache:, userInfo: and
	// userInfoSnapshot:). A more specific family may extend a broader one
	// (rate:ratelimit:remote_session_validate: and rate:ratelimit:); the
	// longest matching prefix wins.
	Prefix string

	// Role is the instance the family is stored on.
	Role Role
}

// manifest lists every key family the server, the tunnel gateway and the
// enforcement workers write. Versions are left out of prefixes on purpose so
// a version bump in a key does not make the family unclassified.
//
// Families written together by one Lua script, MULTI or multi-key command
// share a role (sessions:issued_at: and sessions:valid_after:; authnChallenge:,
// connectionRecovery: and connectionAttemptEnded:; mcpClientInfo: and
// mcpClientInfoLive:; tunnelsess:live: and tunnelsess:map:).
var manifest = []Family{
	// Auth: dashboard sessions and revocations.
	{Prefix: "sessions:v2:", Role: RoleAuth},
	{Prefix: "sessions:issued_at:", Role: RoleAuth},
	{Prefix: "sessions:valid_after:", Role: RoleAuth},
	{Prefix: "session_transfer:", Role: RoleAuth},
	{Prefix: "chat_session_revoked:", Role: RoleAuth},

	// Auth: login and staff authentication.
	{Prefix: "auth:login_nonce:", Role: RoleAuth},
	{Prefix: "auth:signup_intent:", Role: RoleAuth},
	{Prefix: "auth:signup_attribution:", Role: RoleAuth},
	{Prefix: "auth:support_intent:", Role: RoleAuth},
	{Prefix: "auth:support_handoff:", Role: RoleAuth},
	{Prefix: "adminSession:", Role: RoleAuth},
	{Prefix: "adminLoginState:", Role: RoleAuth},
	{Prefix: "adminMCPChallenge:", Role: RoleAuth},
	{Prefix: "assetsPlatformImageUpload:", Role: RoleAuth},
	{Prefix: "cliauth:code:", Role: RoleAuth},
	{Prefix: "platformMCPChallenge:", Role: RoleAuth},
	{Prefix: "platform-mcp-workspace:", Role: RoleAuth},
	{Prefix: "slack-directory-oauth:", Role: RoleAuth},
	{Prefix: "agentInstall:", Role: RoleAuth},
	{Prefix: "assistant-runtime-credential:", Role: RoleAuth},

	// Auth: MCP OAuth, consent and replay protection.
	{Prefix: "authnChallenge:", Role: RoleAuth},
	{Prefix: "authnChallengeSwitched:", Role: RoleAuth},
	{Prefix: "connectionRecovery:", Role: RoleAuth},
	{Prefix: "connectionAttemptEnded:", Role: RoleAuth},
	{Prefix: "consentToolInventory:", Role: RoleAuth},
	{Prefix: "mcpConnectionCatalog:", Role: RoleAuth},
	{Prefix: "agentUserSessionGrant:", Role: RoleAuth},
	{Prefix: "gatewayCatalogGrant:", Role: RoleAuth},
	{Prefix: "gatewayUserSessionGrant:", Role: RoleAuth},
	{Prefix: "userSessionGrant:", Role: RoleAuth},
	{Prefix: "userSessionRefreshReplay:", Role: RoleAuth},
	{Prefix: "lock:userSessionRefreshReplay:", Role: RoleAuth},
	{Prefix: "client_assertion_jti:", Role: RoleAuth},
	{Prefix: "idjag_jti:", Role: RoleAuth},
	{Prefix: "workload_assertion_jti:", Role: RoleAuth},

	// Auth: upstream OAuth and the helpers synchronous auth paths call. The
	// client-credential token, failure marker and mint lease share one owner.
	{Prefix: "remoteLogin:", Role: RoleAuth},
	{Prefix: "remoteLoginHop:", Role: RoleAuth},
	{Prefix: "bindingRefresh:", Role: RoleAuth},
	{Prefix: "remotesession:refresh:", Role: RoleAuth},
	{Prefix: "remotesession:refresh-cooldown:", Role: RoleAuth},
	{Prefix: "remotesession:refresh-outage:", Role: RoleAuth},
	{Prefix: "remotesession:refresh-address:", Role: RoleAuth},
	{Prefix: "remotesession:rotate_registration:", Role: RoleAuth},
	{Prefix: "federatedRefreshClientPause:", Role: RoleAuth},
	{Prefix: "identityChainingLock:", Role: RoleAuth},
	{Prefix: "identityChainingAttempt:", Role: RoleAuth},
	{Prefix: "identityChainingFailure:", Role: RoleAuth},
	{Prefix: "identityChainingConsentAccepted:", Role: RoleAuth},
	{Prefix: "clientCredentials:", Role: RoleAuth},
	{Prefix: "clientCredentialsFailure:", Role: RoleAuth},
	{Prefix: "clientCredentialsLease:", Role: RoleAuth},
	{Prefix: "emaEndpointResource:", Role: RoleAuth},
	{Prefix: "federated-oidc-metadata:", Role: RoleAuth},
	{Prefix: "userInfo:", Role: RoleAuth},
	{Prefix: "sessionPolicy:", Role: RoleAuth},

	// Auth: the tunnel gateway's copy of the live routes of tunnels that carry
	// OAuth back-channel calls, so those calls do not depend on the ops
	// instance. The bulk routes stay on ops.
	{Prefix: "auth_tunnel_routes:", Role: RoleAuth},

	// Auth: rate limits charged on synchronous authentication paths. Most
	// fail closed on a store error, so they must not depend on the ops
	// instance.
	{Prefix: "rate:ratelimit:workload_issuer_lookup:", Role: RoleAuth},
	{Prefix: "rate:ratelimit:workload_device_key_lookup:", Role: RoleAuth},
	{Prefix: "rate:ratelimit:workload_assertion_jwks_refresh:", Role: RoleAuth},
	{Prefix: "rate:ratelimit:workload_assertion_jwks_fetch:", Role: RoleAuth},
	{Prefix: "rate:ratelimit:client_assertion_jwks_refresh:", Role: RoleAuth},
	{Prefix: "rate:ratelimit:client_assertion_jwks_fetch:", Role: RoleAuth},
	{Prefix: "rate:ratelimit:idjag_jwks_refresh:", Role: RoleAuth},
	{Prefix: "rate:ratelimit:idjag_jwks_fetch:", Role: RoleAuth},
	{Prefix: "rate:ratelimit:remote_session_id_token_jwks_refresh:", Role: RoleAuth},
	{Prefix: "rate:ratelimit:remote_session_id_token_jwks_fetch:", Role: RoleAuth},
	{Prefix: "rate:ratelimit:remote_session_validate:", Role: RoleAuth},
	{Prefix: "rate:ratelimit:remote_session_enrichment:", Role: RoleAuth},

	// Ops: live routing and enforcement.
	{Prefix: "enforce:reply:", Role: RoleOps},
	{Prefix: "tunnel_routes:", Role: RoleOps},
	{Prefix: "tunnel_connections:", Role: RoleOps},
	{Prefix: "tunnelsess:live:", Role: RoleOps},
	{Prefix: "tunnelsess:map:", Role: RoleOps},

	// Ops: hooks, evidence and attribution.
	{Prefix: "hook:pending:", Role: RoleOps},
	{Prefix: "hook:blocked-prompt:", Role: RoleOps},
	{Prefix: "hook:idempotency:", Role: RoleOps},
	{Prefix: "hooks:cowork-tool-ask:", Role: RoleOps},
	{Prefix: "ahp-upload:", Role: RoleOps},
	{Prefix: "session:metadata:", Role: RoleOps},
	{Prefix: "session:agent-variant:", Role: RoleOps},
	{Prefix: "session:mcp-list:", Role: RoleOps},
	{Prefix: "session:mcp-list-owner:", Role: RoleOps},
	{Prefix: "session:mcp-list-read:", Role: RoleOps},
	{Prefix: "session:native-prompt:", Role: RoleOps},
	{Prefix: "device_agent_versions:", Role: RoleOps},
	{Prefix: "mcp-attr:tuple:", Role: RoleOps},
	{Prefix: "mcp-attr:promote-claim:", Role: RoleOps},
	{Prefix: "litellm:call:", Role: RoleOps},

	// Ops: runtime controls that fail open or narrow, locks and telemetry
	// dedupe.
	{Prefix: "session:quarantine:", Role: RoleOps},
	{Prefix: "spend_gate:rules:", Role: RoleOps},
	{Prefix: "spend_gate:actor:", Role: RoleOps},
	{Prefix: "polar_period_usage_state:", Role: RoleOps},
	{Prefix: "sessionToolWitness:", Role: RoleOps},
	{Prefix: "code-mode:execution:", Role: RoleOps},
	{Prefix: "risk:policy-ack:", Role: RoleOps},
	{Prefix: "risk:policy-bypass-request:", Role: RoleOps},
	{Prefix: "identity-map:", Role: RoleOps},
	{Prefix: "tenant-dimensions:", Role: RoleOps},
	{Prefix: "openrouter-credits-alert:", Role: RoleOps},
	{Prefix: "openrouter-credits-alert-generation:", Role: RoleOps},
	{Prefix: "openrouter-credits-alert-recipient:", Role: RoleOps},
	{Prefix: "connectionAttemptCatalogExpired:", Role: RoleOps},
	{Prefix: "connectionAttemptIneligible:", Role: RoleOps},
	{Prefix: "connectionAttemptMembers:", Role: RoleOps},

	// Ops: guardian buckets and every rate limit not declared on auth above.
	{Prefix: "rate:pk:", Role: RoleOps},
	{Prefix: "rate:ratelimit:", Role: RoleOps},

	// Evictable: families that rebuild on a miss.
	{Prefix: "chat:turnframes:", Role: RoleEvictable},
	{Prefix: "mcpClientInfo:", Role: RoleEvictable},
	{Prefix: "mcpClientInfoLive:", Role: RoleEvictable},
	{Prefix: "admin_hook_clients:", Role: RoleEvictable},
	{Prefix: "clientCredentialsTokenCache:", Role: RoleEvictable},
	{Prefix: "feature:", Role: RoleEvictable},
	{Prefix: "mcpservers:tool_disposition:", Role: RoleEvictable},
	{Prefix: "openrouter:model:", Role: RoleEvictable},
	{Prefix: "otelUserEnrichment:", Role: RoleEvictable},
	{Prefix: "plugins:has-collaborator:", Role: RoleEvictable},
	{Prefix: "plugins:live-version:", Role: RoleEvictable},
	{Prefix: "plugins:held-entries:", Role: RoleEvictable},
	{Prefix: "polar_customer_state:", Role: RoleEvictable},
	{Prefix: "polar_product:", Role: RoleEvictable},
	{Prefix: "registry:list:", Role: RoleEvictable},
	{Prefix: "registry:details:", Role: RoleEvictable},
	{Prefix: "shadow_mcp_policy_enabled:", Role: RoleEvictable},
	{Prefix: "slack-bot-identity:", Role: RoleEvictable},
	{Prefix: "toolset:", Role: RoleEvictable},
	{Prefix: "userInfoSnapshot:", Role: RoleEvictable},
}

// Manifest returns a copy of every declared key family.
func Manifest() []Family {
	return slices.Clone(manifest)
}

// Prefixes returns the declared prefixes of role.
func Prefixes(role Role) []string {
	var prefixes []string
	for _, family := range manifest {
		if family.Role == role {
			prefixes = append(prefixes, family.Prefix)
		}
	}
	return prefixes
}

// Classification is the outcome of looking a key up in the manifest.
type Classification struct {
	// Role is the instance the key is stored on. An unclassified key reports
	// the role it is stored with in production.
	Role Role

	// Prefix is the matched family's prefix, or UnclassifiedPrefix.
	Prefix string

	// Classified reports whether a family matched.
	Classified bool
}

// Classify looks key up in the manifest. The longest matching prefix wins.
func Classify(key string) Classification {
	best := -1
	for i, family := range manifest {
		if !strings.HasPrefix(key, family.Prefix) {
			continue
		}
		if best < 0 || len(family.Prefix) > len(manifest[best].Prefix) {
			best = i
		}
	}
	if best < 0 {
		return Classification{Role: unclassifiedRole, Prefix: UnclassifiedPrefix, Classified: false}
	}
	return Classification{Role: manifest[best].Role, Prefix: manifest[best].Prefix, Classified: true}
}

// Of reports the role of key and the declared prefix it matched. An
// unclassified key is RoleOps with the prefix UnclassifiedPrefix.
func Of(key string) (Role, string) {
	c := Classify(key)
	return c.Role, c.Prefix
}

// EvictableClient is the Redis client for the evictable role. Its distinct
// type keeps a client of another role from being handed to code that stores
// evictable families. When no separate evictable instance is configured it
// wraps the client the other roles share.
type EvictableClient struct {
	*redis.Client
}
