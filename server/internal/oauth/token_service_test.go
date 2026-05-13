package oauth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
)

func TestExchangeRefreshToken_Success(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTokenTestEnv(t)
	toolsetID := uuid.New()
	upstreamExpiry := time.Now().Add(24 * time.Hour)

	issued := env.IssueToken(t, ctx, toolsetID,
		"upstream-access", "upstream-refresh", &upstreamExpiry, []string{"api_key"})

	oldAccess := issued.AccessToken
	oldRefresh := issued.RefreshToken

	newToken, err := env.TokenService.ExchangeRefreshToken(ctx, &oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: oldRefresh,
		ClientID:     issued.ClientID,
		ClientSecret: issued.ClientSecret,
	}, oauthtest.TestMCPURL, toolsetID)
	require.NoError(t, err)

	require.NotEmpty(t, newToken.AccessToken)
	require.NotEmpty(t, newToken.RefreshToken)
	require.NotEqual(t, oldAccess, newToken.AccessToken)
	require.NotEqual(t, oldRefresh, newToken.RefreshToken)
	require.Equal(t, "Bearer", newToken.TokenType)
}

func TestExchangeRefreshToken_RotatesTokens(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTokenTestEnv(t)
	toolsetID := uuid.New()
	upstreamExpiry := time.Now().Add(24 * time.Hour)

	issued := env.IssueToken(t, ctx, toolsetID,
		"upstream-access", "upstream-refresh", &upstreamExpiry, []string{"api_key"})

	oldAccess := issued.AccessToken
	oldRefresh := issued.RefreshToken

	// Refresh once
	_, err := env.TokenService.ExchangeRefreshToken(ctx, &oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: oldRefresh,
		ClientID:     issued.ClientID,
		ClientSecret: issued.ClientSecret,
	}, oauthtest.TestMCPURL, toolsetID)
	require.NoError(t, err)

	// Old access token should be invalid
	_, err = env.TokenService.ValidateAccessToken(ctx, toolsetID, oldAccess)
	require.ErrorIs(t, err, oauth.ErrInvalidAccessToken)

	// Old refresh token should be invalid
	_, err = env.TokenService.ExchangeRefreshToken(ctx, &oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: oldRefresh,
		ClientID:     issued.ClientID,
		ClientSecret: issued.ClientSecret,
	}, oauthtest.TestMCPURL, toolsetID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid refresh token")
}

func TestExchangeRefreshToken_PreservesExternalSecrets(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTokenTestEnv(t)
	toolsetID := uuid.New()
	upstreamExpiry := time.Now().Add(24 * time.Hour)

	issued := env.IssueToken(t, ctx, toolsetID,
		"upstream-access-token", "upstream-refresh-token", &upstreamExpiry, []string{"Authorization"})

	newToken, err := env.TokenService.ExchangeRefreshToken(ctx, &oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: issued.RefreshToken,
		ClientID:     issued.ClientID,
		ClientSecret: issued.ClientSecret,
	}, oauthtest.TestMCPURL, toolsetID)
	require.NoError(t, err)

	require.Len(t, newToken.ExternalSecrets, 1)
	require.Equal(t, "upstream-access-token", newToken.ExternalSecrets[0].Token)
	require.Equal(t, "upstream-refresh-token", newToken.ExternalSecrets[0].RefreshToken)
	require.Equal(t, []string{"Authorization"}, newToken.ExternalSecrets[0].SecurityKeys)
}

func TestExchangeRefreshToken_MissingClientID(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTokenTestEnv(t)
	toolsetID := uuid.New()

	_, err := env.TokenService.ExchangeRefreshToken(ctx, &oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: "some-token",
		ClientID:     "",
	}, oauthtest.TestMCPURL, toolsetID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "client_id is required")
}

func TestExchangeRefreshToken_InvalidRefreshToken(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTokenTestEnv(t)
	toolsetID := uuid.New()

	// Register a client so credential validation passes
	client, err := env.ClientReg.RegisterClient(ctx, &oauth.ClientInfo{
		ClientName:   "test-client",
		RedirectURIs: []string{oauthtest.TestRedirectURI},
		GrantTypes:   []string{"authorization_code", "refresh_token"},
	}, oauthtest.TestMCPURL)
	require.NoError(t, err)

	_, err = env.TokenService.ExchangeRefreshToken(ctx, &oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: "nonexistent-refresh-token",
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
	}, oauthtest.TestMCPURL, toolsetID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid refresh token")
}

func TestExchangeRefreshToken_ExpiredDownstreamToken(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTokenTestEnv(t)
	toolsetID := uuid.New()
	upstreamExpiry := time.Now().Add(365 * 24 * time.Hour)

	issued := env.IssueToken(t, ctx, toolsetID,
		"upstream-access", "upstream-refresh", &upstreamExpiry, []string{"api_key"})

	// Directly expire the token in cache to simulate 31 days passing
	env.ExpireToken(t, ctx, toolsetID, issued.AccessToken, time.Now().Add(-1*time.Hour))

	_, err := env.TokenService.ValidateAccessToken(ctx, toolsetID, issued.AccessToken)
	require.ErrorIs(t, err, oauth.ErrExpiredAccessToken)

	// Refresh token should still work (24h grace period on cache TTL)
	newToken, err := env.TokenService.ExchangeRefreshToken(ctx, &oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: issued.RefreshToken,
		ClientID:     issued.ClientID,
		ClientSecret: issued.ClientSecret,
	}, oauthtest.TestMCPURL, toolsetID)
	require.NoError(t, err)
	require.NotEqual(t, issued.AccessToken, newToken.AccessToken)
}

func TestExchangeRefreshToken_ExpiredUpstreamSecrets(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTokenTestEnv(t)
	toolsetID := uuid.New()

	// Issue token with an upstream secret that expires in 200ms
	upstreamExpiry := time.Now().Add(200 * time.Millisecond)
	issued := env.IssueToken(t, ctx, toolsetID,
		"upstream-access", "upstream-refresh", &upstreamExpiry, []string{"api_key"})

	// Wait for upstream secret to expire (downstream token is still valid - 30 day TTL)
	time.Sleep(300 * time.Millisecond)

	_, err := env.TokenService.ExchangeRefreshToken(ctx, &oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: issued.RefreshToken,
		ClientID:     issued.ClientID,
		ClientSecret: issued.ClientSecret,
	}, oauthtest.TestMCPURL, toolsetID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "underlying credentials have expired")
}

func TestCreateTokenResponse_IncludesRefreshToken(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTokenTestEnv(t)
	toolsetID := uuid.New()
	upstreamExpiry := time.Now().Add(24 * time.Hour)

	issued := env.IssueToken(t, ctx, toolsetID,
		"upstream-access", "upstream-refresh", &upstreamExpiry, []string{"api_key"})

	resp := env.TokenService.CreateTokenResponse(issued.Token)

	require.NotEmpty(t, resp.RefreshToken)
	require.Equal(t, issued.RefreshToken, resp.RefreshToken)
	require.Equal(t, issued.AccessToken, resp.AccessToken)
	require.Equal(t, "Bearer", resp.TokenType)
	require.Positive(t, resp.ExpiresIn)
}

func TestValidateAccessToken_ReturnsTokenOnExpiredExternalSecrets(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTokenTestEnv(t)
	toolsetID := uuid.New()

	// Issue token with an already-expired upstream secret
	pastTime := time.Now().Add(-1 * time.Hour)
	issued := env.IssueToken(t, ctx, toolsetID,
		"upstream-access", "upstream-refresh", &pastTime, []string{"api_key"})

	got, err := env.TokenService.ValidateAccessToken(ctx, toolsetID, issued.AccessToken)
	require.ErrorIs(t, err, oauth.ErrExpiredExternalSecrets)
	require.NotNil(t, got, "token should be returned alongside expired-secrets error")
	require.Equal(t, toolsetID, got.ToolsetID)
}

func TestValidateAccessToken_NilExpiresAt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTokenTestEnv(t)
	toolsetID := uuid.New()

	// Issue token with nil upstream ExpiresAt — should not trigger expiration
	issued := env.IssueToken(t, ctx, toolsetID,
		"upstream-access", "upstream-refresh", nil, []string{"api_key"})

	got, err := env.TokenService.ValidateAccessToken(ctx, toolsetID, issued.AccessToken)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.ExternalSecrets, 1)
	require.Nil(t, got.ExternalSecrets[0].ExpiresAt)
}
