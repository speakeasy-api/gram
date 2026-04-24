package attr

import (
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
)

const (
	WideEventKey = attribute.Key("gram.wide_event")

	RequestPanicKey      = attribute.Key("gram.request.panic")
	RequestPanicStackKey = attribute.Key("gram.request.panic_stack")

	RequestAuthAccountTypeKey            = attribute.Key("gram.request.auth_account_type")
	RequestAuthAPIKeyIDKey               = attribute.Key("gram.request.auth_api_key_id")
	RequestAuthOrganizationIDKey         = attribute.Key("gram.request.auth_organization_id")
	RequestAuthOrganizationSlugKey       = attribute.Key("gram.request.auth_organization_slug")
	RequestAuthProjectIDKey              = attribute.Key("gram.request.auth_project_id")
	RequestAuthProjectSlugKey            = attribute.Key("gram.request.auth_project_slug")
	RequestAuthSchemeSessionKey          = attribute.Key("gram.request.auth_scheme_session")
	RequestAuthSchemeProjectKey          = attribute.Key("gram.request.auth_scheme_project")
	RequestAuthSchemeAPIKeyKey           = attribute.Key("gram.request.auth_scheme_api_key")
	RequestAuthSessionIDKey              = attribute.Key("gram.request.auth_session_id")
	RequestAuthUserEmailKey              = attribute.Key("gram.request.auth_user_email")
	RequestAuthUserIDKey                 = attribute.Key("gram.request.auth_user_id")
	RequestAuthUserExternalIDKey         = attribute.Key("gram.request.auth_external_user_id")
	RequestAuthSchemeAPIKeyErrorKey      = attribute.Key("gram.request.auth_api_key_error")
	RequestAuthSchemeSessionErrorKey     = attribute.Key("gram.request.auth_session_error")
	RequestAuthSchemeProjectSlugErrorKey = attribute.Key("gram.request.auth_project_slug_error")
	RequestCustomDomainIDKey             = attribute.Key("gram.request.custom_domain_id")
	RequestCustomDomainNameKey           = attribute.Key("gram.request.custom_domain_name")
	RequestMCPSlugKey                    = attribute.Key("gram.request.mcp_slug")
	RequestMCPAuthorizationHeaderSetKey  = attribute.Key("gram.request.mcp_authorization_header_set")
	RequestMCPChatSessionHeaderSetKey    = attribute.Key("gram.request.mcp_chat_session_header_set")
)

func WideEvent() attribute.KeyValue { return WideEventKey.Bool(true) }
func SlogWideEvent() slog.Attr      { return slog.Bool(string(WideEventKey), true) }

func RequestPanic(v any) attribute.KeyValue { return RequestPanicKey.String(fmt.Sprintf("%v", v)) }
func SlogRequestPanic(v any) slog.Attr      { return slog.Any(string(RequestPanicKey), v) }

func RequestPanicStack(v string) attribute.KeyValue { return RequestPanicStackKey.String(v) }
func SlogRequestPanicStack(v string) slog.Attr      { return slog.String(string(RequestPanicStackKey), v) }

func RequestAuthAccountType(v string) attribute.KeyValue { return RequestAuthAccountTypeKey.String(v) }
func SlogRequestAuthAccountType(v string) slog.Attr {
	return slog.String(string(RequestAuthAccountTypeKey), v)
}

func RequestAuthAPIKeyID(v string) attribute.KeyValue { return RequestAuthAPIKeyIDKey.String(v) }
func SlogRequestAuthAPIKeyID(v string) slog.Attr {
	return slog.String(string(RequestAuthAPIKeyIDKey), v)
}

func RequestAuthOrganizationID(v string) attribute.KeyValue {
	return RequestAuthOrganizationIDKey.String(v)
}
func SlogRequestAuthOrganizationID(v string) slog.Attr {
	return slog.String(string(RequestAuthOrganizationIDKey), v)
}

func RequestAuthOrganizationSlug(v string) attribute.KeyValue {
	return RequestAuthOrganizationSlugKey.String(v)
}
func SlogRequestAuthOrganizationSlug(v string) slog.Attr {
	return slog.String(string(RequestAuthOrganizationSlugKey), v)
}

func RequestAuthProjectID(v string) attribute.KeyValue { return RequestAuthProjectIDKey.String(v) }
func SlogRequestAuthProjectID(v string) slog.Attr {
	return slog.String(string(RequestAuthProjectIDKey), v)
}

func RequestAuthProjectSlug(v string) attribute.KeyValue { return RequestAuthProjectSlugKey.String(v) }
func SlogRequestAuthProjectSlug(v string) slog.Attr {
	return slog.String(string(RequestAuthProjectSlugKey), v)
}

func RequestAuthSessionScheme(matched bool) attribute.KeyValue {
	return RequestAuthSchemeSessionKey.Bool(matched)
}
func SlogRequestAuthSessionScheme(matched bool) slog.Attr {
	return slog.Bool(string(RequestAuthSchemeSessionKey), matched)
}
func RequestAuthProjectScheme(matched bool) attribute.KeyValue {
	return RequestAuthSchemeProjectKey.Bool(matched)
}
func SlogRequestAuthProjectScheme(matched bool) slog.Attr {
	return slog.Bool(string(RequestAuthSchemeProjectKey), matched)
}
func RequestAuthAPIKeyScheme(matched bool) attribute.KeyValue {
	return RequestAuthSchemeAPIKeyKey.Bool(matched)
}
func SlogRequestAuthAPIKeyScheme(matched bool) slog.Attr {
	return slog.Bool(string(RequestAuthSchemeAPIKeyKey), matched)
}

func RequestAuthSessionID(v string) attribute.KeyValue { return RequestAuthSessionIDKey.String(v) }
func SlogRequestAuthSessionID(v string) slog.Attr {
	return slog.String(string(RequestAuthSessionIDKey), v)
}

func RequestAuthUserEmail(v string) attribute.KeyValue { return RequestAuthUserEmailKey.String(v) }
func SlogRequestAuthUserEmail(v string) slog.Attr {
	return slog.String(string(RequestAuthUserEmailKey), v)
}

func RequestAuthUserID(v string) attribute.KeyValue { return RequestAuthUserIDKey.String(v) }
func SlogRequestAuthUserID(v string) slog.Attr      { return slog.String(string(RequestAuthUserIDKey), v) }

func RequestAuthUserExternalID(v string) attribute.KeyValue {
	return RequestAuthUserExternalIDKey.String(v)
}
func SlogRequestAuthUserExternalID(v string) slog.Attr {
	return slog.String(string(RequestAuthUserExternalIDKey), v)
}

func RequestAuthSchemeAPIKeyError(v string) attribute.KeyValue {
	return RequestAuthSchemeAPIKeyErrorKey.String(v)
}
func SlogRequestAuthSchemeAPIKeyError(v string) slog.Attr {
	return slog.String(string(RequestAuthSchemeAPIKeyErrorKey), v)
}

func RequestAuthSchemeSessionError(v string) attribute.KeyValue {
	return RequestAuthSchemeSessionErrorKey.String(v)
}
func SlogRequestAuthSchemeSessionError(v string) slog.Attr {
	return slog.String(string(RequestAuthSchemeSessionErrorKey), v)
}

func RequestAuthSchemeProjectSlugError(v string) attribute.KeyValue {
	return RequestAuthSchemeProjectSlugErrorKey.String(v)
}
func SlogRequestAuthSchemeProjectSlugError(v string) slog.Attr {
	return slog.String(string(RequestAuthSchemeProjectSlugErrorKey), v)
}

func RequestCustomDomainID(v string) attribute.KeyValue { return RequestCustomDomainIDKey.String(v) }
func SlogRequestCustomDomainID(v string) slog.Attr {
	return slog.String(string(RequestCustomDomainIDKey), v)
}

func RequestCustomDomainName(v string) attribute.KeyValue {
	return RequestCustomDomainNameKey.String(v)
}
func SlogRequestCustomDomainName(v string) slog.Attr {
	return slog.String(string(RequestCustomDomainNameKey), v)
}

func RequestMCPSlug(v string) attribute.KeyValue { return RequestMCPSlugKey.String(v) }
func SlogRequestMCPSlug(v string) slog.Attr {
	return slog.String(string(RequestMCPSlugKey), v)
}

func RequestMCPAuthorizationHeaderSet(v bool) attribute.KeyValue {
	return RequestMCPAuthorizationHeaderSetKey.Bool(v)
}
func SlogRequestMCPAuthorizationHeaderSet(v bool) slog.Attr {
	return slog.Bool(string(RequestMCPAuthorizationHeaderSetKey), v)
}

func RequestMCPChatSessionHeaderSet(v bool) attribute.KeyValue {
	return RequestMCPChatSessionHeaderSetKey.Bool(v)
}
func SlogRequestMCPChatSessionHeaderSet(v bool) slog.Attr {
	return slog.Bool(string(RequestMCPChatSessionHeaderSetKey), v)
}
