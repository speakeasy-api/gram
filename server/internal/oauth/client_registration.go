package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

// ClientRegistrationService handles OAuth Dynamic Client Registration
type ClientRegistrationService struct {
	logger            *slog.Logger
	clientInfoStorage cache.TypedCacheObject[OauthProxyClientInfo]
}

func NewClientRegistrationService(cacheImpl cache.Cache, logger *slog.Logger) *ClientRegistrationService {
	return &ClientRegistrationService{
		logger:            logger,
		clientInfoStorage: cache.NewTypedObjectCache[OauthProxyClientInfo](logger.With(attr.SlogCacheNamespace("oauth_client_info")), cacheImpl, cache.SuffixNone),
	}
}

// RegisterClient implements RFC 7591 Dynamic Client Registration
func (s *ClientRegistrationService) RegisterClient(ctx context.Context, req *ClientInfo, mcpURL string) (*ClientInfo, error) {
	// Generate client ID
	clientID, err := s.generateClientID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate client ID: %w", err)
	}

	clientSecret, err := s.generateClientSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to generate client secret: %w", err)
	}

	// Set default values
	client := &ClientInfo{
		MCPURL:                  mcpURL,
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		ClientSecretExpiresAt:   time.Now().Add(15 * 24 * time.Hour).Unix(),
		ClientName:              req.ClientName,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              req.GrantTypes,
		ResponseTypes:           req.ResponseTypes,
		Scope:                   req.Scope,
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
		ApplicationType:         req.ApplicationType,
		CreatedAt:               time.Now(),
		UpdatedAt:               time.Now(),
	}

	if err := s.applyDefaults(client); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	if err := s.validateClientRegistration(client); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	clientInfoStorage := OauthProxyClientInfo{
		MCPURL:                  mcpURL,
		ClientID:                client.ClientID,
		ClientSecret:            client.ClientSecret,
		ClientSecretExpiresAt:   time.Unix(client.ClientSecretExpiresAt, 0),
		ClientName:              client.ClientName,
		RedirectUris:            client.RedirectURIs,
		GrantTypes:              client.GrantTypes,
		ResponseTypes:           client.ResponseTypes,
		Scope:                   client.Scope,
		TokenEndpointAuthMethod: client.TokenEndpointAuthMethod,
		ApplicationType:         client.ApplicationType,
		CreatedAt:               client.CreatedAt,
		UpdatedAt:               client.UpdatedAt,
	}

	if err = s.storeClientInfo(ctx, clientInfoStorage); err != nil {
		return nil, fmt.Errorf("failed to store client: %w", err)
	}

	return client, nil
}

func (s *ClientRegistrationService) GetClient(ctx context.Context, mcpURL string, clientID string) (*ClientInfo, error) {
	storeClientInfo, err := s.getClientInfo(ctx, mcpURL, clientID)
	if err != nil {
		return nil, fmt.Errorf("client not found: %w", err)
	}

	client := &ClientInfo{
		MCPURL:                  storeClientInfo.MCPURL,
		ClientID:                storeClientInfo.ClientID,
		ClientSecret:            storeClientInfo.ClientSecret,
		ClientSecretExpiresAt:   storeClientInfo.ClientSecretExpiresAt.Unix(),
		ClientName:              storeClientInfo.ClientName,
		RedirectURIs:            storeClientInfo.RedirectUris,
		GrantTypes:              storeClientInfo.GrantTypes,
		ResponseTypes:           storeClientInfo.ResponseTypes,
		Scope:                   storeClientInfo.Scope,
		TokenEndpointAuthMethod: storeClientInfo.TokenEndpointAuthMethod,
		ApplicationType:         storeClientInfo.ApplicationType,
		CreatedAt:               storeClientInfo.CreatedAt,
		UpdatedAt:               storeClientInfo.UpdatedAt,
	}

	return client, nil
}

// applyDefaults sets default values for client registration
func (s *ClientRegistrationService) applyDefaults(client *ClientInfo) error {
	// Default grant types
	if len(client.GrantTypes) == 0 {
		client.GrantTypes = []string{"authorization_code"}
	}

	// Default response types
	if len(client.ResponseTypes) == 0 {
		client.ResponseTypes = []string{"code"}
	}

	// Default token endpoint auth method
	if client.TokenEndpointAuthMethod == "" {
		client.TokenEndpointAuthMethod = "client_secret_post"
	}

	// Default application type
	if client.ApplicationType == "" {
		client.ApplicationType = "web"
	}

	return nil
}

// validateClientRegistration validates client registration parameters
func (s *ClientRegistrationService) validateClientRegistration(client *ClientInfo) error {
	// Validate redirect URIs
	if len(client.RedirectURIs) == 0 {
		return fmt.Errorf("redirect_uris is required")
	}

	for _, uri := range client.RedirectURIs {
		if _, err := url.Parse(uri); err != nil {
			return fmt.Errorf("invalid redirect_uri: %s", uri)
		}
	}

	// Validate grant types
	validGrantTypes := map[string]bool{
		"authorization_code": true,
		"refresh_token":      true,
	}

	for _, grantType := range client.GrantTypes {
		if !validGrantTypes[grantType] {
			return fmt.Errorf("invalid grant_type: %s", grantType)
		}
	}

	// Validate response types
	validResponseTypes := map[string]bool{
		"code": true,
	}

	for _, responseType := range client.ResponseTypes {
		if !validResponseTypes[responseType] {
			return fmt.Errorf("invalid response_type: %s", responseType)
		}
	}

	// Validate token endpoint auth method
	validAuthMethods := map[string]bool{
		"client_secret_basic": true,
		"client_secret_post":  true,
		"none":                true,
	}

	if !validAuthMethods[client.TokenEndpointAuthMethod] {
		return fmt.Errorf("invalid token_endpoint_auth_method: %s", client.TokenEndpointAuthMethod)
	}

	// Validate application type
	validAppTypes := map[string]bool{
		"web":    true,
		"native": true,
	}

	if !validAppTypes[client.ApplicationType] {
		return fmt.Errorf("invalid application_type: %s", client.ApplicationType)
	}

	return nil
}

// generateClientID generates a unique client ID
func (s *ClientRegistrationService) generateClientID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate client ID: %w", err)
	}
	return "client_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

// generateClientSecret generates a secure client secret
func (s *ClientRegistrationService) generateClientSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate client secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// ValidateClientCredentials validates client credentials
func (s *ClientRegistrationService) ValidateClientCredentials(ctx context.Context, mcpURL string, clientID, clientSecret string) (*ClientInfo, error) {
	client, err := s.GetClient(ctx, mcpURL, clientID)
	if err != nil {
		return nil, fmt.Errorf("invalid client credentials")
	}

	// For public clients (no secret), skip secret validation
	if client.ClientSecret == "" && clientSecret == "" {
		return client, nil
	}

	// For confidential clients, validate secret
	if client.ClientSecret != clientSecret {
		return nil, fmt.Errorf("invalid client credentials")
	}

	return client, nil
}

// IsValidRedirectURI checks if a redirect URI is valid for the client
func (s *ClientRegistrationService) IsValidRedirectURI(ctx context.Context, mcpURL string, clientID, redirectURI string) (bool, error) {
	client, err := s.GetClient(ctx, mcpURL, clientID)
	if err != nil {
		return false, fmt.Errorf("failed to get client: %w", err)
	}

	if slices.Contains(client.RedirectURIs, redirectURI) {
		return true, nil
	}

	return false, nil
}

func (s *ClientRegistrationService) storeClientInfo(ctx context.Context, clientInfo OauthProxyClientInfo) error {
	if err := s.clientInfoStorage.Store(ctx, clientInfo); err != nil {
		return fmt.Errorf("failed to store client info: %w", err)
	}
	return nil
}

func (s *ClientRegistrationService) getClientInfo(ctx context.Context, mcpURL, clientId string) (*OauthProxyClientInfo, error) {
	clientInfo, err := s.clientInfoStorage.Get(ctx, ClientInfoCacheKey(mcpURL, clientId))
	if err != nil {
		return nil, fmt.Errorf("client info not found: %w", err)
	}

	return &clientInfo, nil
}
