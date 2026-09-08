package otel

import "go.opentelemetry.io/otel/attribute"

type Key = attribute.Key

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
