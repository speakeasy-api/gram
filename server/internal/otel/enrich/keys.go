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

// eventColumnKeyPrefix is the namespace of the canonical agent_events
// columns on a normalized record: one key per column, named after it.
const eventColumnKeyPrefix = "speakeasy.event."

// EventColumnKey is the attribute key that carries one agent_events column
// on a normalized record. The column enrichers in the transform write these
// keys next to the producer's original attributes, which stay as they were,
// and the agent_events writer copies them into the row without asking a
// dialect. The set below is the contract between the transform and every
// consumer on the normalized topics: a column that is not listed is filled
// by the pipeline some other way (tenancy, timing, the directory enricher)
// or not at all.
func EventColumnKey(column string) attribute.Key {
	return attribute.Key(eventColumnKeyPrefix + column)
}

const (
	// What the record is.
	EventTypeColumnKey    = attribute.Key(eventColumnKeyPrefix + "event_type")
	RawEventNameColumnKey = attribute.Key(eventColumnKeyPrefix + "raw_event_name")
	SourceColumnKey       = attribute.Key(eventColumnKeyPrefix + "source")
	ProviderColumnKey     = attribute.Key(eventColumnKeyPrefix + "provider")
	SurfaceColumnKey      = attribute.Key(eventColumnKeyPrefix + "surface")

	// Who and where.
	SessionIDColumnKey      = attribute.Key(eventColumnKeyPrefix + "session_id")
	TurnIDColumnKey         = attribute.Key(eventColumnKeyPrefix + "turn_id")
	EventIDColumnKey        = attribute.Key(eventColumnKeyPrefix + "event_id")
	UserEmailColumnKey      = attribute.Key(eventColumnKeyPrefix + "user_email")
	ExternalUserIDColumnKey = attribute.Key(eventColumnKeyPrefix + "external_user_id")
	ExternalOrgIDColumnKey  = attribute.Key(eventColumnKeyPrefix + "external_org_id")

	// What happened.
	ModelColumnKey          = attribute.Key(eventColumnKeyPrefix + "model")
	QuerySourceColumnKey    = attribute.Key(eventColumnKeyPrefix + "query_source")
	SkillNameColumnKey      = attribute.Key(eventColumnKeyPrefix + "skill_name")
	AgentNameColumnKey      = attribute.Key(eventColumnKeyPrefix + "agent_name")
	MCPServerNameColumnKey  = attribute.Key(eventColumnKeyPrefix + "mcp_server_name")
	MCPToolNameColumnKey    = attribute.Key(eventColumnKeyPrefix + "mcp_tool_name")
	NameColumnKey           = attribute.Key(eventColumnKeyPrefix + "name")
	ToolNameColumnKey       = attribute.Key(eventColumnKeyPrefix + "tool_name")
	TextColumnKey           = attribute.Key(eventColumnKeyPrefix + "text")
	OutcomeColumnKey        = attribute.Key(eventColumnKeyPrefix + "outcome")
	OutcomeMessageColumnKey = attribute.Key(eventColumnKeyPrefix + "outcome_message")
	DurationNanoColumnKey   = attribute.Key(eventColumnKeyPrefix + "duration_nano")

	// Usage, carried by api_request only.
	InputTokensColumnKey      = attribute.Key(eventColumnKeyPrefix + "input_tokens")
	OutputTokensColumnKey     = attribute.Key(eventColumnKeyPrefix + "output_tokens")
	CacheReadTokensColumnKey  = attribute.Key(eventColumnKeyPrefix + "cache_read_tokens")
	CacheWriteTokensColumnKey = attribute.Key(eventColumnKeyPrefix + "cache_write_tokens")
	CostUSDColumnKey          = attribute.Key(eventColumnKeyPrefix + "cost_usd")
)

// IsEventColumnKey reports whether an attribute key is in the reserved
// speakeasy.event namespace. Only the column enrichers write there: a
// producer that sends such a key is trying to classify its own record, and
// the transform drops it before the enrichers write theirs.
func IsEventColumnKey(key string) bool {
	return strings.HasPrefix(key, eventColumnKeyPrefix)
}
