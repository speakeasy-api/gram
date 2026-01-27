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
	"github.com/speakeasy-api/gram/server/internal/encryption"
)

// ExternalOAuthSecrets represents decrypted client credentials for external OAuth servers.
type ExternalOAuthSecrets struct {
	ClientID        string `json:"client_id"`
	ClientSecret    string `json:"client_secret"`
	EnvironmentSlug string `json:"environment_slug,omitempty"`
}

// ExternalTokenExchangeParams contains the parameters for exchanging tokens with external OAuth servers.
type ExternalTokenExchangeParams struct {
	Code          string
	TokenEndpoint string
	ClientID      string
	ClientSecret  string
	RedirectURI   string
	AuthMethods   []string // token_endpoint_auth_methods_supported
}

// ExternalOAuthProvider handles token exchange for external OAuth servers
// where client credentials are manually configured.
type ExternalOAuthProvider struct {
	logger *slog.Logger
	enc    *encryption.Client
}

// NewExternalOAuthProvider creates a new external OAuth provider.
func NewExternalOAuthProvider(logger *slog.Logger, enc *encryption.Client) *ExternalOAuthProvider {
	return &ExternalOAuthProvider{
		logger: logger,
		enc:    enc,
	}
}

// DecryptSecrets decrypts and parses external OAuth secrets.
func (p *ExternalOAuthProvider) DecryptSecrets(encryptedSecrets []byte) (*ExternalOAuthSecrets, error) {
	if len(encryptedSecrets) == 0 {
		return nil, fmt.Errorf("no secrets configured")
	}

	decrypted, err := p.enc.Decrypt(string(encryptedSecrets))
	if err != nil {
		return nil, fmt.Errorf("decrypt secrets: %w", err)
	}

	var secrets ExternalOAuthSecrets
	if err := json.Unmarshal([]byte(decrypted), &secrets); err != nil {
		return nil, fmt.Errorf("unmarshal secrets: %w", err)
	}

	return &secrets, nil
}

// EncryptSecrets encrypts external OAuth secrets for storage.
func (p *ExternalOAuthProvider) EncryptSecrets(secrets *ExternalOAuthSecrets) ([]byte, error) {
	secretsJSON, err := json.Marshal(secrets)
	if err != nil {
		return nil, fmt.Errorf("marshal secrets: %w", err)
	}

	encrypted, err := p.enc.Encrypt(secretsJSON)
	if err != nil {
		return nil, fmt.Errorf("encrypt secrets: %w", err)
	}

	return []byte(encrypted), nil
}

// ExchangeToken exchanges an authorization code for an access token from an external OAuth server.
func (p *ExternalOAuthProvider) ExchangeToken(ctx context.Context, params ExternalTokenExchangeParams) (*TokenExchangeResult, error) {
	if params.TokenEndpoint == "" {
		return nil, fmt.Errorf("token endpoint is required")
	}
	if params.ClientID == "" {
		return nil, fmt.Errorf("client_id is required")
	}

	tokenData := url.Values{}
	tokenData.Set("grant_type", "authorization_code")
	tokenData.Set("code", params.Code)
	tokenData.Set("redirect_uri", params.RedirectURI)

	// Determine authentication method
	// Default to client_secret_post if not specified or empty
	useBasicAuth := false
	for _, method := range params.AuthMethods {
		if method == "client_secret_basic" {
			useBasicAuth = true
			break
		}
	}

	// For POST auth, client credentials go in form body
	if !useBasicAuth {
		tokenData.Set("client_id", params.ClientID)
		if params.ClientSecret != "" {
			tokenData.Set("client_secret", params.ClientSecret)
		}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", params.TokenEndpoint, strings.NewReader(tokenData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	if useBasicAuth {
		req.SetBasicAuth(params.ClientID, params.ClientSecret)
	}

	p.logger.InfoContext(ctx, "exchanging authorization code with external OAuth server",
		attr.SlogURL(params.TokenEndpoint))

	tokenResp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer func() {
		if closeErr := tokenResp.Body.Close(); closeErr != nil {
			p.logger.ErrorContext(ctx, "failed to close response body", attr.SlogError(closeErr))
		}
	}()

	tokenRespBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if tokenResp.StatusCode != http.StatusOK {
		p.logger.ErrorContext(ctx, "OAuth token exchange failed",
			attr.SlogHTTPResponseStatusCode(tokenResp.StatusCode))
		return nil, fmt.Errorf("token exchange failed with status %d: %s", tokenResp.StatusCode, string(tokenRespBody))
	}

	var oauthResp map[string]interface{}
	if err := json.Unmarshal(tokenRespBody, &oauthResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	// Extract access_token (try both snake_case and camelCase)
	accessToken, ok := oauthResp["access_token"].(string)
	if !ok {
		accessToken, ok = oauthResp["accessToken"].(string)
		if !ok {
			return nil, fmt.Errorf("missing access_token in OAuth response")
		}
	}

	// Extract refresh_token if present
	refreshToken, _ := oauthResp["refresh_token"].(string)
	if refreshToken == "" {
		refreshToken, _ = oauthResp["refreshToken"].(string)
	}

	// Extract token_type if present
	tokenType, _ := oauthResp["token_type"].(string)
	if tokenType == "" {
		tokenType, _ = oauthResp["tokenType"].(string)
	}
	if tokenType == "" {
		tokenType = "Bearer"
	}

	// Extract scope if present
	scope, _ := oauthResp["scope"].(string)

	// Calculate expiration time
	var expiresAt *time.Time
	if expiresInFloat, ok := oauthResp["expires_in"].(float64); ok && expiresInFloat > 0 {
		expiryTime := time.Now().Add(time.Duration(expiresInFloat) * time.Second)
		expiresAt = &expiryTime
	}
	if expiresAt == nil {
		if expiresInFloat, ok := oauthResp["expiresIn"].(float64); ok && expiresInFloat > 0 {
			expiryTime := time.Now().Add(time.Duration(expiresInFloat) * time.Second)
			expiresAt = &expiryTime
		}
	}

	p.logger.InfoContext(ctx, "successfully exchanged authorization code")

	return &TokenExchangeResult{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		Scope:        scope,
		ExpiresAt:    expiresAt,
	}, nil
}

// MCPDynamicRegistrationParams contains parameters for dynamic client registration.
type MCPDynamicRegistrationParams struct {
	RegistrationEndpoint string
	ClientName           string
	RedirectURIs         []string
	TokenEndpointAuth    string // token_endpoint_auth_method
	GrantTypes           []string
	ResponseTypes        []string
}

// DynamicClientRegistration contains the result of a dynamic client registration.
type DynamicClientRegistration struct {
	ClientID                string
	ClientSecret            string
	ClientIDExpiresAt       *time.Time
	RegistrationAccessToken string
	RegistrationClientURI   string
	TokenEndpointAuthMethod string
	GrantTypes              []string
	ResponseTypes           []string
}

// MCPOAuthProvider handles dynamic client registration and token exchange
// for external MCP servers that support RFC 7591.
type MCPOAuthProvider struct {
	logger *slog.Logger
	enc    *encryption.Client
}

// NewMCPOAuthProvider creates a new MCP OAuth provider.
func NewMCPOAuthProvider(logger *slog.Logger, enc *encryption.Client) *MCPOAuthProvider {
	return &MCPOAuthProvider{
		logger: logger,
		enc:    enc,
	}
}

// RegisterClient performs RFC 7591 dynamic client registration.
func (p *MCPOAuthProvider) RegisterClient(ctx context.Context, params MCPDynamicRegistrationParams) (*DynamicClientRegistration, error) {
	if params.RegistrationEndpoint == "" {
		return nil, fmt.Errorf("registration endpoint is required")
	}

	// Build registration request per RFC 7591
	regRequest := map[string]interface{}{
		"client_name":   params.ClientName,
		"redirect_uris": params.RedirectURIs,
	}

	if params.TokenEndpointAuth != "" {
		regRequest["token_endpoint_auth_method"] = params.TokenEndpointAuth
	} else {
		// Default to client_secret_post
		regRequest["token_endpoint_auth_method"] = "client_secret_post"
	}

	if len(params.GrantTypes) > 0 {
		regRequest["grant_types"] = params.GrantTypes
	} else {
		regRequest["grant_types"] = []string{"authorization_code", "refresh_token"}
	}

	if len(params.ResponseTypes) > 0 {
		regRequest["response_types"] = params.ResponseTypes
	} else {
		regRequest["response_types"] = []string{"code"}
	}

	reqBody, err := json.Marshal(regRequest)
	if err != nil {
		return nil, fmt.Errorf("marshal registration request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", params.RegistrationEndpoint, strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, fmt.Errorf("create registration request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	p.logger.InfoContext(ctx, "performing dynamic client registration",
		attr.SlogURL(params.RegistrationEndpoint))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registration request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			p.logger.ErrorContext(ctx, "failed to close response body", attr.SlogError(closeErr))
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read registration response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		p.logger.ErrorContext(ctx, "dynamic client registration failed",
			attr.SlogHTTPResponseStatusCode(resp.StatusCode))
		return nil, fmt.Errorf("registration failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var regResp map[string]interface{}
	if err := json.Unmarshal(respBody, &regResp); err != nil {
		return nil, fmt.Errorf("parse registration response: %w", err)
	}

	clientID, ok := regResp["client_id"].(string)
	if !ok || clientID == "" {
		return nil, fmt.Errorf("missing client_id in registration response")
	}

	result := &DynamicClientRegistration{
		ClientID:                clientID,
		ClientSecret:            "",
		ClientIDExpiresAt:       nil,
		RegistrationAccessToken: "",
		RegistrationClientURI:   "",
		TokenEndpointAuthMethod: "",
		GrantTypes:              nil,
		ResponseTypes:           nil,
	}

	// Optional fields
	if clientSecret, ok := regResp["client_secret"].(string); ok {
		result.ClientSecret = clientSecret
	}

	if expiresAt, ok := regResp["client_id_expires_at"].(float64); ok && expiresAt > 0 {
		t := time.Unix(int64(expiresAt), 0)
		result.ClientIDExpiresAt = &t
	}

	if rat, ok := regResp["registration_access_token"].(string); ok {
		result.RegistrationAccessToken = rat
	}

	if uri, ok := regResp["registration_client_uri"].(string); ok {
		result.RegistrationClientURI = uri
	}

	if method, ok := regResp["token_endpoint_auth_method"].(string); ok {
		result.TokenEndpointAuthMethod = method
	}

	p.logger.InfoContext(ctx, "successfully registered dynamic client")

	return result, nil
}

// ExchangeToken exchanges an authorization code using dynamically registered credentials.
func (p *MCPOAuthProvider) ExchangeToken(ctx context.Context, params ExternalTokenExchangeParams) (*TokenExchangeResult, error) {
	// Delegate to the standard external provider logic
	extProvider := &ExternalOAuthProvider{
		logger: p.logger,
		enc:    p.enc,
	}
	return extProvider.ExchangeToken(ctx, params)
}
