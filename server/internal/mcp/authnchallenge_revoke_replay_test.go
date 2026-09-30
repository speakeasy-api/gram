package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestHandleRevoke_ConsumedRefreshReplay(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	toolset, _, client, original := seedRefreshReplaySession(t, ctx, ti)
	result := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, original)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusOK, result.code, result.body)
	var response tokenResponseFixture
	require.NoError(t, json.Unmarshal([]byte(result.body), &response))

	performRevokeReplayRequest(t, ctx, ti, toolset.McpSlug.String, client.ClientID, original)
	// Revoking the lost-response token drops its replay and revokes the live
	// successor refresh/access pair it would have served.
	jti, err := sessiontokens.NewSigner("test-jwt-secret").VerifiedJTI(response.AccessToken)
	require.NoError(t, err)
	revoked, err := ti.chatSessionsManager.IsTokenRevoked(ctx, jti)
	require.NoError(t, err)
	require.True(t, revoked)
	for _, token := range []string{original, response.RefreshToken} {
		result := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, token)
		require.NoError(t, result.err)
		require.Equal(t, http.StatusBadRequest, result.code, result.body)
		require.Contains(t, result.body, "invalid_grant")
	}
	// Already revoked remains a silent success.
	performRevokeReplayRequest(t, ctx, ti, toolset.McpSlug.String, client.ClientID, original)
}

func TestHandleRevoke_ConsumedRefreshReplayDoesNotFollowLaterRotations(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	toolset, _, client, original := seedRefreshReplaySession(t, ctx, ti)
	tokens := []string{original}
	for range 3 {
		result := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, tokens[len(tokens)-1])
		require.NoError(t, result.err)
		require.Equal(t, http.StatusOK, result.code, result.body)
		var response tokenResponseFixture
		require.NoError(t, json.Unmarshal([]byte(result.body), &response))
		tokens = append(tokens, response.RefreshToken)
	}

	// The original's replay is dropped, but its successor was already rotated,
	// so the live head two rotations later is untouched.
	performRevokeReplayRequest(t, ctx, ti, toolset.McpSlug.String, client.ClientID, original)
	stale := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, original)
	require.NoError(t, stale.err)
	require.Equal(t, http.StatusBadRequest, stale.code, stale.body)
	live := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, tokens[len(tokens)-1])
	require.NoError(t, live.err)
	require.Equal(t, http.StatusOK, live.code, live.body)
}

func TestHandleRevoke_ConsumedRefreshReplayClientBinding(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, original := seedRefreshReplaySession(t, ctx, ti)
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, original)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusOK, first.code, first.body)
	other, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                "other-client-" + uuid.NewString(),
		ClientName:              "other client",
		RedirectUris:            []string{"http://localhost:3001/callback"},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)
	performRevokeReplayRequest(t, ctx, ti, toolset.McpSlug.String, other.ClientID, original)
	replayed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, original)
	require.NoError(t, replayed.err)
	require.Equal(t, http.StatusOK, replayed.code, replayed.body)
	assertSameTokenPair(t, first.body, replayed.body)
}

func TestHandleRevoke_ConsumedRefreshReplayKeyBinding(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, original := seedRefreshReplaySession(t, ctx, ti)
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, original)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusOK, first.code, first.body)
	cacheKey := func(token string) string {
		hash := sha256.Sum256([]byte(token))
		return "userSessionRefreshReplay:" + issuer.ID.String() + ":" + base64.RawURLEncoding.EncodeToString(hash[:]) + ":"
	}
	var entry map[string]any
	require.NoError(t, ti.cacheAdapter.Get(ctx, cacheKey(original), &entry))
	// A copied encrypted payload is not authority to revoke the embedded
	// successor when presented under a different token's cache key.
	unknown := "unknown-" + uuid.NewString()
	require.NoError(t, ti.cacheAdapter.Set(ctx, cacheKey(unknown), entry, time.Minute))
	performRevokeReplayRequest(t, ctx, ti, toolset.McpSlug.String, client.ClientID, unknown)
	var preserved map[string]any
	require.NoError(t, ti.cacheAdapter.Get(ctx, cacheKey(unknown), &preserved))
	require.Equal(t, entry, preserved)
	replayed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, original)
	require.NoError(t, replayed.err)
	require.Equal(t, http.StatusOK, replayed.code, replayed.body)
	assertSameTokenPair(t, first.body, replayed.body)
}

func performRevokeReplayRequest(t *testing.T, ctx context.Context, ti *testInstance, slug, clientID, token string) {
	t.Helper()
	form := url.Values{"token": {token}, "token_type_hint": {"refresh_token"}, "client_id": {clientID}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleRevoke(w, req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}
