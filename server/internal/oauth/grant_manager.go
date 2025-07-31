package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
)

// GrantManager handles OAuth authorization grant operations
type GrantManager struct {
	grantStorage            cache.TypedCacheObject[Grant]
	clientRegistration      *ClientRegistrationService
	pkceService             *PKCEService
	logger                  *slog.Logger
	grantExpiration         time.Duration
	authorizationCodeLength int
	enc                     *encryption.Client
}

func NewGrantManager(cacheImpl cache.Cache, clientRegistration *ClientRegistrationService, pkceService *PKCEService, logger *slog.Logger, enc *encryption.Client) *GrantManager {
	grantStorage := cache.NewTypedObjectCache[Grant](logger.With(attr.SlogCacheNamespace("oauth_grant")), cacheImpl, cache.SuffixNone)
	return &GrantManager{
		grantStorage:            grantStorage,
		clientRegistration:      clientRegistration,
		pkceService:             pkceService,
		logger:                  logger,
		grantExpiration:         10 * time.Minute, // 10 minutes is standard for OAuth 2.1
		authorizationCodeLength: 32,
		enc:                     enc,
	}
}

// CreateAuthorizationGrant creates a new authorization grant
func (gm *GrantManager) CreateAuthorizationGrant(ctx context.Context, req *AuthorizationRequest, mcpSlug string, accessToken string, expiresAt *time.Time, securityKeys []string) (*Grant, error) {
	if err := gm.ValidateAuthorizationRequest(ctx, req, mcpSlug); err != nil {
		return nil, fmt.Errorf("invalid authorization request: %w", err)
	}

	authCode, err := gm.generateAuthorizationCode()
	if err != nil {
		return nil, fmt.Errorf("failed to generate authorization code: %w", err)
	}

	grant := &Grant{
		MCPSlug:             mcpSlug,
		Code:                authCode,
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		Scope:               req.Scope,
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		Props:               make(map[string]string),
		ExternalSecrets: []ExternalSecret{
			{
				SecurityKeys: securityKeys,
				Token:        accessToken,
				ExpiresAt:    expiresAt,
			},
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(gm.grantExpiration),
	}

	if req.Nonce != "" {
		grant.Props["nonce"] = req.Nonce
	}

	if err := gm.storeGrant(ctx, *grant); err != nil {
		return nil, fmt.Errorf("failed to store grant: %w", err)
	}

	gm.logger.InfoContext(ctx, "authorization grant created",
		attr.SlogOAuthClientID(req.ClientID),
		attr.SlogOAuthScope(req.Scope))

	return grant, nil
}

// ValidateAndConsumeGrant validates and consumes an authorization grant
func (gm *GrantManager) ValidateAndConsumeGrant(ctx context.Context, mcpSlug string, code, clientID, redirectURI string) (*Grant, error) {
	grant, err := gm.getGrant(ctx, mcpSlug, code)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization code")
	}

	if time.Now().After(grant.ExpiresAt) {
		// Clean up expired grant
		if err := gm.deleteGrant(ctx, *grant); err != nil {
			gm.logger.ErrorContext(ctx, "failed to delete expired grant", attr.SlogError(err))
		}
		return nil, fmt.Errorf("authorization code has expired")
	}

	// Validate client ID
	if grant.ClientID != clientID {
		return nil, fmt.Errorf("invalid client")
	}

	// Validate redirect URI
	if grant.RedirectURI != redirectURI {
		return nil, fmt.Errorf("invalid redirect URI")
	}

	// Grant is valid, consume it (delete it to prevent replay)
	if err := gm.deleteGrant(ctx, *grant); err != nil {
		gm.logger.ErrorContext(ctx, "failed to delete grant",
			attr.SlogOAuthCode(code),
			attr.SlogError(err))
	}

	gm.logger.InfoContext(ctx, "grant validated and consumed",
		attr.SlogOAuthClientID(clientID))

	return grant, nil
}

// ValidateAuthorizationRequest validates an authorization request
func (gm *GrantManager) ValidateAuthorizationRequest(ctx context.Context, req *AuthorizationRequest, mcpSlug string) error {
	if req.ResponseType == "" {
		return fmt.Errorf("response_type is required")
	}

	if req.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}

	if req.RedirectURI == "" {
		return fmt.Errorf("redirect_uri is required")
	}

	if req.ResponseType != "code" {
		return fmt.Errorf("unsupported response_type: %s", req.ResponseType)
	}

	// Validate client exists
	client, err := gm.clientRegistration.GetClient(ctx, mcpSlug, req.ClientID)
	if err != nil {
		return fmt.Errorf("invalid client_id: %w", err)
	}

	validRedirectURI, err := gm.clientRegistration.IsValidRedirectURI(ctx, mcpSlug, req.ClientID, req.RedirectURI)
	if err != nil {
		return fmt.Errorf("failed to validate redirect URI: %w", err)
	}
	if !validRedirectURI {
		return fmt.Errorf("invalid redirect_uri")
	}

	if req.CodeChallenge != "" {
		if req.CodeChallengeMethod == "plain" {
			gm.logger.InfoContext(ctx, "client is using plain code challenge method", attr.SlogOAuthClientID(req.ClientID))
		}
		if err := gm.pkceService.ValidateCodeChallenge(ctx, req.CodeChallenge, req.CodeChallengeMethod); err != nil {
			return fmt.Errorf("invalid PKCE challenge: %w", err)
		}
	}

	// Validate scope (basic validation)
	if req.Scope != "" {
		if err := gm.validateScope(req.Scope, client); err != nil {
			return fmt.Errorf("invalid scope: %w", err)
		}
	}

	return nil
}

// validateScope validates the requested scope
func (gm *GrantManager) validateScope(requestedScope string, client *ClientInfo) error {
	// If no scope is configured for the client, allow any scope
	if client.Scope == "" {
		return nil
	}

	// Parse scopes
	clientScopes := strings.Fields(client.Scope)
	requestedScopes := strings.Fields(requestedScope)

	// Check if all requested scopes are allowed
	clientScopeMap := make(map[string]bool)
	for _, scope := range clientScopes {
		clientScopeMap[scope] = true
	}

	for _, scope := range requestedScopes {
		if !clientScopeMap[scope] {
			return fmt.Errorf("scope '%s' is not allowed for this client", scope)
		}
	}

	return nil
}

// generateAuthorizationCode generates a cryptographically secure authorization code
func (gm *GrantManager) generateAuthorizationCode() (string, error) {
	bytes := make([]byte, gm.authorizationCodeLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// BuildAuthorizationResponse builds the authorization response URL
func (gm *GrantManager) BuildAuthorizationResponse(ctx context.Context, grant *Grant, redirectURI string) (string, error) {
	// Parse the redirect URI
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", fmt.Errorf("invalid redirect URI: %w", err)
	}

	// Add query parameters
	query := u.Query()
	query.Set("code", grant.Code)

	if grant.State != "" {
		query.Set("state", grant.State)
	}

	u.RawQuery = query.Encode()

	gm.logger.InfoContext(ctx, "authorization response built",
		attr.SlogOAuthClientID(grant.ClientID),
		attr.SlogOAuthRedirectURIFull(redirectURI))

	return u.String(), nil
}

// BuildErrorResponse builds an error response URL
func (gm *GrantManager) BuildErrorResponse(ctx context.Context, redirectURI, errorString, errorDescription, state string) (string, error) {
	// Parse the redirect URI
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", fmt.Errorf("invalid redirect URI: %w", err)
	}

	// Add error parameters
	query := u.Query()
	query.Set("error", errorString)

	if errorDescription != "" {
		query.Set("error_description", errorDescription)
	}

	if state != "" {
		query.Set("state", state)
	}

	u.RawQuery = query.Encode()

	gm.logger.InfoContext(ctx, "error response built",
		attr.SlogErrorMessage(errorString),
		attr.SlogOAuthRedirectURIFull(redirectURI))

	return u.String(), nil
}

func (gm *GrantManager) storeGrant(ctx context.Context, grant Grant) error {
	encryptedExternalSecrets := make([]ExternalSecret, len(grant.ExternalSecrets))
	for i, externalSecret := range grant.ExternalSecrets {
		encryptedTokenSecret, err := gm.enc.Encrypt([]byte(externalSecret.Token))
		if err != nil {
			return fmt.Errorf("failed to encrypt token secret: %w", err)
		}
		encryptedExternalSecrets[i] = ExternalSecret{
			Token:        encryptedTokenSecret,
			SecurityKeys: externalSecret.SecurityKeys,
			ExpiresAt:    externalSecret.ExpiresAt,
		}
	}
	grant.ExternalSecrets = encryptedExternalSecrets
	grant.CreatedAt = time.Now()
	grant.ExpiresAt = time.Now().Add(10 * time.Minute) // 10 minute expiration
	if err := gm.grantStorage.Store(ctx, grant); err != nil {
		return fmt.Errorf("failed to store grant: %w", err)
	}
	return nil
}

func (gm *GrantManager) getGrant(ctx context.Context, mcpSlug string, code string) (*Grant, error) {
	grant, err := gm.grantStorage.Get(ctx, GrantCacheKey(mcpSlug, code))
	if err != nil {
		return nil, fmt.Errorf("grant not found: %w", err)
	}

	for i, externalSecret := range grant.ExternalSecrets {
		decryptedTokenSecret, err := gm.enc.Decrypt(externalSecret.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt token secret: %w", err)
		}
		grant.ExternalSecrets[i].Token = decryptedTokenSecret
	}

	// Check if grant has expired
	if time.Now().After(grant.ExpiresAt) {
		return nil, fmt.Errorf("grant has expired")
	}

	return &grant, nil
}

func (gm *GrantManager) deleteGrant(ctx context.Context, grant Grant) error {
	if err := gm.grantStorage.Delete(ctx, grant); err != nil {
		return fmt.Errorf("failed to delete grant: %w", err)
	}
	return nil
}
