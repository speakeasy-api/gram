package sessions

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/internal/cache"
	"github.com/speakeasy-api/gram/internal/contextvalues"
)

type Sessions struct {
	logger        *slog.Logger
	sessionCache  cache.Cache[Session]
	userInfoCache cache.Cache[CachedUserInfo]
}

func NewSessionAuth(logger *slog.Logger, redisClient *redis.Client) *Sessions {
	return &Sessions{
		logger:        logger.With("component", "sessions"),
		sessionCache:  cache.New[Session](redisClient, logger.With("cache", "session"), sessionCacheExpiry),
		userInfoCache: cache.New[CachedUserInfo](redisClient, logger.With("cache", "user_info"), userInfoCacheExpiry),
	}
}

func (s *Sessions) SessionAuth(ctx context.Context, key string, canStubAuth bool) (context.Context, error) {
	if key == "" {
		// This may have been set via cookie from http middleware, GOA does not support natively
		key, _ = contextvalues.GetSessionTokenFromContext(ctx)
	}

	if key == "" {
		// If you attempt auth with no token provided in local we will automatically populate the session from local env
		if canStubAuth && os.Getenv("GRAM_ENVIRONMENT") == "local" {
			var err error
			key, err = s.PopulateLocalDevDefaultAuthSession(ctx)
			if err != nil {
				return ctx, err
			}
		} else {
			return ctx, errors.New("session token is required for auth")
		}
	}

	session, err := s.sessionCache.Get(ctx, SessionCacheKey(key))
	if err != nil {
		return ctx, errors.New("session token is invalid")
	}

	if _, ok := s.HasAccessToOrganization(ctx, session.UserID, session.ActiveOrganizationID); !ok {
		return ctx, errors.New("user does not have access to organization")
	}

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		UserID:               session.UserID,
	})

	return ctx, nil
}

func (s *Sessions) UpdateSession(ctx context.Context, session Session) error {
	return s.sessionCache.Update(ctx, session)
}

func (s *Sessions) ClearSession(ctx context.Context, session Session) error {
	return s.sessionCache.Delete(ctx, session)
}
