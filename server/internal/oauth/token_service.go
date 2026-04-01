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
	"github.com/speakeasy-api/gram/server/internal/inv"
)

const defaultTokenExpiration = 30 * 24 * time.Hour

// TokenService handles OAuth token operations
type TokenService struct {
	tokenStorage       cache.TypedCacheObject[Token]
	clientRegistration *ClientRegistrationService
	grantManager       *GrantManager
	pkceService        *PKCEService
	logger             *slog.Logger
	tokenLength        int
	enc                *encryption.Client
}

func NewTokenService(cacheImpl cache.Cache, clientRegistration *ClientRegistrationService, grantManager *GrantManager, pkceService *PKCEService, logger *slog.Logger, enc *encryption.Client) *TokenService {
	tokenStorage := cache.NewTypedObjectCache[Token](logger.With(attr.SlogCacheNamespace("oauth_token")), cacheImpl, cache.SuffixNone)
	return &TokenService{
		tokenStorage:       tokenStorage,
		clientRegistration: clientRegistration,
		grantManager:       grantManager,
		pkceService:        pkceService,
		logger:             logger,
		tokenLength:        32,
		enc:                enc,
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

	if err := inv.Check("grant",
		"has exactly 1 external secret", len(grant.ExternalSecrets) == 1,
	); err != nil {
		return nil, fmt.Errorf("invalid grant (invariant): %w", err)
	}
	secret := grant.ExternalSecrets[0]

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

	refreshToken, err := ts.generateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	// When the upstream server didn't issue a refresh token, we can't refresh
	// the external secret, so our token's lifetime is bounded by the upstream
	// token's expiry. In that case, use the upstream expiry directly.
	//
	// However, if the upstream token lives longer than our standard access
	// token duration, cap it to our standard duration minus a 10-minute buffer.
	// The buffer ensures our token expires before the upstream token, giving
	// clients a window to detect expiry and re-authenticate rather than
	// failing mid-request with a stale upstream token. Additionally, we using
	// this lower bound to better mitigate against token exposure. A short
	// expiry means that if an access token is leaked, it will be valid for a
	// shorter period of time, reducing the potential impact of the leak.
	expires := time.Now().Add(defaultTokenExpiration)
	if secret.RefreshToken == "" && secret.ExpiresAt != nil && !secret.ExpiresAt.IsZero() {
		expires = *secret.ExpiresAt
		if time.Until(expires) > defaultTokenExpiration {
			expires = time.Now().Add(defaultTokenExpiration).Add(-10 * time.Minute)
		}
	}

	// Create token response
	token := &Token{
		ToolsetID:       toolsetId,
		AccessToken:     accessToken,
		RefreshToken:    refreshToken,
		TokenType:       "Bearer",
		Scope:           grant.Scope,
		ExternalSecrets: grant.ExternalSecrets,
		CreatedAt:       time.Now(),
		ExpiresAt:       expires,
	}

	if err := ts.storeToken(ctx, *token); err != nil {
		return nil, fmt.Errorf("failed to store token: %w", err)
	}

	return token, nil
}

// ExchangeRefreshToken exchanges a refresh token for a new access/refresh token pair (rotation).
func (ts *TokenService) ExchangeRefreshToken(ctx context.Context, req *TokenRequest, mcpURL string, toolsetID uuid.UUID) (*Token, error) {
	if req.ClientID == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	if req.RefreshToken == "" {
		return nil, fmt.Errorf("refresh_token is required")
	}

	_, err := ts.clientRegistration.ValidateClientCredentials(ctx, mcpURL, req.ClientID, req.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid client credentials: %w", err)
	}

	// Look up existing token by refresh token hash
	refreshHash := sha256.Sum256([]byte(req.RefreshToken))
	refreshTokenHash := base64.RawURLEncoding.EncodeToString(refreshHash[:])
	oldToken, err := ts.tokenStorage.Get(ctx, RefreshTokenCacheKey(toolsetID, refreshTokenHash))
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	if err := inv.Check("grant",
		"has exactly 1 external secret", len(oldToken.ExternalSecrets) == 1,
	); err != nil {
		return nil, fmt.Errorf("invalid outgoing access token (invariant): %w", err)
	}
	secret := oldToken.ExternalSecrets[0]

	// Decrypt external secrets and check expiration
	for i, externalSecret := range oldToken.ExternalSecrets {
		decrypted, err := ts.enc.Decrypt(externalSecret.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt token secret: %w", err)
		}
		oldToken.ExternalSecrets[i].Token = decrypted
		if externalSecret.RefreshToken != "" {
			decryptedRefresh, err := ts.enc.Decrypt(externalSecret.RefreshToken)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt refresh token secret: %w", err)
			}
			oldToken.ExternalSecrets[i].RefreshToken = decryptedRefresh
		}
		if externalSecret.ExpiresAt != nil && externalSecret.ExpiresAt.Before(time.Now()) {
			return nil, fmt.Errorf("underlying credentials have expired")
		}
	}

	// Delete old token (removes both access + refresh cache keys).
	// The retrieved token already has hashed AccessToken and RefreshToken from storage,
	// so CacheKey() and AdditionalCacheKeys() compute the correct keys.
	if err := ts.deleteToken(ctx, oldToken); err != nil {
		ts.logger.ErrorContext(ctx, "failed to delete old token during refresh", attr.SlogError(err))
	}

	// Generate new access token + new refresh token (rotation)
	newAccessToken, err := ts.generateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}
	newRefreshToken, err := ts.generateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	// When the upstream server didn't issue a refresh token, we can't refresh
	// the external secret, so our token's lifetime is bounded by the upstream
	// token's expiry. In that case, use the upstream expiry directly.
	//
	// However, if the upstream token lives longer than our standard access
	// token duration, cap it to our standard duration minus a 10-minute buffer.
	// The buffer ensures our token expires before the upstream token, giving
	// clients a window to detect expiry and re-authenticate rather than
	// failing mid-request with a stale upstream token. Additionally, we using
	// this lower bound to better mitigate against token exposure. A short
	// expiry means that if an access token is leaked, it will be valid for a
	// shorter period of time, reducing the potential impact of the leak.
	expires := time.Now().Add(defaultTokenExpiration)
	if secret.RefreshToken == "" && secret.ExpiresAt != nil && !secret.ExpiresAt.IsZero() {
		expires = *secret.ExpiresAt
		if time.Until(expires) > defaultTokenExpiration {
			expires = time.Now().Add(defaultTokenExpiration).Add(-10 * time.Minute)
		}
	}

	newToken := &Token{
		ToolsetID:       toolsetID,
		AccessToken:     newAccessToken,
		RefreshToken:    newRefreshToken,
		TokenType:       "Bearer",
		Scope:           oldToken.Scope,
		ExternalSecrets: oldToken.ExternalSecrets,
		CreatedAt:       time.Now(),
		ExpiresAt:       expires,
	}

	if err := ts.storeToken(ctx, *newToken); err != nil {
		return nil, fmt.Errorf("failed to store new token: %w", err)
	}

	return newToken, nil
}

var (
	ErrInvalidAccessToken     = fmt.Errorf("invalid access token")
	ErrExpiredAccessToken     = fmt.Errorf("access token has expired")
	ErrExpiredExternalSecrets = fmt.Errorf("underlying credentials have expired")
	ErrNoUpstreamRefreshToken = fmt.Errorf("no upstream refresh token available")
)

// ValidateAccessToken validates an access token
func (ts *TokenService) ValidateAccessToken(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*Token, error) {
	token, err := ts.getToken(ctx, toolsetId, accessToken)
	if err != nil {
		return nil, fmt.Errorf("%w: session not found", ErrInvalidAccessToken)
	}

	// Check if token has expired
	if time.Now().After(token.ExpiresAt) {
		// Don't delete — the refresh token may still be valid and the client
		// can exchange it for a new token pair.
		return nil, ErrExpiredAccessToken
	}

	for _, externalSecret := range token.ExternalSecrets {
		if externalSecret.ExpiresAt != nil && externalSecret.ExpiresAt.Before(time.Now()) {
			// Return the token alongside the error so the caller can attempt an upstream refresh
			return token, fmt.Errorf("%w: upstream external secret expired", ErrExpiredExternalSecrets)
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
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		ExpiresIn:    expiresIn,
		Scope:        token.Scope,
		RefreshToken: token.RefreshToken,
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

// RefreshExternalSecrets updates the external secrets on an existing token in the cache.
func (ts *TokenService) RefreshExternalSecrets(ctx context.Context, token *Token, newSecrets []ExternalSecret) error {
	token.ExternalSecrets = newSecrets
	storeCopy := *token

	// Encrypt external secrets
	encryptedExternalSecrets := make([]ExternalSecret, len(storeCopy.ExternalSecrets))
	for i, es := range storeCopy.ExternalSecrets {
		encryptedToken, err := ts.enc.Encrypt([]byte(es.Token))
		if err != nil {
			return fmt.Errorf("failed to encrypt token secret: %w", err)
		}
		var encryptedRefreshToken string
		if es.RefreshToken != "" {
			encryptedRefreshToken, err = ts.enc.Encrypt([]byte(es.RefreshToken))
			if err != nil {
				return fmt.Errorf("failed to encrypt refresh token secret: %w", err)
			}
		}
		encryptedExternalSecrets[i] = ExternalSecret{
			Token:        encryptedToken,
			RefreshToken: encryptedRefreshToken,
			SecurityKeys: es.SecurityKeys,
			ExpiresAt:    es.ExpiresAt,
		}
	}
	storeCopy.ExternalSecrets = encryptedExternalSecrets

	// The token's AccessToken is already hashed from getToken, so we can store directly
	if err := ts.tokenStorage.Store(ctx, storeCopy); err != nil {
		return fmt.Errorf("failed to store updated token: %w", err)
	}
	return nil
}

func (ts *TokenService) storeToken(ctx context.Context, token Token) error {
	encryptedExternalSecrets := make([]ExternalSecret, len(token.ExternalSecrets))
	for i, externalSecret := range token.ExternalSecrets {
		encryptedTokenSecret, err := ts.enc.Encrypt([]byte(externalSecret.Token))
		if err != nil {
			return fmt.Errorf("failed to encrypt token secret: %w", err)
		}
		var encryptedRefreshToken string
		if externalSecret.RefreshToken != "" {
			encryptedRefreshToken, err = ts.enc.Encrypt([]byte(externalSecret.RefreshToken))
			if err != nil {
				return fmt.Errorf("failed to encrypt refresh token secret: %w", err)
			}
		}
		encryptedExternalSecrets[i] = ExternalSecret{
			Token:        encryptedTokenSecret,
			RefreshToken: encryptedRefreshToken,
			SecurityKeys: externalSecret.SecurityKeys,
			ExpiresAt:    externalSecret.ExpiresAt,
		}
	}
	token.ExternalSecrets = encryptedExternalSecrets
	// hash access token on storage
	hash := sha256.Sum256([]byte(token.AccessToken))
	token.AccessToken = base64.RawURLEncoding.EncodeToString(hash[:])

	// hash refresh token on storage
	if token.RefreshToken != "" {
		refreshHash := sha256.Sum256([]byte(token.RefreshToken))
		token.RefreshToken = base64.RawURLEncoding.EncodeToString(refreshHash[:])
	}

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

	for i, externalSecret := range token.ExternalSecrets {
		decryptedTokenSecret, err := ts.enc.Decrypt(externalSecret.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt token secret: %w", err)
		}
		token.ExternalSecrets[i].Token = decryptedTokenSecret
		if externalSecret.RefreshToken != "" {
			decryptedRefreshToken, err := ts.enc.Decrypt(externalSecret.RefreshToken)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt refresh token secret: %w", err)
			}
			token.ExternalSecrets[i].RefreshToken = decryptedRefreshToken
		}
	}

	return &token, nil
}

func (ts *TokenService) deleteToken(ctx context.Context, token Token) error {
	if err := ts.tokenStorage.Delete(ctx, token); err != nil {
		return fmt.Errorf("failed to delete token: %w", err)
	}
	return nil
}
