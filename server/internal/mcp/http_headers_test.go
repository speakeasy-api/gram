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

	// "Bearer " with no token after the space matches the prefix and strips
	// to empty; callers treat empty returns as "no auth".
	require.Empty(t, mcp.AuthorizationBearerToken(newAuthRequest(t, "Bearer ")))
}

// TestAuthorizationBearerToken_RawKeyNoScheme is the regression guard for the
// hosted install-page snippets, which emit "Authorization:${GRAM_KEY}" with
// no scheme prefix. The user's raw Gram API key must round-trip through this
// helper untouched. See the [mcp.AuthorizationBearerToken] docstring.
func TestAuthorizationBearerToken_RawKeyNoScheme(t *testing.T) {
	t.Parallel()

	require.Equal(t, "gram_live_abc123", mcp.AuthorizationBearerToken(newAuthRequest(t, "gram_live_abc123")))
}

func TestAuthorizationBearerToken_BasicScheme(t *testing.T) {
	t.Parallel()

	// Non-Bearer schemes are returned verbatim under the lenient policy.
	require.Equal(t, "Basic abc123", mcp.AuthorizationBearerToken(newAuthRequest(t, "Basic abc123")))
}

func TestAuthorizationBearerToken_DigestScheme(t *testing.T) {
	t.Parallel()

	require.Equal(t, `Digest username="u", realm="r"`, mcp.AuthorizationBearerToken(newAuthRequest(t, `Digest username="u", realm="r"`)))
}

func TestAuthorizationBearerToken_BareWordBearerNoSpace(t *testing.T) {
	t.Parallel()

	// "Bearer" alone is shorter than the "Bearer " prefix; it falls through
	// the prefix check and is returned verbatim.
	require.Equal(t, "Bearer", mcp.AuthorizationBearerToken(newAuthRequest(t, "Bearer")))
}

func TestAuthorizationBearerToken_BearerLikePrefix(t *testing.T) {
	t.Parallel()

	// A scheme that starts with the same letters but isn't "Bearer " is
	// returned verbatim rather than matched as a prefix.
	require.Equal(t, "Bearer-Like xyz", mcp.AuthorizationBearerToken(newAuthRequest(t, "Bearer-Like xyz")))
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

// TestAuthorizationOrChatSessionToken_RawKeyWinsOverChatSession asserts the
// lenient-bearer path: a raw Authorization value (e.g. an unprefixed Gram
// API key from the install-page snippets) is returned and pre-empts the
// chat-session fallback.
func TestAuthorizationOrChatSessionToken_RawKeyWinsOverChatSession(t *testing.T) {
	t.Parallel()

	require.Equal(t, "gram_live_abc123", mcp.AuthorizationOrChatSessionToken(newIdentityRequest(t, "gram_live_abc123", "session-jwt")))
}

func TestAuthorizationOrChatSessionToken_NonBearerSchemePreEmptsChatSession(t *testing.T) {
	t.Parallel()

	// Non-Bearer schemes are returned verbatim, so they take priority over
	// the chat-session header just like Bearer and raw values do.
	require.Equal(t, "Basic abc123", mcp.AuthorizationOrChatSessionToken(newIdentityRequest(t, "Basic abc123", "session-jwt")))
}

func TestAuthorizationOrChatSessionToken_BasicSchemeAlone(t *testing.T) {
	t.Parallel()

	require.Equal(t, "Basic abc123", mcp.AuthorizationOrChatSessionToken(newIdentityRequest(t, "Basic abc123", "")))
}
