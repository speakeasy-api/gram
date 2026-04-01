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

// oauthTokenResponseSnake is the spec-compliant (RFC 6749) token response format.
type oauthTokenResponseSnake struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresIn    float64 `json:"expires_in"`
}

// oauthTokenResponseCamel is a non-compliant camelCase variant that some providers return.
// TODO: Remove this fallback (and the double-deserialization in parseTokenResponse) once
// we've confirmed no providers use it. To check, query Datadog:
//
//	@msg:"non-compliant camelCase OAuth token response" service:gram-server
//
// If zero hits appear over a reasonable window (e.g. 30 days), it's safe to delete.
type oauthTokenResponseCamel struct {
	AccessToken  string  `json:"accessToken"`
	RefreshToken string  `json:"refreshToken"`
	ExpiresIn    float64 `json:"expiresIn"`
}

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

// parseTokenResponse deserializes a raw OAuth token response body into a TokenExchangeResult.
// It first tries the spec-compliant snake_case format, then falls back to camelCase for
// non-compliant providers. Returns an error only if neither format yields an access token.
func (p *CustomProvider) parseTokenResponse(ctx context.Context, body []byte, providerSlug string) (*TokenExchangeResult, error) {
	var snake oauthTokenResponseSnake
	if err := json.Unmarshal(body, &snake); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// Happy path: spec-compliant snake_case response
	if snake.AccessToken != "" {
		var expiresAt *time.Time
		if snake.ExpiresIn > 0 {
			expiryTime := time.Now().Add(time.Duration(snake.ExpiresIn) * time.Second)
			expiresAt = &expiryTime
		}
		return &TokenExchangeResult{
			AccessToken:  snake.AccessToken,
			RefreshToken: snake.RefreshToken,
			ExpiresAt:    expiresAt,
		}, nil
	}

	// Fallback: try camelCase for non-compliant providers
	var camel oauthTokenResponseCamel
	if err := json.Unmarshal(body, &camel); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if camel.AccessToken == "" {
		return nil, fmt.Errorf("missing access_token in token response")
	}

	// Log so we can track which providers use camelCase and eventually remove this fallback.
	p.logger.WarnContext(ctx, "non-compliant camelCase OAuth token response",
		attr.SlogOAuthProvider(providerSlug))

	var expiresAt *time.Time
	if camel.ExpiresIn > 0 {
		expiryTime := time.Now().Add(time.Duration(camel.ExpiresIn) * time.Second)
		expiresAt = &expiryTime
	}
	return &TokenExchangeResult{
		AccessToken:  camel.AccessToken,
		RefreshToken: camel.RefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// resolveClientCredentials extracts client_id and client_secret from provider secrets,
// falling back to environment variables if configured.
func (p *CustomProvider) resolveClientCredentials(ctx context.Context, provider repo.OauthProxyProvider, toolset *toolsets_repo.Toolset) (string, string, error) {
	var secrets map[string]string
	if err := json.Unmarshal(provider.Secrets, &secrets); err != nil {
		p.logger.ErrorContext(ctx, "OAuth provider secrets invalid",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))
		return "", "", fmt.Errorf("OAuth provider secrets invalid: %w", err)
	}

	clientID := secrets["client_id"]
	clientSecret := secrets["client_secret"]

	// Fallback to environment if credentials are missing and environment is specified
	if (clientID == "" || clientSecret == "") && secrets["environment_slug"] != "" {
		envMap, err := p.environments.Load(ctx, toolset.ProjectID, toolconfig.Slug(secrets["environment_slug"]))
		if err != nil {
			p.logger.ErrorContext(ctx, "failed to load environment",
				attr.SlogOAuthProvider(provider.Slug),
				attr.SlogError(err))
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
		p.logger.ErrorContext(ctx, "OAuth provider client_id not configured",
			attr.SlogOAuthProvider(provider.Slug))
		return "", "", fmt.Errorf("OAuth provider client_id not configured")
	}
	if clientSecret == "" {
		p.logger.ErrorContext(ctx, "OAuth provider client_secret not configured",
			attr.SlogOAuthProvider(provider.Slug))
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
	codeVerifier string,
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
	if codeVerifier != "" {
		tokenData.Set("code_verifier", codeVerifier)
	}

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
	req.Header.Set("Accept", "application/json")
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
		errBody, _ := io.ReadAll(tokenResp.Body)
		p.logger.ErrorContext(ctx, "OAuth token exchange failed",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogHTTPResponseStatusCode(tokenResp.StatusCode),
			attr.SlogHTTPResponseBody(string(errBody)))
		return nil, fmt.Errorf("token exchange failed with status %d", tokenResp.StatusCode)
	}

	tokenRespBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to read OAuth token response",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))
		return nil, fmt.Errorf("failed to read OAuth token response: %w", err)
	}

	result, err := p.parseTokenResponse(ctx, tokenRespBody, provider.Slug)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to parse OAuth token response",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))
		return nil, err
	}

	return result, nil
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
	req.Header.Set("Accept", "application/json")
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
		errBody, _ := io.ReadAll(tokenResp.Body)
		p.logger.ErrorContext(ctx, "OAuth token refresh failed",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogHTTPResponseStatusCode(tokenResp.StatusCode),
			attr.SlogHTTPResponseBody(string(errBody)))
		return nil, fmt.Errorf("token refresh failed with status %d", tokenResp.StatusCode)
	}

	tokenRespBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read refresh token response: %w", err)
	}

	result, err := p.parseTokenResponse(ctx, tokenRespBody, provider.Slug)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to parse refresh token response",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))
		return nil, err
	}

	return result, nil
}
