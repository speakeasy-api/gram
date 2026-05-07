package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/mcp"
)

func newAuthRequest(t *testing.T, header string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	return req
}

func newIdentityRequest(t *testing.T, authHeader, chatSession string) *http.Request {
	t.Helper()

	req := newAuthRequest(t, authHeader)
	if chatSession != "" {
		req.Header.Set(constants.ChatSessionsTokenHeader, chatSession)
	}
	return req
}

func TestAuthorizationBearerToken_AbsentHeader(t *testing.T) {
	t.Parallel()

	require.Empty(t, mcp.AuthorizationBearerToken(newAuthRequest(t, "")))
}

func TestAuthorizationBearerToken_BearerCanonical(t *testing.T) {
	t.Parallel()

	require.Equal(t, "abc123", mcp.AuthorizationBearerToken(newAuthRequest(t, "Bearer abc123")))
}

func TestAuthorizationBearerToken_BearerLowercase(t *testing.T) {
	t.Parallel()

	require.Equal(t, "abc123", mcp.AuthorizationBearerToken(newAuthRequest(t, "bearer abc123")))
}

func TestAuthorizationBearerToken_BearerUppercase(t *testing.T) {
	t.Parallel()

	require.Equal(t, "abc123", mcp.AuthorizationBearerToken(newAuthRequest(t, "BEARER abc123")))
}

func TestAuthorizationBearerToken_BearerEmptyToken(t *testing.T) {
	t.Parallel()

	// "Bearer " with no token after the space is technically a valid prefix
	// match but yields no token; callers treat empty returns as "no auth".
	require.Empty(t, mcp.AuthorizationBearerToken(newAuthRequest(t, "Bearer ")))
}

// TestAuthorizationBearerToken_BasicScheme is the regression test for the
// non-Bearer fallthrough: returning the raw header would land "Basic abc123"
// inside an upstream "Authorization: Bearer Basic abc123" — see the function
// docstring for the proxy-forwarding rationale.
func TestAuthorizationBearerToken_BasicScheme(t *testing.T) {
	t.Parallel()

	require.Empty(t, mcp.AuthorizationBearerToken(newAuthRequest(t, "Basic abc123")))
}

func TestAuthorizationBearerToken_DigestScheme(t *testing.T) {
	t.Parallel()

	require.Empty(t, mcp.AuthorizationBearerToken(newAuthRequest(t, `Digest username="u", realm="r"`)))
}

func TestAuthorizationBearerToken_BareWordBearerNoSpace(t *testing.T) {
	t.Parallel()

	// "Bearer" alone is shorter than the "Bearer " prefix and must not match.
	require.Empty(t, mcp.AuthorizationBearerToken(newAuthRequest(t, "Bearer")))
}

func TestAuthorizationBearerToken_BearerLikePrefix(t *testing.T) {
	t.Parallel()

	// A scheme that starts with the same letters but isn't "Bearer " must not
	// match — guards against a hypothetical "Bearer-Like xyz" or similar.
	require.Empty(t, mcp.AuthorizationBearerToken(newAuthRequest(t, "Bearer-Like xyz")))
}

// TestAuthorizationBearerToken_RawKeyNoScheme documents that the strict
// helper does NOT accept a raw token — the lenient path lives in
// [AuthorizationOrChatSessionToken]. See that helper's tests for the
// install-page regression guard.
func TestAuthorizationBearerToken_RawKeyNoScheme(t *testing.T) {
	t.Parallel()

	require.Empty(t, mcp.AuthorizationBearerToken(newAuthRequest(t, "gram_live_abc123")))
}

func TestAuthorizationOrChatSessionToken_BothEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, mcp.AuthorizationOrChatSessionToken(newIdentityRequest(t, "", "")))
}

func TestAuthorizationOrChatSessionToken_BearerOnly(t *testing.T) {
	t.Parallel()

	require.Equal(t, "abc123", mcp.AuthorizationOrChatSessionToken(newIdentityRequest(t, "Bearer abc123", "")))
}

func TestAuthorizationOrChatSessionToken_ChatSessionOnly(t *testing.T) {
	t.Parallel()

	require.Equal(t, "session-jwt", mcp.AuthorizationOrChatSessionToken(newIdentityRequest(t, "", "session-jwt")))
}

func TestAuthorizationOrChatSessionToken_BearerWinsWhenBothSet(t *testing.T) {
	t.Parallel()

	require.Equal(t, "abc123", mcp.AuthorizationOrChatSessionToken(newIdentityRequest(t, "Bearer abc123", "session-jwt")))
}

// TestAuthorizationOrChatSessionToken_RawKeyWinsOverChatSession is the
// regression guard for the hosted install-page snippets: a raw Gram API
// key with no Bearer prefix must round-trip through the identity-auth
// helper untouched and pre-empt the chat-session fallback. See the
// [mcp.AuthorizationOrChatSessionToken] docstring.
func TestAuthorizationOrChatSessionToken_RawKeyWinsOverChatSession(t *testing.T) {
	t.Parallel()

	require.Equal(t, "gram_live_abc123", mcp.AuthorizationOrChatSessionToken(newIdentityRequest(t, "gram_live_abc123", "session-jwt")))
}

func TestAuthorizationOrChatSessionToken_RawKeyAlone(t *testing.T) {
	t.Parallel()

	require.Equal(t, "gram_live_abc123", mcp.AuthorizationOrChatSessionToken(newIdentityRequest(t, "gram_live_abc123", "")))
}

func TestAuthorizationOrChatSessionToken_BearerEmptyTokenFallsThrough(t *testing.T) {
	t.Parallel()

	// "Bearer " with no token after the prefix yields an empty Authorization
	// value, so the chat-session header takes over.
	require.Equal(t, "session-jwt", mcp.AuthorizationOrChatSessionToken(newIdentityRequest(t, "Bearer ", "session-jwt")))
}

// TestAuthorizationOrChatSessionToken_BasicSchemePreEmptsChatSession
// documents that under the lenient identity-auth policy a non-Bearer
// scheme is returned verbatim and pre-empts the chat-session header. The
// returned value will fail API-key lookup downstream — fine for identity
// auth but unsafe for OAuth upstream forwarding (which uses the strict
// [mcp.AuthorizationBearerToken] helper instead).
func TestAuthorizationOrChatSessionToken_BasicSchemePreEmptsChatSession(t *testing.T) {
	t.Parallel()

	require.Equal(t, "Basic abc123", mcp.AuthorizationOrChatSessionToken(newIdentityRequest(t, "Basic abc123", "session-jwt")))
}

func TestAuthorizationOrChatSessionToken_BasicSchemeAlone(t *testing.T) {
	t.Parallel()

	require.Equal(t, "Basic abc123", mcp.AuthorizationOrChatSessionToken(newIdentityRequest(t, "Basic abc123", "")))
}
