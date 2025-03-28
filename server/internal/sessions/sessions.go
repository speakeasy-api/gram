package sessions

import (
	"context"
	"errors"
	"os"

	"github.com/speakeasy-api/gram/internal/cache"
)

type Sessions struct {
	sessionCache  cache.Cache[GramSession]
	userInfoCache cache.Cache[CachedUserInfo]
}

func New() *Sessions {
	return &Sessions{
		sessionCache:  cache.New[GramSession](sessionCacheExpiry),
		userInfoCache: cache.New[CachedUserInfo](userInfoCacheExpiry),
	}
}

func (s *Sessions) SessionAuth(ctx context.Context, key string) (context.Context, error) {
	if key == "" {
		// This may have been set via cookie from http middleware, GOA does not support natively
		key, _ = GetSessionTokenFromContext(ctx)
	}

	if key == "" {
		// If you attempt auth with no token provided in local we will automatically populate the session from local env
		if os.Getenv("GRAM_ENVIRONMENT") == "local" {
			var err error
			key, err = s.PopulateLocalDevDefaultAuthSession(ctx)
			if err != nil {
				return ctx, err
			}
		} else {
			return ctx, errors.New("session token is required for auth")
		}
	}

	session, err := s.sessionCache.Get(ctx, GramSessionCacheKey(key))
	if err != nil {
		return ctx, errors.New("session token is invalid")
	}

	if _, ok := s.HasAccessToOrganization(ctx, session.UserID, session.ActiveOrganizationID); !ok {
		return ctx, errors.New("user does not have access to organization")
	}

	ctx = SetSessionValueInContext(ctx, &session)

	return ctx, nil
}

func (s *Sessions) UpdateSession(ctx context.Context, session GramSession) error {
	return s.sessionCache.Store(ctx, session)
}

func (s *Sessions) ClearSession(ctx context.Context, session GramSession) error {
	return s.sessionCache.Delete(ctx, session)
}
