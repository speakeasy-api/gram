package sessions

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/internal/cache"
)

type Sessions struct {
	sessionCache cache.Cache[GramSession]
}

func New() *Sessions {
	return &Sessions{
		sessionCache: cache.New[GramSession](sessionCacheExpiry),
	}
}

func (s *Sessions) SessionAuth(ctx context.Context, key string) (context.Context, error) {
	if key == "" {
		// This may have been set via cookie from http middleware, GOA does not support natively
		key, _ = GetSessionTokenFromContext(ctx)
	}

	if key == "" {
		return ctx, errors.New("session token is required for auth")
	}

	session, err := s.sessionCache.Get(ctx, key)
	if err != nil {
		return ctx, errors.New("session token is invalid: " + err.Error())
	}

	ctx = SetSessionValueInContext(ctx, &session)

	return ctx, nil
}
