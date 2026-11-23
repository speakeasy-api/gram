// Package redisrole classifies Redis keys into the two roles the server
// stores them in.
//
// The sessions role holds state whose loss logs users out, breaks in-flight
// flows or weakens a security control. The evictable role holds families that
// rebuild on a miss and may live on an instance that evicts under memory
// pressure (allkeys-lru). A key belongs to the evictable role only when it
// starts with one of the declared prefixes; every other key, including any
// family added later and not declared here, belongs to the sessions role.
package redisrole

import (
	"slices"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Role names the Redis instance a key family is stored on.
type Role string

const (
	// RoleSessions is the instance for state that must survive memory
	// pressure. It is the default for every key not declared evictable.
	RoleSessions Role = "sessions"

	// RoleEvictable is the instance for families that rebuild on a miss.
	RoleEvictable Role = "evictable"
)

// unclassifiedPrefix is the metric and log label for a key that matched no
// declared evictable prefix.
const unclassifiedPrefix = "unclassified"

// evictablePrefixes lists every key family stored on the evictable instance.
// Each entry ends in ":" so that one family cannot capture another whose name
// extends it (clientCredentials: and clientCredentialsFailure:, userInfo: and
// userInfoSnapshot:). Versions are left out on purpose so a version bump in a
// key does not move the family back to the sessions instance.
var evictablePrefixes = []string{
	"admin_hook_clients:",
	"chat:turnframes:",
	"clientCredentials:",
	"clientCredentialsFailure:",
	"clientCredentialsLease:",
	"clientCredentialsTokenCache:",
	"emaEndpointResource:",
	"federated-oidc-metadata:",
	"feature:",
	"mcpClientInfo:",
	"mcpClientInfoLive:",
	"mcpservers:tool_disposition:",
	"openrouter:model:",
	"otelUserEnrichment:",
	"plugins:has-collaborator:",
	"plugins:held-entries:",
	"plugins:live-version:",
	"polar_customer_state:",
	"polar_product:",
	"registry:details:",
	"registry:list:",
	"sessionPolicy:",
	"shadow_mcp_policy_enabled:",
	"slack-bot-identity:",
	"toolset:",
	"userInfo:",
	"userInfoSnapshot:",
}

// EvictablePrefixes returns a copy of the declared evictable key prefixes.
func EvictablePrefixes() []string {
	return slices.Clone(evictablePrefixes)
}

// Of reports the role of key and the declared prefix it matched. A key that
// matches no evictable prefix is RoleSessions with the prefix
// "unclassified".
func Of(key string) (Role, string) {
	for _, prefix := range evictablePrefixes {
		if strings.HasPrefix(key, prefix) {
			return RoleEvictable, prefix
		}
	}
	return RoleSessions, unclassifiedPrefix
}

// EvictableClient is the Redis client for the evictable role. Its distinct
// type keeps a sessions client from being handed to code that stores
// evictable families. When no separate evictable instance is configured it
// wraps the sessions client itself.
type EvictableClient struct {
	*redis.Client
}
