package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/internal/cache"
	"github.com/speakeasy-api/gram/internal/contextvalues"
)

type localEnvFile map[string]struct {
	UserEmail     string `json:"user_email"`
	Organizations []struct {
		OrganizationID   string `json:"organization_id"`
		OrganizationName string `json:"organization_name"`
		OrganizationSlug string `json:"organization_slug"`
		AccountType      string `json:"account_type"`
	} `json:"organizations"`
}

type Sessions struct {
	logger        *slog.Logger
	sessionCache  cache.Cache[Session]
	userInfoCache cache.Cache[CachedUserInfo]
	localEnvFile  localEnvFile
	unsafeLocal   bool
}

func NewSessionAuth(logger *slog.Logger, redisClient *redis.Client, suffix cache.Suffix) *Sessions {
	return &Sessions{
		logger:        logger.With("component", "sessions"),
		sessionCache:  cache.New[Session](logger.With("cache", "session"), redisClient, sessionCacheExpiry, cache.SuffixNone),
		userInfoCache: cache.New[CachedUserInfo](logger.With("cache", "user_info"), redisClient, userInfoCacheExpiry, cache.SuffixNone),
	}
}

func NewUnsafeSessionAuth(logger *slog.Logger, redisClient *redis.Client, suffix cache.Suffix, localEnvPath string) (*Sessions, error) {
	file, err := os.Open(localEnvPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open local env file: %w", err)
	}
	defer file.Close()

	bs, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read local env file: %w", err)
	}

	var data localEnvFile
	if err := json.Unmarshal(bs, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal local env file: %w", err)
	}

	return &Sessions{
		logger:        logger.With("component", "sessions"),
		sessionCache:  cache.New[Session](logger.With("cache", "session"), redisClient, sessionCacheExpiry, cache.SuffixNone),
		userInfoCache: cache.New[CachedUserInfo](logger.With("cache", "user_info"), redisClient, userInfoCacheExpiry, cache.SuffixNone),
		localEnvFile:  data,
		unsafeLocal:   true,
	}, nil
}

func (s *Sessions) SessionAuth(ctx context.Context, key string, canStubAuth bool) (context.Context, error) {
	if key == "" {
		// This may have been set via cookie from http middleware, GOA does not support natively
		key, _ = contextvalues.GetSessionTokenFromContext(ctx)
	}

	if key == "" {
		// If you attempt auth with no token provided in local we will automatically populate the session from local env
		if canStubAuth && s.unsafeLocal {
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
