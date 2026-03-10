package oauth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/oauth/providers"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// fakeUpstream creates an httptest.Server that returns the given JSON body
// with the given status code on POST /token.
func fakeUpstream(t *testing.T, status int, body map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
}

func customProvider(tokenEndpoint string) oauth_repo.OauthProxyProvider {
	return oauth_repo.OauthProxyProvider{
		ID:                                uuid.New(),
		ProviderType:                      "custom",
		TokenEndpoint:                     pgtype.Text{String: tokenEndpoint, Valid: true},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
		Secrets:                           []byte(`{"client_id":"test-id","client_secret":"test-secret"}`),
	}
}

func minimalToolset(toolsetID uuid.UUID) *toolsets_repo.Toolset {
	return &toolsets_repo.Toolset{
		ID:        toolsetID,
		ProjectID: uuid.New(),
	}
}

func TestRefreshProxyToken_Success(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newOAuthServiceTestEnv(t)
	toolsetID := uuid.New()

	// Issue token with upstream credentials
	upstreamExpiry := time.Now().Add(-1 * time.Minute) // expired so refresh is needed
	token, _, _ := env.issueToken(t, ctx, toolsetID,
		"old-upstream-access", "old-upstream-refresh", &upstreamExpiry, []string{"api_key"})

	// Validate to get the hashed-access-token variant that RefreshExternalSecrets expects
	validated, err := env.tokenService.ValidateAccessToken(ctx, toolsetID, token.AccessToken)
	require.ErrorIs(t, err, oauth.ErrExpiredExternalSecrets)
	require.NotNil(t, validated)

	// Start fake upstream that returns rotated tokens
	upstream := fakeUpstream(t, 200, map[string]any{
		"access_token":  "new-upstream-access",
		"refresh_token": "new-upstream-refresh",
		"expires_in":    3600,
	})
	defer upstream.Close()

	provider := customProvider(upstream.URL + "/token")
	toolset := minimalToolset(toolsetID)

	refreshed, err := env.service.RefreshProxyToken(ctx, toolsetID, validated, &provider, toolset)
	require.NoError(t, err)
	require.NotNil(t, refreshed)
	require.Len(t, refreshed.ExternalSecrets, 1)
	require.Equal(t, "new-upstream-access", refreshed.ExternalSecrets[0].Token)
	require.Equal(t, "new-upstream-refresh", refreshed.ExternalSecrets[0].RefreshToken)
	require.NotNil(t, refreshed.ExternalSecrets[0].ExpiresAt)
}

func TestRefreshProxyToken_PreservesRefreshToken(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newOAuthServiceTestEnv(t)
	toolsetID := uuid.New()

	upstreamExpiry := time.Now().Add(-1 * time.Minute)
	token, _, _ := env.issueToken(t, ctx, toolsetID,
		"old-upstream-access", "original-upstream-refresh", &upstreamExpiry, []string{"api_key"})

	validated, err := env.tokenService.ValidateAccessToken(ctx, toolsetID, token.AccessToken)
	require.ErrorIs(t, err, oauth.ErrExpiredExternalSecrets)

	// Upstream does NOT return a new refresh_token (non-rotating provider)
	upstream := fakeUpstream(t, 200, map[string]any{
		"access_token": "new-upstream-access",
		"expires_in":   3600,
	})
	defer upstream.Close()

	provider := customProvider(upstream.URL + "/token")
	toolset := minimalToolset(toolsetID)

	refreshed, err := env.service.RefreshProxyToken(ctx, toolsetID, validated, &provider, toolset)
	require.NoError(t, err)
	require.Len(t, refreshed.ExternalSecrets, 1)
	require.Equal(t, "new-upstream-access", refreshed.ExternalSecrets[0].Token)
	require.Equal(t, "original-upstream-refresh", refreshed.ExternalSecrets[0].RefreshToken,
		"original refresh token should be preserved when upstream doesn't rotate")
}

func TestRefreshProxyToken_NoUpstreamRefreshToken(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newOAuthServiceTestEnv(t)
	toolsetID := uuid.New()

	// Issue token WITHOUT an upstream refresh token
	upstreamExpiry := time.Now().Add(-1 * time.Minute)
	token, _, _ := env.issueToken(t, ctx, toolsetID,
		"upstream-access", "", &upstreamExpiry, []string{"api_key"})

	_, err := env.tokenService.ValidateAccessToken(ctx, toolsetID, token.AccessToken)
	require.ErrorIs(t, err, oauth.ErrExpiredAccessToken)
}

func TestRefreshProxyToken_UpstreamError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newOAuthServiceTestEnv(t)
	toolsetID := uuid.New()

	upstreamExpiry := time.Now().Add(-1 * time.Minute)
	token, _, _ := env.issueToken(t, ctx, toolsetID,
		"upstream-access", "upstream-refresh", &upstreamExpiry, []string{"api_key"})

	validated, err := env.tokenService.ValidateAccessToken(ctx, toolsetID, token.AccessToken)
	require.ErrorIs(t, err, oauth.ErrExpiredExternalSecrets)

	// Upstream returns 401
	upstream := fakeUpstream(t, 401, map[string]any{
		"error": "invalid_grant",
	})
	defer upstream.Close()

	provider := customProvider(upstream.URL + "/token")
	toolset := minimalToolset(toolsetID)

	_, err = env.service.RefreshProxyToken(ctx, toolsetID, validated, &provider, toolset)
	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream token refresh failed")
}

func TestRefreshProxyToken_GramProviderUnsupported(t *testing.T) {
	t.Parallel()

	provider := providers.NewGramProvider(newLogger(t), nil)
	_, err := provider.RefreshToken(
		context.Background(),
		"some-refresh-token",
		oauth_repo.OauthProxyProvider{},
		&toolsets_repo.Toolset{},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}

// Verify that ValidateAccessToken still shows updated secrets after a successful
// RefreshProxyToken (round-trip through cache).
func TestRefreshProxyToken_SecretsVisibleAfterRefresh(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newOAuthServiceTestEnv(t)
	toolsetID := uuid.New()

	upstreamExpiry := time.Now().Add(-1 * time.Minute)
	token, _, _ := env.issueToken(t, ctx, toolsetID,
		"old-access", "old-refresh", &upstreamExpiry, []string{"key"})
	rawAccess := token.AccessToken

	validated, _ := env.tokenService.ValidateAccessToken(ctx, toolsetID, rawAccess)

	upstream := fakeUpstream(t, 200, map[string]any{
		"access_token":  "refreshed-access",
		"refresh_token": "refreshed-refresh",
		"expires_in":    7200,
	})
	defer upstream.Close()

	provider := customProvider(upstream.URL + "/token")
	toolset := minimalToolset(toolsetID)

	_, err := env.service.RefreshProxyToken(ctx, toolsetID, validated, &provider, toolset)
	require.NoError(t, err)

	// Read back via the original raw access token
	reloaded, err := env.tokenService.ValidateAccessToken(ctx, toolsetID, rawAccess)
	require.NoError(t, err)
	require.Len(t, reloaded.ExternalSecrets, 1)
	require.Equal(t, "refreshed-access", reloaded.ExternalSecrets[0].Token)
	require.Equal(t, "refreshed-refresh", reloaded.ExternalSecrets[0].RefreshToken)

	fmt.Println("secrets round-trip verified")
}
