package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type mintedTokens struct {
	AccessToken            string `json:"access_token"`
	RefreshToken           string `json:"refresh_token"`
	TokenType              string `json:"token_type"`
	ExpiresIn              int64  `json:"expires_in"`
	AuthorizationExpiresIn int64  `json:"authorization_expires_in"`
}

func TestTokenRefresh_ReusesRefreshToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	first := mintIssuerTokens(t, ctx, ti, toolset.McpSlug.String, toolset.UserSessionIssuerID.UUID, client)

	second := postRefreshToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, first.RefreshToken)
	require.Equal(t, http.StatusOK, second.Code, "body=%s", second.Body.String())
	replayed := decodeMintedTokens(t, second)
	require.Equal(t, first.RefreshToken, replayed.RefreshToken, "refresh token must not rotate")
	require.NotEqual(t, first.AccessToken, replayed.AccessToken)

	third := postRefreshToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, first.RefreshToken)
	require.Equal(t, http.StatusOK, third.Code, "body=%s", third.Body.String())
	again := decodeMintedTokens(t, third)
	require.Equal(t, first.RefreshToken, again.RefreshToken)
	require.NotEqual(t, replayed.AccessToken, again.AccessToken)
}

func TestTokenRefresh_UnknownToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)

	w := postRefreshToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, "not-a-real-refresh-token")
	require.Equal(t, http.StatusBadRequest, w.Code)
	requireTokenOAuthError(t, w, "invalid_grant")
}

func TestTokenRefresh_RevokedToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	tokens := mintIssuerTokens(t, ctx, ti, toolset.McpSlug.String, toolset.UserSessionIssuerID.UUID, client)

	revoke := postRevokeToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, tokens.RefreshToken)
	require.Equal(t, http.StatusOK, revoke.Code)

	w := postRefreshToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, tokens.RefreshToken)
	require.Equal(t, http.StatusBadRequest, w.Code)
	requireTokenOAuthError(t, w, "invalid_grant")
}

func TestTokenRefresh_ExpiredToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	refresh := "expired-refresh-" + uuid.NewString()
	_, err := usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: issuer.ID,
		UserSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true},
		SubjectUrn:          urn.NewAnonymousSubject(uuid.NewString()),
		Jti:                 "jti-" + uuid.NewString(),
		RefreshTokenHash:    opaqueRefreshHash(refresh),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), InfinityModifier: 0, Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), InfinityModifier: 0, Valid: true},
	})
	require.NoError(t, err)

	w := postRefreshToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, refresh)
	require.Equal(t, http.StatusBadRequest, w.Code)
	requireTokenOAuthError(t, w, "invalid_grant")
	require.Contains(t, w.Body.String(), "refresh_token has expired")
}

func TestTokenRefresh_ClientMismatchRevokesToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, owner := seedPrivateToolsetWithIssuer(t, ctx, ti)
	other, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:   issuer.ID,
		ClientID:              "other-client-" + uuid.NewString()[:8],
		ClientSecretHash:      pgtype.Text{},
		ClientName:            "other client",
		RedirectUris:          []string{"http://localhost:3000/other"},
		ClientSecretExpiresAt: pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	tokens := mintIssuerTokens(t, ctx, ti, toolset.McpSlug.String, issuer.ID, owner)
	mismatch := postRefreshToken(t, ctx, ti, toolset.McpSlug.String, other.ClientID, tokens.RefreshToken)
	require.Equal(t, http.StatusBadRequest, mismatch.Code)
	requireTokenOAuthError(t, mismatch, "invalid_grant")
	require.Contains(t, mismatch.Body.String(), "issued to a different client")

	ownerRetry := postRefreshToken(t, ctx, ti, toolset.McpSlug.String, owner.ClientID, tokens.RefreshToken)
	require.Equal(t, http.StatusBadRequest, ownerRetry.Code)
	requireTokenOAuthError(t, ownerRetry, "invalid_grant")
}

func TestTokenRefresh_ConcurrentReplaySucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	first := mintIssuerTokens(t, ctx, ti, toolset.McpSlug.String, toolset.UserSessionIssuerID.UUID, client)

	const n = 8
	type refreshResult struct {
		w   *httptest.ResponseRecorder
		err error
	}
	results := make([]refreshResult, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			w, err := doHostedTokenForm(ctx, ti, toolset.McpSlug.String, refreshTokenForm(client.ClientID, first.RefreshToken))
			results[i] = refreshResult{w: w, err: err}
		}()
	}
	wg.Wait()

	for i, result := range results {
		require.NoError(t, result.err, "request %d", i)
		require.Equal(t, http.StatusOK, result.w.Code, "request %d body=%s", i, result.w.Body.String())
		got := decodeMintedTokens(t, result.w)
		require.Equal(t, first.RefreshToken, got.RefreshToken)
		require.NotEmpty(t, got.AccessToken)
	}
}

func mintIssuerTokens(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	mcpSlug string,
	issuerID uuid.UUID,
	client usersessions_repo.UserSessionClient,
) mintedTokens {
	t.Helper()

	redirectURI := client.RedirectUris[0]
	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	code := "auth-code-" + uuid.NewString()
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
		Code:                        code,
		FlowID:                      "",
		UserSessionIssuerID:         issuerID,
		UserSessionClientID:         client.ID,
		ClientID:                    client.ClientID,
		RedirectURI:                 redirectURI,
		CodeChallenge:               base64.RawURLEncoding.EncodeToString(sum[:]),
		CodeChallengeMethod:         "S256",
		Subject:                     urn.NewAnonymousSubject(uuid.NewString()),
		DesiredSessionDurationHours: 0,
		CreatedAt:                   time.Now(),
	}))

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", client.ClientID)
	form.Set("code_verifier", verifier)
	w := postHostedTokenForm(t, ctx, ti, mcpSlug, form)
	require.Equal(t, http.StatusOK, w.Code, "authorization_code grant: %s", w.Body.String())
	tokens := decodeMintedTokens(t, w)
	require.NotEmpty(t, tokens.AccessToken)
	require.NotEmpty(t, tokens.RefreshToken)
	return tokens
}

func refreshTokenForm(clientID, refreshToken string) url.Values {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	return form
}

func postRefreshToken(t *testing.T, ctx context.Context, ti *testInstance, mcpSlug, clientID, refreshToken string) *httptest.ResponseRecorder {
	t.Helper()
	return postHostedTokenForm(t, ctx, ti, mcpSlug, refreshTokenForm(clientID, refreshToken))
}

func postRevokeToken(t *testing.T, ctx context.Context, ti *testInstance, mcpSlug, clientID, token string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{}
	form.Set("token", token)
	form.Set("token_type_hint", "refresh_token")
	form.Set("client_id", clientID)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/"+mcpSlug+"/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleRevoke(w, req))
	return w
}

func postHostedTokenForm(t *testing.T, ctx context.Context, ti *testInstance, mcpSlug string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	w, err := doHostedTokenForm(ctx, ti, mcpSlug, form)
	require.NoError(t, err)
	return w
}

func doHostedTokenForm(ctx context.Context, ti *testInstance, mcpSlug string, form url.Values) (*httptest.ResponseRecorder, error) {
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/"+mcpSlug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	if err := ti.service.HandleToken(w, req); err != nil {
		return w, fmt.Errorf("handle token: %w", err)
	}
	return w, nil
}

func decodeMintedTokens(t *testing.T, w *httptest.ResponseRecorder) mintedTokens {
	t.Helper()

	var tokens mintedTokens
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tokens))
	return tokens
}

func opaqueRefreshHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
