package enrich

import (
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

const (
	OrganizationIDKey                   = attribute.Key("speakeasy.organization.id")
	OrganizationSlugKey                 = attribute.Key("speakeasy.organization.slug")
	ProjectIDKey                        = attribute.Key("speakeasy.project.id")
	ProjectSlugKey                      = attribute.Key("speakeasy.project.slug")
	APIKeyIDKey                         = attribute.Key("speakeasy.api_key.id")
	APIKeyNameKey                       = attribute.Key("speakeasy.api_key.name")
	TokensCountKey                      = attribute.Key("speakeasy.tokens.count")
	TokensCodecKey                      = attribute.Key("speakeasy.tokens.codec")
	OriginalInstrumentationScopeNameKey = attribute.Key("speakeasy.original_instrumentation_scope.name")
	DirectoryIDKey                      = attribute.Key("directory.id")
	DirectoryAttributeKey               = attribute.Key("directory.attribute")
	DirectoryGroupIDsKey                = attribute.Key("directory.group.ids")
	DirectoryGroupNamesKey              = attribute.Key("directory.group.names")
	GramUserRolesKey                    = attribute.Key("speakeasy.user.roles")
)

func OrganizationID(v string) attribute.KeyValue { return OrganizationIDKey.String(v) }

func OrganizationSlug(v string) attribute.KeyValue { return OrganizationSlugKey.String(v) }

func ProjectID(v string) attribute.KeyValue { return ProjectIDKey.String(v) }

func ProjectSlug(v string) attribute.KeyValue { return ProjectSlugKey.String(v) }

func APIKeyID(v string) attribute.KeyValue { return APIKeyIDKey.String(v) }

func APIKeyName(v string) attribute.KeyValue { return APIKeyNameKey.String(v) }

func TokensCount(v int) attribute.KeyValue { return TokensCountKey.Int(v) }

func TokensCodec(v string) attribute.KeyValue { return TokensCodecKey.String(v) }

func OriginalInstrumentationScopeName(v string) attribute.KeyValue {
	return OriginalInstrumentationScopeNameKey.String(v)
}

func DirectoryID(v string) attribute.KeyValue { return DirectoryIDKey.String(v) }

func DirectoryAttribute(key string) attribute.Key {
	return attribute.Key(string(DirectoryAttributeKey) + "." + key)
}

func DirectoryGroupIDs(v []string) attribute.KeyValue {
	return DirectoryGroupIDsKey.StringSlice(v)
}

func DirectoryGroupNames(v []string) attribute.KeyValue {
	return DirectoryGroupNamesKey.StringSlice(v)
}

func GramUserRoles(v []string) attribute.KeyValue { return GramUserRolesKey.StringSlice(v) }

const agentKeyPrefix = "speakeasy.agent."

// AgentKey is one agent attribute on a normalized record:
// speakeasy.agent.<name>. Only the transform writes these keys; the
// agent_events writer copies each into the column of the same name without
// asking a dialect.
func AgentKey(name string) attribute.Key {
	return attribute.Key(agentKeyPrefix + name)
}

const (
	// What the record is.
	AgentEventTypeKey    = attribute.Key(agentKeyPrefix + "event_type")
	AgentRawEventNameKey = attribute.Key(agentKeyPrefix + "raw_event_name")
	AgentSourceKey       = attribute.Key(agentKeyPrefix + "source")
	AgentProviderKey     = attribute.Key(agentKeyPrefix + "provider")
	AgentSurfaceKey      = attribute.Key(agentKeyPrefix + "surface")

	// Who and where.
	AgentSessionIDKey      = attribute.Key(agentKeyPrefix + "session_id")
	AgentTurnIDKey         = attribute.Key(agentKeyPrefix + "turn_id")
	AgentEventIDKey        = attribute.Key(agentKeyPrefix + "event_id")
	AgentUserEmailKey      = attribute.Key(agentKeyPrefix + "user_email")
	AgentExternalUserIDKey = attribute.Key(agentKeyPrefix + "external_user_id")
	AgentExternalOrgIDKey  = attribute.Key(agentKeyPrefix + "external_org_id")

	// What happened.
	AgentModelKey          = attribute.Key(agentKeyPrefix + "model")
	AgentQuerySourceKey    = attribute.Key(agentKeyPrefix + "query_source")
	AgentSkillNameKey      = attribute.Key(agentKeyPrefix + "skill_name")
	AgentAgentNameKey      = attribute.Key(agentKeyPrefix + "agent_name")
	AgentMCPServerNameKey  = attribute.Key(agentKeyPrefix + "mcp_server_name")
	AgentMCPToolNameKey    = attribute.Key(agentKeyPrefix + "mcp_tool_name")
	AgentNameKey           = attribute.Key(agentKeyPrefix + "name")
	AgentToolNameKey       = attribute.Key(agentKeyPrefix + "tool_name")
	AgentTextKey           = attribute.Key(agentKeyPrefix + "text")
	AgentOutcomeKey        = attribute.Key(agentKeyPrefix + "outcome")
	AgentOutcomeMessageKey = attribute.Key(agentKeyPrefix + "outcome_message")
	AgentDurationNanoKey   = attribute.Key(agentKeyPrefix + "duration_nano")

	// Usage, carried by api_request only.
	AgentInputTokensKey      = attribute.Key(agentKeyPrefix + "input_tokens")
	AgentOutputTokensKey     = attribute.Key(agentKeyPrefix + "output_tokens")
	AgentCacheReadTokensKey  = attribute.Key(agentKeyPrefix + "cache_read_tokens")
	AgentCacheWriteTokensKey = attribute.Key(agentKeyPrefix + "cache_write_tokens")
	AgentCostUSDKey          = attribute.Key(agentKeyPrefix + "cost_usd")
)

// IsAgentKey reports whether a key is in the reserved speakeasy.agent
// namespace, which only the transform writes.
func IsAgentKey(key string) bool {
	return strings.HasPrefix(key, agentKeyPrefix)
}

const (
	pipelineKeyPrefix  = "speakeasy."
	directoryKeyPrefix = "directory."
)

// IsPipelineKey reports whether a key is in a namespace the pipeline owns,
// speakeasy or directory. The transform drops what a producer sends there
// before it writes its own, so a producer cannot classify its own record,
// claim another tenant, pose as another scope or give a person a group.
func IsPipelineKey(key string) bool {
	return strings.HasPrefix(key, pipelineKeyPrefix) || strings.HasPrefix(key, directoryKeyPrefix)
}
