package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
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

// ExchangeToken exchanges an authorization code for an access token from a custom OAuth provider
func (p *CustomProvider) ExchangeToken(
	ctx context.Context,
	code string,
	provider repo.OauthProxyProvider,
	toolset *toolsets_repo.Toolset,
	serverURL *url.URL,
) (*TokenExchangeResult, error) {
	var secrets map[string]string
	if err := json.Unmarshal(provider.Secrets, &secrets); err != nil {
		p.logger.ErrorContext(ctx, "OAuth provider secrets invalid",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))
		return nil, fmt.Errorf("OAuth provider secrets invalid: %w", err)
	}

	clientID := secrets["client_id"]
	clientSecret := secrets["client_secret"]

	// Fallback to environment if credentials are missing and environment is specified
	if (clientID == "" || clientSecret == "") && secrets["environment_slug"] != "" {
		envMap, err := p.environments.Load(ctx, toolset.ProjectID, gateway.Slug(secrets["environment_slug"]))
		if err != nil {
			p.logger.ErrorContext(ctx, "failed to load environment",
				attr.SlogOAuthProvider(provider.Slug),
				attr.SlogError(err))
			return nil, fmt.Errorf("failed to load environment: %w", err)
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
		p.logger.ErrorContext(ctx, "OAuth provider client_id not configured",
			attr.SlogOAuthProvider(provider.Slug))
		return nil, fmt.Errorf("OAuth provider client_id not configured")
	}
	if clientSecret == "" {
		p.logger.ErrorContext(ctx, "OAuth provider client_secret not configured",
			attr.SlogOAuthProvider(provider.Slug))
		return nil, fmt.Errorf("OAuth provider client_secret not configured")
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
		for _, method := range provider.TokenEndpointAuthMethodsSupported {
			if method == "client_secret_basic" {
				useBasicAuth = true
				break
			}
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

	var oauthTokenResp map[string]interface{}
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

	return &TokenExchangeResult{
		AccessToken: accessToken,
		ExpiresAt:   expiresAt,
	}, nil
}
