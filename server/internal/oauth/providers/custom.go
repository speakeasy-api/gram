package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// CustomProvider implements OAuth provider for custom OAuth providers
type CustomProvider struct {
	logger       *slog.Logger
	environments *environments.EnvironmentEntries
}

// NewCustomProvider creates a new custom OAuth provider
func NewCustomProvider(logger *slog.Logger, environments *environments.EnvironmentEntries) *CustomProvider {
	return &CustomProvider{
		logger:       logger,
		environments: environments,
	}
}

// resolveClientCredentials extracts client_id and client_secret from provider secrets,
// falling back to environment variables if configured.
func (p *CustomProvider) resolveClientCredentials(ctx context.Context, provider repo.OauthProxyProvider, toolset *toolsets_repo.Toolset) (string, string, error) {
	var secrets map[string]string
	if err := json.Unmarshal(provider.Secrets, &secrets); err != nil {
		return "", "", fmt.Errorf("OAuth provider secrets invalid: %w", err)
	}

	clientID := secrets["client_id"]
	clientSecret := secrets["client_secret"]

	if (clientID == "" || clientSecret == "") && secrets["environment_slug"] != "" {
		envMap, err := p.environments.Load(ctx, toolset.ProjectID, toolconfig.Slug(secrets["environment_slug"]))
		if err != nil {
			return "", "", fmt.Errorf("failed to load environment: %w", err)
		}

		for k, v := range envMap {
			if clientID == "" && strings.ToLower(k) == "client_id" {
				clientID = v
			}
			if clientSecret == "" && strings.ToLower(k) == "client_secret" {
				clientSecret = v
			}
		}
	}

	if clientID == "" {
		return "", "", fmt.Errorf("OAuth provider client_id not configured")
	}
	if clientSecret == "" {
		return "", "", fmt.Errorf("OAuth provider client_secret not configured")
	}

	return clientID, clientSecret, nil
}

// ExchangeToken exchanges an authorization code for an access token from a custom OAuth provider
func (p *CustomProvider) ExchangeToken(
	ctx context.Context,
	code string,
	provider repo.OauthProxyProvider,
	toolset *toolsets_repo.Toolset,
	serverURL *url.URL,
) (*TokenExchangeResult, error) {
	clientID, clientSecret, err := p.resolveClientCredentials(ctx, provider, toolset)
	if err != nil {
		return nil, err
	}

	callbackURL := fmt.Sprintf("%s/oauth/callback", serverURL.String())

	tokenURL := provider.TokenEndpoint
	tokenData := url.Values{}
	tokenData.Set("grant_type", "authorization_code")
	tokenData.Set("redirect_uri", callbackURL)
	tokenData.Set("code", code)

	// Determine authentication method based on provider configuration
	// Default to client_secret_post (form body) if TokenEndpointAuthMethodsSupported is empty
	useBasicAuth := false
	if len(provider.TokenEndpointAuthMethodsSupported) > 0 {
		// Check if provider supports client_secret_basic
		if slices.Contains(provider.TokenEndpointAuthMethodsSupported, "client_secret_basic") {
			useBasicAuth = true
		}
	}

	// For Post Auth, client credentials go in form body
	if !useBasicAuth {
		tokenData.Set("client_id", clientID)
		tokenData.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL.String, strings.NewReader(tokenData.Encode()))
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to create token request",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if useBasicAuth {
		req.SetBasicAuth(clientID, clientSecret)
	}

	tokenResp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to exchange code for token",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))
		return nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}
	defer func() {
		if err := tokenResp.Body.Close(); err != nil {
			p.logger.ErrorContext(ctx, "failed to close response body", attr.SlogError(err))
		}
	}()

	if tokenResp.StatusCode != http.StatusOK {
		p.logger.ErrorContext(ctx, "OAuth token exchange failed",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogHTTPResponseStatusCode(tokenResp.StatusCode))
		return nil, fmt.Errorf("token exchange failed with status %d", tokenResp.StatusCode)
	}

	tokenRespBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to read OAuth token response",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))
		return nil, fmt.Errorf("failed to read OAuth token response: %w", err)
	}

	var oauthTokenResp map[string]any
	if err := json.Unmarshal(tokenRespBody, &oauthTokenResp); err != nil {
		p.logger.ErrorContext(ctx, "failed to parse OAuth token response",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))
		return nil, fmt.Errorf("failed to parse OAuth token response: %w", err)
	}

	// Technically the OAuth spec does expect snake_case field names in the response
	// but we will be generous to mistakes and try with camelCase
	accessToken, ok := oauthTokenResp["access_token"].(string)
	if !ok {
		// Retry with camelCase field name
		accessToken, ok = oauthTokenResp["accessToken"].(string)
		if !ok {
			p.logger.ErrorContext(ctx, "missing access_token in OAuth response",
				attr.SlogOAuthProvider(provider.Slug))
			return nil, fmt.Errorf("missing access_token in OAuth response")
		}
	}

	var expiresAt *time.Time
	if expiresInFloat, ok := oauthTokenResp["expires_in"].(float64); ok {
		expiryTime := time.Now().Add(time.Duration(expiresInFloat) * time.Second)
		expiresAt = &expiryTime
	}
	if expiresAt == nil {
		if expiresInFloat, ok := oauthTokenResp["expiresIn"].(float64); ok {
			// Retry with camelCase field name
			expiryTime := time.Now().Add(time.Duration(expiresInFloat) * time.Second)
			expiresAt = &expiryTime
		}
	}

	var refreshToken string
	if rt, ok := oauthTokenResp["refresh_token"].(string); ok {
		refreshToken = rt
	} else if rt, ok := oauthTokenResp["refreshToken"].(string); ok {
		refreshToken = rt
	}

	return &TokenExchangeResult{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// RefreshToken exchanges a refresh token for a new access token from a custom OAuth provider
func (p *CustomProvider) RefreshToken(
	ctx context.Context,
	refreshToken string,
	provider repo.OauthProxyProvider,
	toolset *toolsets_repo.Toolset,
) (*TokenExchangeResult, error) {
	clientID, clientSecret, err := p.resolveClientCredentials(ctx, provider, toolset)
	if err != nil {
		return nil, err
	}

	tokenURL := provider.TokenEndpoint
	tokenData := url.Values{}
	tokenData.Set("grant_type", "refresh_token")
	tokenData.Set("refresh_token", refreshToken)

	// Determine authentication method
	useBasicAuth := false
	if len(provider.TokenEndpointAuthMethodsSupported) > 0 {
		if slices.Contains(provider.TokenEndpointAuthMethodsSupported, "client_secret_basic") {
			useBasicAuth = true
		}
	}

	if !useBasicAuth {
		tokenData.Set("client_id", clientID)
		tokenData.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL.String, strings.NewReader(tokenData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if useBasicAuth {
		req.SetBasicAuth(clientID, clientSecret)
	}

	tokenResp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	defer func() {
		if err := tokenResp.Body.Close(); err != nil {
			p.logger.ErrorContext(ctx, "failed to close response body", attr.SlogError(err))
		}
	}()

	if tokenResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed with status %d", tokenResp.StatusCode)
	}

	tokenRespBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read refresh token response: %w", err)
	}

	var oauthTokenResp map[string]any
	if err := json.Unmarshal(tokenRespBody, &oauthTokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse refresh token response: %w", err)
	}

	accessToken, ok := oauthTokenResp["access_token"].(string)
	if !ok {
		accessToken, ok = oauthTokenResp["accessToken"].(string)
		if !ok {
			return nil, fmt.Errorf("missing access_token in refresh response")
		}
	}

	var newRefreshToken string
	if rt, ok := oauthTokenResp["refresh_token"].(string); ok {
		newRefreshToken = rt
	} else if rt, ok := oauthTokenResp["refreshToken"].(string); ok {
		newRefreshToken = rt
	}

	var expiresAt *time.Time
	if expiresInFloat, ok := oauthTokenResp["expires_in"].(float64); ok {
		expiryTime := time.Now().Add(time.Duration(expiresInFloat) * time.Second)
		expiresAt = &expiryTime
	}
	if expiresAt == nil {
		if expiresInFloat, ok := oauthTokenResp["expiresIn"].(float64); ok {
			expiryTime := time.Now().Add(time.Duration(expiresInFloat) * time.Second)
			expiresAt = &expiryTime
		}
	}

	return &TokenExchangeResult{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}
