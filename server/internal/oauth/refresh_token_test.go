package oauth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedTokenData creates all the DB rows needed for a user_oauth_tokens entry
// (toolset + client registration + token) and returns the created token.
func seedTokenData(
	t *testing.T,
	ti *testInstance,
	ctx context.Context,
	tokenEndpoint string,
	accessToken string,
	refreshToken string,
	expiresAt time.Time,
) oauth_repo.UserOauthToken {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	oauthRepo := oauth_repo.New(ti.conn)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	// Create toolset
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "OAuth Test Toolset",
		Slug:                   "oauth-test-toolset-" + t.Name(),
		Description:            conv.ToPGText("test toolset"),
		DefaultEnvironmentSlug: pgtype.Text{Valid: false},
		McpSlug:                conv.ToPGText("oauth-test-" + t.Name()),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	// Create client registration
	clientReg, err := oauthRepo.UpsertExternalOAuthClientRegistration(ctx, oauth_repo.UpsertExternalOAuthClientRegistrationParams{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		OauthServerIssuer:     "https://auth.example.com",
		ClientID:              "test-client-id",
		ClientSecretEncrypted: pgtype.Text{Valid: false},
		ClientIDIssuedAt:      pgtype.Timestamptz{Valid: false},
		ClientSecretExpiresAt: pgtype.Timestamptz{Valid: false},
	})
	require.NoError(t, err)

	// Encrypt tokens
	accessTokenEnc, err := ti.enc.Encrypt([]byte(accessToken))
	require.NoError(t, err)

	var refreshTokenEnc pgtype.Text
	if refreshToken != "" {
		encrypted, encErr := ti.enc.Encrypt([]byte(refreshToken))
		require.NoError(t, encErr)
		refreshTokenEnc = conv.ToPGText(encrypted)
	}

	// Create token
	token, err := oauthRepo.UpsertUserOAuthToken(ctx, oauth_repo.UpsertUserOAuthTokenParams{
		UserID:               authCtx.UserID,
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            *authCtx.ProjectID,
		ClientRegistrationID: clientReg.ID,
		ToolsetID:            toolset.ID,
		OauthServerIssuer:    "https://auth.example.com",
		AccessTokenEncrypted: accessTokenEnc,
		RefreshTokenEncrypted: refreshTokenEnc,
		TokenType:            conv.ToPGText("bearer"),
		ExpiresAt: pgtype.Timestamptz{
			Time:             expiresAt,
			Valid:            true,
			InfinityModifier: pgtype.Finite,
		},
		Scopes:       []string{"read", "write"},
		ProviderName: conv.ToPGText("TestProvider"),
	})
	require.NoError(t, err)

	return token
}

func TestRefreshUpstreamToken_Success(t *testing.T) {
	t.Parallel()

	// Set up mock upstream token endpoint
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		err := r.ParseForm()
		require.NoError(t, err)
		require.Equal(t, "refresh_token", r.FormValue("grant_type"))
		require.Equal(t, "test-client-id", r.FormValue("client_id"))
		require.NotEmpty(t, r.FormValue("refresh_token"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer upstream.Close()

	ctx, ti := newTestExternalOAuthService(t)

	token := seedTokenData(t, ti, ctx, upstream.URL, "old-access-token", "old-refresh-token", time.Now().Add(-1*time.Hour))

	config := &oauth.ExternalOAuthConfig{
		Issuer:                "https://auth.example.com",
		TokenEndpoint:         upstream.URL,
		ClientID:              "test-client-id",
		ClientSecret:          "",
		AuthorizationEndpoint: "",
		RegistrationEndpoint:  "",
		ScopesSupported:       nil,
		ProviderName:          "TestProvider",
	}

	newAccessToken, err := ti.service.RefreshUpstreamToken(ctx, token, config)
	require.NoError(t, err)
	require.Equal(t, "new-access-token", newAccessToken)

	// Verify the token was updated in the DB
	oauthRepo := oauth_repo.New(ti.conn)
	updatedToken, err := oauthRepo.GetUserOAuthToken(ctx, oauth_repo.GetUserOAuthTokenParams{
		UserID:         token.UserID,
		OrganizationID: token.OrganizationID,
		ToolsetID:      token.ToolsetID,
	})
	require.NoError(t, err)

	// Decrypt and verify the new access token was stored
	decryptedAccess, err := ti.enc.Decrypt(updatedToken.AccessTokenEncrypted)
	require.NoError(t, err)
	require.Equal(t, "new-access-token", decryptedAccess)

	// The refresh token should still be the old one (not rotated)
	require.True(t, updatedToken.RefreshTokenEncrypted.Valid)
	decryptedRefresh, err := ti.enc.Decrypt(updatedToken.RefreshTokenEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, "old-refresh-token", decryptedRefresh)

	// Verify expiry was updated
	require.True(t, updatedToken.ExpiresAt.Valid)
	require.True(t, updatedToken.ExpiresAt.Time.After(time.Now()))
}

func TestRefreshUpstreamToken_TokenRotation(t *testing.T) {
	t.Parallel()

	// Mock upstream that returns a new refresh token (token rotation)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "rotated-access-token",
			"refresh_token": "rotated-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    7200,
		})
	}))
	defer upstream.Close()

	ctx, ti := newTestExternalOAuthService(t)

	token := seedTokenData(t, ti, ctx, upstream.URL, "old-access", "old-refresh", time.Now().Add(-1*time.Hour))

	config := &oauth.ExternalOAuthConfig{
		Issuer:                "https://auth.example.com",
		TokenEndpoint:         upstream.URL,
		ClientID:              "test-client-id",
		ClientSecret:          "",
		AuthorizationEndpoint: "",
		RegistrationEndpoint:  "",
		ScopesSupported:       nil,
		ProviderName:          "TestProvider",
	}

	newAccessToken, err := ti.service.RefreshUpstreamToken(ctx, token, config)
	require.NoError(t, err)
	require.Equal(t, "rotated-access-token", newAccessToken)

	// Verify the rotated refresh token was stored
	oauthRepo := oauth_repo.New(ti.conn)
	updatedToken, err := oauthRepo.GetUserOAuthToken(ctx, oauth_repo.GetUserOAuthTokenParams{
		UserID:         token.UserID,
		OrganizationID: token.OrganizationID,
		ToolsetID:      token.ToolsetID,
	})
	require.NoError(t, err)

	decryptedRefresh, err := ti.enc.Decrypt(updatedToken.RefreshTokenEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, "rotated-refresh-token", decryptedRefresh)
}

func TestRefreshUpstreamToken_UpstreamRejects(t *testing.T) {
	t.Parallel()

	// Mock upstream that rejects the refresh token (e.g., revoked)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "The refresh token has been revoked",
		})
	}))
	defer upstream.Close()

	ctx, ti := newTestExternalOAuthService(t)

	token := seedTokenData(t, ti, ctx, upstream.URL, "old-access", "revoked-refresh", time.Now().Add(-1*time.Hour))

	config := &oauth.ExternalOAuthConfig{
		Issuer:                "https://auth.example.com",
		TokenEndpoint:         upstream.URL,
		ClientID:              "test-client-id",
		ClientSecret:          "",
		AuthorizationEndpoint: "",
		RegistrationEndpoint:  "",
		ScopesSupported:       nil,
		ProviderName:          "TestProvider",
	}

	_, err := ti.service.RefreshUpstreamToken(ctx, token, config)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "exchange refresh token"))

	// Verify the stale token was soft-deleted
	oauthRepo := oauth_repo.New(ti.conn)
	_, err = oauthRepo.GetUserOAuthToken(ctx, oauth_repo.GetUserOAuthTokenParams{
		UserID:         token.UserID,
		OrganizationID: token.OrganizationID,
		ToolsetID:      token.ToolsetID,
	})
	// Token should no longer be found (soft-deleted)
	require.Error(t, err)
}

func TestRefreshUpstreamToken_NoRefreshToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalOAuthService(t)

	// Seed a token without a refresh token
	token := seedTokenData(t, ti, ctx, "http://unused", "old-access", "", time.Now().Add(-1*time.Hour))

	config := &oauth.ExternalOAuthConfig{
		Issuer:                "https://auth.example.com",
		TokenEndpoint:         "http://unused",
		ClientID:              "test-client-id",
		ClientSecret:          "",
		AuthorizationEndpoint: "",
		RegistrationEndpoint:  "",
		ScopesSupported:       nil,
		ProviderName:          "TestProvider",
	}

	_, err := ti.service.RefreshUpstreamToken(ctx, token, config)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "no refresh token available"))
}
