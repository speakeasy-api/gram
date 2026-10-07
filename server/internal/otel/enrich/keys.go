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

// agentColumnKeyPrefix is the namespace of the canonical agent_events
// columns on a normalized record: one key per column, named after it.
const agentColumnKeyPrefix = "speakeasy.agent."

// AgentColumnKey is the attribute key that carries one agent_events column
// on a normalized record. The speakeasy.agent namespace holds one record
// described in the agent vocabulary: what kind of event it is, whose session
// it belongs to, what model or tool it concerns, how it went and what it
// used. Only the column enrichers in the transform write these keys, next to
// the producer's original attributes, which stay as they were; a key a
// producer sends under this namespace is stripped before the enrichers run.
// The keys are forwarded to customer destinations like the other speakeasy.*
// keys, and the agent_events writer copies them into the row without asking
// a dialect. The set below is the contract between the transform and every
// consumer on the normalized topics: a column that is not listed is filled
// by the pipeline some other way (tenancy, timing, the directory enricher)
// or not at all.
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

// IsAgentColumnKey reports whether an attribute key is in the reserved
// speakeasy.agent namespace. Only the column enrichers write there: a
// producer that sends such a key is trying to classify its own record, and
// the transform drops it before the enrichers write theirs.
func IsAgentColumnKey(key string) bool {
	return strings.HasPrefix(key, agentColumnKeyPrefix)
}
