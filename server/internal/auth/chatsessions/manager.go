package chatsessions

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// Manager handles chat session token lifecycle
type Manager struct {
	logger             *slog.Logger
	revokedTokensCache cache.TypedCacheObject[RevokedToken]
	jwtSecret          string
}

// RevokedToken represents a revoked chat session token in the cache
type RevokedToken struct {
	JTI       string    `json:"jti"`
	RevokedAt time.Time `json:"revoked_at"`
}

// CacheKey returns the cache key for a revoked token
func (r RevokedToken) CacheKey() string {
	return fmt.Sprintf("chat_session_revoked:%s", r.JTI)
}

// AdditionalCacheKeys returns additional cache keys for the revoked token (none needed)
func (r RevokedToken) AdditionalCacheKeys() []string {
	return nil
}

// TTL returns the TTL for the cache entry (24 hours for revoked tokens)
func (r RevokedToken) TTL() time.Duration {
	return 24 * time.Hour
}

// NewManager creates a new chat sessions manager
func NewManager(logger *slog.Logger, redisClient *redis.Client, jwtSecret string) *Manager {
	logger = logger.With(attr.SlogComponent("chat_sessions"))

	return &Manager{
		logger:             logger,
		revokedTokensCache: cache.NewTypedObjectCache[RevokedToken](logger.With(attr.SlogCacheNamespace("chat_session_revoked")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		jwtSecret:          jwtSecret,
	}
}

// RevokeToken adds a token JTI to the revoked tokens cache
func (m *Manager) RevokeToken(ctx context.Context, jti string) error {
	revokedToken := RevokedToken{
		JTI:       jti,
		RevokedAt: time.Now(),
	}

	err := m.revokedTokensCache.Store(ctx, revokedToken)
	if err != nil {
		return fmt.Errorf("store revoked token: %w", err)
	}

	return nil
}

// IsTokenRevoked checks if a token JTI is in the revoked tokens cache
func (m *Manager) IsTokenRevoked(ctx context.Context, jti string) (bool, error) {
	cacheKey := fmt.Sprintf("chat_session_revoked:%s", jti)
	_, err := m.revokedTokensCache.Get(ctx, cacheKey)
	if err != nil {
		if err.Error() == "key is missing" {
			return false, nil
		}
		return false, fmt.Errorf("get revoked token: %w", err)
	}

	return true, nil
}

func (m *Manager) Authorize(ctx context.Context, token string) (context.Context, error) {
	claims, err := m.ValidateToken(ctx, token)
	if err != nil {
		return ctx, err
	}

	// Parse project ID from string to UUID
	projectID, err := uuid.Parse(claims.ProjectID)
	if err != nil {
		return ctx, fmt.Errorf("failed to parse project ID: %w", err)
	}

	// Set auth context from JWT claims
	externalUserID := ""
	if claims.ExternalUserID != nil {
		externalUserID = *claims.ExternalUserID
	}

	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID: claims.OrgID,
		ProjectID:            &projectID,
		OrganizationSlug:     claims.OrganizationSlug,
		ProjectSlug:          &claims.ProjectSlug,
		UserID:               "",
		ExternalUserID:       externalUserID,
		Email:                nil,
		AccountType:          "",
		APIKeyScopes:         nil,
		SessionID:            nil, // DO NOT SET THIS for chat sessions. The existence of this field implies that this is a dashboard-authenticated request.
		APIKeyID:             claims.APIKeyID,
	}

	return contextvalues.SetAuthContext(ctx, authCtx), nil
}
