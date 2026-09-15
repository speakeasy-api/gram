package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

// stubChatSessionValidator stands in for *chatsessions.Manager, whose real
// ValidateToken checks revocation against Redis. The narrow interface is what
// lets this file exist at all: the middleware package has no testcontainer
// harness and should not grow one to test header handling.
type stubChatSessionValidator struct {
	audience     []string
	invalidToken bool
	err          error
}

func (s stubChatSessionValidator) ValidateToken(_ context.Context, _ string) (*chatsessions.ChatSessionClaims, bool, error) {
	if s.invalidToken || s.err != nil {
		return nil, s.invalidToken, s.err
	}
	return &chatsessions.ChatSessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings(s.audience)},
	}, false, nil
}

// serveChatSessionCORS runs a request through chatSessionsCORS and reports
// what the downstream handler saw.
func serveChatSessionCORS(t *testing.T, validator ChatSessionValidator, req *http.Request) (rec *httptest.ResponseRecorder, reached bool, originTrusted bool) {
	t.Helper()

	handler := chatSessionsCORS(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		originTrusted = chatSessionOriginTrusted(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, reached, originTrusted
}

func elementsMCPRequest() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "https://app.getgram.ai/mcp/petstore", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(constants.ChatSessionsTokenHeader, "a-chat-session-token")
	req.Header.Set("Origin", elementsOrigin)
	return req
}

// Elements embedded on a customer domain: the audience claim names the
// embedding origin, so the request is admitted and marked trusted for
// MCPSecurity downstream.
func TestChatSessionsCORS_AudienceMatchMarksOriginTrusted(t *testing.T) {
	t.Parallel()

	validator := stubChatSessionValidator{audience: []string{elementsOrigin}, invalidToken: false, err: nil}

	rec, reached, originTrusted := serveChatSessionCORS(t, validator, elementsMCPRequest())

	require.True(t, reached)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, elementsOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
	require.Contains(t, rec.Header().Get("Vary"), "Origin")
	require.True(t, originTrusted, "an audience-validated origin must be exempt from the same-origin check")
}

func TestChatSessionsCORS_AudienceMismatchIsForbidden(t *testing.T) {
	t.Parallel()

	validator := stubChatSessionValidator{audience: []string{"https://someone-else.com"}, invalidToken: false, err: nil}

	rec, reached, _ := serveChatSessionCORS(t, validator, elementsMCPRequest())

	require.False(t, reached)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestChatSessionsCORS_InvalidTokenIsUnauthorized(t *testing.T) {
	t.Parallel()

	validator := stubChatSessionValidator{audience: nil, invalidToken: true, err: nil}

	rec, reached, _ := serveChatSessionCORS(t, validator, elementsMCPRequest())

	require.False(t, reached)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// A request with no chat session is passed through unmarked, so MCPSecurity
// applies its own check. This is the credential-free public MCP path.
func TestChatSessionsCORS_NoSessionDoesNotMarkTrusted(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "https://app.getgram.ai/mcp/petstore", strings.NewReader("{}"))
	req.Header.Set("Origin", hostileOrigin)

	_, reached, originTrusted := serveChatSessionCORS(t, stubChatSessionValidator{audience: nil, invalidToken: false, err: nil}, req)

	require.True(t, reached, "chatSessionsCORS itself does not gate anonymous requests")
	require.False(t, originTrusted, "no chat session means no exemption")
}

// The regression this issue uncovered: a dummy Gram-Key used to buy a hostile
// origin a readable response from a public MCP server. Gram-Key authenticates
// nothing under /mcp, so the echo is gone there.
func TestChatSessionsCORS_GramKeyDoesNotEchoOriginOnMCPRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/mcp/petstore",
		"/mcp/petstore/token",
		"/mcp/petstore/register",
		"/mcp/petstore/authorize",
		"/mcp/petstore/connect",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "https://app.getgram.ai"+path, strings.NewReader("{}"))
			req.Header.Set(constants.APIKeyHeader, "gram_key_whatever")
			req.Header.Set("Origin", hostileOrigin)

			rec, reached, _ := serveChatSessionCORS(t, stubChatSessionValidator{audience: nil, invalidToken: false, err: nil}, req)

			require.True(t, reached, "the request still runs; only the CORS echo is withheld")
			require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"),
				"a Gram-Key must not make an MCP response readable cross-origin")
		})
	}
}

// Elements' dangerousApiKey flow exchanges the key for a chat session here,
// cross-site, before any session exists — so this route must keep the echo.
func TestChatSessionsCORS_GramKeyEchoesOriginOnConsumingRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/rpc/chatSessions.create",
		"/rpc/chat.load",
		"/chat/completions",
		"/chat/turnstream",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "https://app.getgram.ai"+path, strings.NewReader("{}"))
			req.Header.Set(constants.APIKeyHeader, "gram_key_whatever")
			req.Header.Set("Origin", elementsOrigin)

			rec, reached, _ := serveChatSessionCORS(t, stubChatSessionValidator{audience: nil, invalidToken: false, err: nil}, req)

			require.True(t, reached)
			require.Equal(t, elementsOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
		})
	}
}

// loadChat is the only chat method declaring security.ByKey, so the rest of
// the /rpc/chat.* family must not hand out credentialed CORS for a key that
// was never consulted.
func TestChatSessionsCORS_GramKeyDoesNotEchoOriginOnNonConsumingChatRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/rpc/chat.list",
		"/rpc/chat.delete",
		"/rpc/chat.summarize",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "https://app.getgram.ai"+path, strings.NewReader("{}"))
			req.Header.Set(constants.APIKeyHeader, "gram_key_whatever")
			req.Header.Set("Origin", hostileOrigin)

			rec, reached, _ := serveChatSessionCORS(t, stubChatSessionValidator{audience: nil, invalidToken: false, err: nil}, req)

			require.True(t, reached)
			require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
		})
	}
}

// A validator fault is a server fault, not an auth failure: the caller must
// see 500 and the handler must not run.
func TestChatSessionsCORS_ValidatorErrorIsInternalError(t *testing.T) {
	t.Parallel()

	validator := stubChatSessionValidator{audience: nil, invalidToken: false, err: errors.New("redis unavailable")}

	rec, reached, _ := serveChatSessionCORS(t, validator, elementsMCPRequest())

	require.False(t, reached)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

// Browsers omit Origin on same-origin GET/HEAD, so the audience check falls
// back to comparing Host. That fallback is what stops a stripped Origin from
// bypassing the audience claim, so both outcomes are pinned.
func TestChatSessionsCORS_HostFallbackWhenOriginAbsent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		host       string
		audience   []string
		wantStatus int
		wantReach  bool
	}{
		{name: "host matches audience", host: "app.getgram.ai", audience: []string{"https://app.getgram.ai"}, wantStatus: http.StatusOK, wantReach: true},
		{name: "host matches http audience", host: "app.getgram.ai", audience: []string{"http://app.getgram.ai"}, wantStatus: http.StatusOK, wantReach: true},
		{name: "host does not match", host: "app.getgram.ai", audience: []string{"https://docs.customer.com"}, wantStatus: http.StatusForbidden, wantReach: false},
		{name: "empty audience", host: "app.getgram.ai", audience: nil, wantStatus: http.StatusForbidden, wantReach: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "https://app.getgram.ai/mcp/petstore", nil)
			req.Host = tt.host
			req.Header.Set(constants.ChatSessionsTokenHeader, "a-chat-session-token")

			validator := stubChatSessionValidator{audience: tt.audience, invalidToken: false, err: nil}
			rec, reached, originTrusted := serveChatSessionCORS(t, validator, req)

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantReach, reached)
			require.Equal(t, tt.wantReach, originTrusted, "trust marker must track the audience decision")
		})
	}
}

// End to end through both middlewares in their real order: CORSMiddleware runs
// chatSessionsCORS, which marks the request, which MCPSecurity then honours.
func TestChatSessionsCORS_ElementsSurvivesMCPSecurity(t *testing.T) {
	t.Parallel()

	reached := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	validator := stubChatSessionValidator{audience: []string{elementsOrigin}, invalidToken: false, err: nil}
	handler := CORSMiddleware("prod", gramOrigin, validator)(newMCPSecurity(t)(inner))

	req := elementsMCPRequest()
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.True(t, reached, "Elements must still reach the MCP handler cross-site")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, elementsOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
}

// The same chain without a chat session is the attack, and must be refused.
func TestChatSessionsCORS_HostileOriginBlockedThroughFullChain(t *testing.T) {
	t.Parallel()

	reached := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	validator := stubChatSessionValidator{audience: nil, invalidToken: false, err: nil}
	handler := CORSMiddleware("prod", gramOrigin, validator)(newMCPSecurity(t)(inner))

	req := httptest.NewRequest(http.MethodPost, "https://app.getgram.ai/mcp/petstore", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(constants.APIKeyHeader, "gram_key_whatever")
	req.Header.Set("Origin", hostileOrigin)
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.False(t, reached)
	require.Equal(t, http.StatusForbidden, rec.Code)
	// The base CORS middleware still stamps the server's own origin, which is
	// the point: the browser compares it against the requesting origin, so the
	// hostile page can neither run the call nor read the refusal.
	require.Equal(t, gramOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
	require.NotEqual(t, hostileOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
}
