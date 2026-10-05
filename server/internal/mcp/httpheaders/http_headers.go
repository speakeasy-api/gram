// Package httpheaders contains shared MCP request header parsing helpers.
package httpheaders

import (
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// IsStandardMCPRequestHeader reports whether name is one of the MCP
// Streamable HTTP standard request headers that mirror protocol metadata
// into HTTP headers (the 2026-07-28 Request Metadata section):
// MCP-Protocol-Version (canonically mcpversions.HTTPHeader), Mcp-Method,
// Mcp-Name, and the Mcp-Param-{Name} family. Comparison is
// case-insensitive, per RFC 9110 field-name rules.
//
// These headers are sent by every conforming client as protocol metadata,
// as distinct from the user-supplied Mcp-* variable headers Gram's hosted
// runtime accepts. The hosted runtime never maps a standard header to a
// tool variable, on any protocol revision, and configuration rejects
// variable names that would need one ([IsReservedVariableHeaderName]).
func IsStandardMCPRequestHeader(name string) bool {
	if strings.EqualFold(name, mcpversions.HTTPHeader) || strings.EqualFold(name, MethodHeader) || strings.EqualFold(name, NameHeader) {
		return true
	}

	// The Mcp-Param-{Name} family requires a non-empty name; a bare
	// "Mcp-Param-" is not a standard header.
	return len(name) > len(ParamHeaderPrefix) && strings.EqualFold(name[:len(ParamHeaderPrefix)], ParamHeaderPrefix)
}

// VariableHeaderName returns the request header the hosted runtime reads a
// configured tool variable name or header display name from: "MCP-" followed
// by the name, with underscores and spaces written as dashes (a variable
// PARAM_REGION arrives as MCP-PARAM-REGION, a display name "Param Region" as
// MCP-Param-Region).
func VariableHeaderName(name string) string {
	return "MCP-" + strings.NewReplacer("_", "-", " ", "-").Replace(strings.TrimSpace(name))
}

// IsReservedVariableHeaderName reports whether a configured tool variable
// name or header display name would be supplied through a standard MCP
// request header ([IsStandardMCPRequestHeader]), so it could never receive a
// value: the hosted runtime never reads those headers as variables.
//
// Names derived from a slug (OpenAPI security and server URL variables,
// external MCP header variables) are not checked: they collide only when
// the slug itself is "param" or starts with "param-", and those variables
// remain settable through the stored environment.
func IsReservedVariableHeaderName(name string) bool {
	return IsStandardMCPRequestHeader(VariableHeaderName(name))
}

// AuthorizationBearerToken returns the Bearer token from the request's
// Authorization header with the "Bearer " prefix stripped. Prefix matching
// is case-insensitive per RFC 7235 § 2.1 ("Bearer" / "BEARER" / "bearer").
// Returns "" when the header is absent or carries a non-Bearer scheme — a
// non-empty return is always a Bearer token, never the raw header. This
// matters for callers that synthesise an upstream Authorization header
// from the result; forwarding a non-Bearer value would produce a malformed
// "Bearer <other-scheme> ..." upstream.
//
// Do NOT loosen this for the Gram API key install-snippet case (raw key
// with no Bearer prefix). The lenient path lives in
// [AuthorizationOrChatSessionToken], which is the only helper used by the
// identity-auth path. OAuth-forwarding callers must keep the strict
// semantics here.
func AuthorizationBearerToken(r *http.Request) string {
	const bearerPrefix = "Bearer "
	token := r.Header.Get("Authorization")
	if len(token) >= len(bearerPrefix) && strings.EqualFold(token[:len(bearerPrefix)], bearerPrefix) {
		return token[len(bearerPrefix):]
	}
	return ""
}

// AuthorizationOrChatSessionToken returns the caller's identity token,
// drawn from the Authorization header when present, otherwise the
// Gram-Chat-Session header. Returns "" when neither carries a value.
//
// Unlike [AuthorizationBearerToken], the Authorization header is read
// leniently: a "Bearer " prefix is stripped when present, but a header
// that lacks the prefix is returned verbatim. The hosted MCP install page
// (server/internal/mcpmetadata/hosted_page.html.tmpl) emits
// "Authorization:${GRAM_KEY}" with no scheme, so users paste their raw
// Gram API key directly. PR #2540 briefly required strict Bearer parsing
// here and broke every existing private MCP install — don't repeat that.
// If you ever tighten this, update the install-page snippets to emit a
// Bearer prefix in the same change.
//
// Leniency is safe in the identity-auth path because the returned token
// is only used to look up a Gram API key / chat-session JWT in our own
// data store; a non-Bearer scheme like "Basic ..." just fails that
// lookup. It is NOT safe for OAuth upstream-forwarding callers, which is
// why [AuthorizationBearerToken] stays strict.
func AuthorizationOrChatSessionToken(r *http.Request) string {
	const bearerPrefix = "Bearer "
	raw := r.Header.Get("Authorization")
	if len(raw) >= len(bearerPrefix) && strings.EqualFold(raw[:len(bearerPrefix)], bearerPrefix) {
		raw = raw[len(bearerPrefix):]
	}
	if raw != "" {
		return raw
	}
	return r.Header.Get(constants.ChatSessionsTokenHeader)
}
