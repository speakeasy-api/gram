package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
)

// TokenService handles OAuth token operations
type TokenService struct {
	tokenStorage          cache.TypedCacheObject[Token]
	clientRegistration    *ClientRegistrationService
	grantManager          *GrantManager
	pkceService           *PKCEService
	logger                *slog.Logger
	accessTokenExpiration time.Duration
	tokenLength           int
	enc                   *encryption.Client
}

func NewTokenService(cacheImpl cache.Cache, clientRegistration *ClientRegistrationService, grantManager *GrantManager, pkceService *PKCEService, logger *slog.Logger, enc *encryption.Client) *TokenService {
	tokenStorage := cache.NewTypedObjectCache[Token](logger.With(attr.SlogCacheNamespace("oauth_token")), cacheImpl, cache.SuffixNone)
	return &TokenService{
		tokenStorage:          tokenStorage,
		clientRegistration:    clientRegistration,
		grantManager:          grantManager,
		pkceService:           pkceService,
		logger:                logger,
		accessTokenExpiration: 30 * 24 * time.Hour, // This is an overaching default, it is actually driven by the underlying credentials
		tokenLength:           32,
		enc:                   enc,
	}
}

// ExchangeAuthorizationCode exchanges an authorization code for tokens
func (ts *TokenService) ExchangeAuthorizationCode(ctx context.Context, req *TokenRequest, mcpURL string, toolsetId uuid.UUID) (*Token, error) {
	if err := ts.validateTokenRequest(req); err != nil {
		return nil, fmt.Errorf("invalid token request: %w", err)
	}

	_, err := ts.clientRegistration.ValidateClientCredentials(ctx, mcpURL, req.ClientID, req.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid client credentials: %w", err)
	}

	grant, err := ts.grantManager.ValidateAndConsumeGrant(ctx, toolsetId, req.Code, req.ClientID, req.RedirectURI)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization code: %w", err)
	}

	// Validate PKCE if present
	if req.CodeVerifier != "" {
		if err := ts.pkceService.ValidatePKCEFlow(ctx, grant, req.CodeVerifier); err != nil {
			return nil, fmt.Errorf("PKCE validation failed: %w", err)
		}
	}

	accessToken, err := ts.generateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	// Create token response
	token := &Token{
		ToolsetID:       toolsetId,
		AccessToken:     accessToken,
		TokenType:       "Bearer",
		Scope:           grant.Scope,
		ExternalSecrets: grant.ExternalSecrets,
		CreatedAt:       time.Now(),
		ExpiresAt:       time.Now().Add(ts.accessTokenExpiration),
	}

	if err := ts.storeToken(ctx, *token); err != nil {
		return nil, fmt.Errorf("failed to store token: %w", err)
	}

	return token, nil
}

var (
	ErrInvalidAccessToken = fmt.Errorf("invalid access token")
	ErrExpiredAccessToken = fmt.Errorf("access token has expired")
)

// ValidateAccessToken validates an access token
func (ts *TokenService) ValidateAccessToken(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*Token, error) {
	token, err := ts.getToken(ctx, toolsetId, accessToken)
	if err != nil {
		return nil, ErrInvalidAccessToken
	}

	// Check if token has expired
	if time.Now().After(token.ExpiresAt) {
		if err := ts.deleteToken(ctx, *token); err != nil {
			ts.logger.ErrorContext(ctx, "failed to delete expired token", attr.SlogError(err))
		}
		return nil, ErrExpiredAccessToken
	}

	for _, externalSecret := range token.ExternalSecrets {
		if externalSecret.ExpiresAt.Before(time.Now()) {
			// TODO: Eventually we will want to 403 but not actually delete the credentials as we may have multiple credentials under the hood
			if err := ts.deleteToken(ctx, *token); err != nil {
				ts.logger.ErrorContext(ctx, "failed to delete expired token", attr.SlogError(err))
			}
			return nil, ErrExpiredAccessToken
		}
	}

	return token, nil
}

// validateTokenRequest validates a token request
func (ts *TokenService) validateTokenRequest(req *TokenRequest) error {
	if req.GrantType != "authorization_code" {
		return fmt.Errorf("unsupported grant_type: %s", req.GrantType)
	}

	if req.Code == "" {
		return fmt.Errorf("code is required")
	}

	if req.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}

	if req.RedirectURI == "" {
		return fmt.Errorf("redirect_uri is required")
	}

	return nil
}

// generateToken generates a cryptographically secure token
func (ts *TokenService) generateToken() (string, error) {
	bytes := make([]byte, ts.tokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// CreateTokenResponse creates a standardized token response
func (ts *TokenService) CreateTokenResponse(token *Token) *TokenResponse {
	expiresIn := max(int(time.Until(token.ExpiresAt).Seconds()), 0)

	return &TokenResponse{
		AccessToken: token.AccessToken,
		TokenType:   token.TokenType,
		ExpiresIn:   expiresIn,
		Scope:       token.Scope,
	}
}

// CreateErrorResponse creates a standardized error response
func (ts *TokenService) CreateErrorResponse(errorType, description string) map[string]any {
	response := map[string]any{
		"error": errorType,
	}

	if description != "" {
		response["error_description"] = description
	}

	return response
}

// SetTokenExpiration sets custom token expiration time
func (ts *TokenService) SetTokenExpiration(accessTokenExpiration time.Duration) {
	ts.accessTokenExpiration = accessTokenExpiration
}

func (ts *TokenService) storeToken(ctx context.Context, token Token) error {
	encryptedExternalSecrets := make([]ExternalSecret, len(token.ExternalSecrets))
	for i, externalSecret := range token.ExternalSecrets {
		encryptedTokenSecret, err := ts.enc.Encrypt([]byte(externalSecret.Token))
		if err != nil {
			return fmt.Errorf("failed to encrypt token secret: %w", err)
		}
		encryptedExternalSecrets[i] = ExternalSecret{
			Token:        encryptedTokenSecret,
			SecurityKeys: externalSecret.SecurityKeys,
			ExpiresAt:    externalSecret.ExpiresAt,
		}
	}
	token.ExternalSecrets = encryptedExternalSecrets
	// hash access token on storage
	hash := sha256.Sum256([]byte(token.AccessToken))
	token.AccessToken = base64.RawURLEncoding.EncodeToString(hash[:])

	if err := ts.tokenStorage.Store(ctx, token); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}
	return nil
}

func (ts *TokenService) getToken(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*Token, error) {
	hash := sha256.Sum256([]byte(accessToken))
	accessTokenHash := base64.RawURLEncoding.EncodeToString(hash[:])
	token, err := ts.tokenStorage.Get(ctx, TokenCacheKey(toolsetId, accessTokenHash))
	if err != nil {
		return nil, fmt.Errorf("token not found: %w", err)
	}

	token.AccessToken = accessTokenHash

	// Check if token has expired
	if time.Now().After(token.ExpiresAt) {
		return nil, fmt.Errorf("token has expired")
	}

	for i, externalSecret := range token.ExternalSecrets {
		decryptedTokenSecret, err := ts.enc.Decrypt(externalSecret.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt token secret: %w", err)
		}
		token.ExternalSecrets[i].Token = decryptedTokenSecret
	}

	return &token, nil
}

func (ts *TokenService) deleteToken(ctx context.Context, token Token) error {
	if err := ts.tokenStorage.Delete(ctx, token); err != nil {
		return fmt.Errorf("failed to delete token: %w", err)
	}
	return nil
}
