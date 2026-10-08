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

const agentColumnKeyPrefix = "speakeasy.agent."

// AgentColumnKey is the attribute that carries one agent_events column on a
// normalized record: speakeasy.agent.<column>. Only the transform writes
// these keys; the writer copies them into the row without asking a dialect.
func AgentColumnKey(column string) attribute.Key {
	return attribute.Key(agentColumnKeyPrefix + column)
}

const (
	// What the record is.
	EventTypeColumnKey    = attribute.Key(agentColumnKeyPrefix + "event_type")
	RawEventNameColumnKey = attribute.Key(agentColumnKeyPrefix + "raw_event_name")
	SourceColumnKey       = attribute.Key(agentColumnKeyPrefix + "source")
	ProviderColumnKey     = attribute.Key(agentColumnKeyPrefix + "provider")
	SurfaceColumnKey      = attribute.Key(agentColumnKeyPrefix + "surface")

	// Who and where.
	SessionIDColumnKey      = attribute.Key(agentColumnKeyPrefix + "session_id")
	TurnIDColumnKey         = attribute.Key(agentColumnKeyPrefix + "turn_id")
	EventIDColumnKey        = attribute.Key(agentColumnKeyPrefix + "event_id")
	UserEmailColumnKey      = attribute.Key(agentColumnKeyPrefix + "user_email")
	ExternalUserIDColumnKey = attribute.Key(agentColumnKeyPrefix + "external_user_id")
	ExternalOrgIDColumnKey  = attribute.Key(agentColumnKeyPrefix + "external_org_id")

	// What happened.
	ModelColumnKey          = attribute.Key(agentColumnKeyPrefix + "model")
	QuerySourceColumnKey    = attribute.Key(agentColumnKeyPrefix + "query_source")
	SkillNameColumnKey      = attribute.Key(agentColumnKeyPrefix + "skill_name")
	AgentNameColumnKey      = attribute.Key(agentColumnKeyPrefix + "agent_name")
	MCPServerNameColumnKey  = attribute.Key(agentColumnKeyPrefix + "mcp_server_name")
	MCPToolNameColumnKey    = attribute.Key(agentColumnKeyPrefix + "mcp_tool_name")
	NameColumnKey           = attribute.Key(agentColumnKeyPrefix + "name")
	ToolNameColumnKey       = attribute.Key(agentColumnKeyPrefix + "tool_name")
	TextColumnKey           = attribute.Key(agentColumnKeyPrefix + "text")
	OutcomeColumnKey        = attribute.Key(agentColumnKeyPrefix + "outcome")
	OutcomeMessageColumnKey = attribute.Key(agentColumnKeyPrefix + "outcome_message")
	DurationNanoColumnKey   = attribute.Key(agentColumnKeyPrefix + "duration_nano")

	// Usage, carried by api_request only.
	InputTokensColumnKey      = attribute.Key(agentColumnKeyPrefix + "input_tokens")
	OutputTokensColumnKey     = attribute.Key(agentColumnKeyPrefix + "output_tokens")
	CacheReadTokensColumnKey  = attribute.Key(agentColumnKeyPrefix + "cache_read_tokens")
	CacheWriteTokensColumnKey = attribute.Key(agentColumnKeyPrefix + "cache_write_tokens")
	CostUSDColumnKey          = attribute.Key(agentColumnKeyPrefix + "cost_usd")
)

// IsAgentColumnKey reports whether a key is in the reserved speakeasy.agent
// namespace, which only the transform writes.
func IsAgentColumnKey(key string) bool {
	return strings.HasPrefix(key, agentColumnKeyPrefix)
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
