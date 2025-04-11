package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/internal/cache"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/o11y"
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

type Manager struct {
	logger        *slog.Logger
	sessionCache  cache.Cache[Session]
	userInfoCache cache.Cache[CachedUserInfo]
	localEnvFile  localEnvFile
	unsafeLocal   bool
}

func NewManager(logger *slog.Logger, redisClient *redis.Client, suffix cache.Suffix) *Manager {
	return &Manager{
		logger:        logger.With(slog.String("component", "sessions")),
		sessionCache:  cache.New[Session](logger.With(slog.String("cache", "session")), redisClient, sessionCacheExpiry, cache.SuffixNone),
		userInfoCache: cache.New[CachedUserInfo](logger.With(slog.String("cache", "user_info")), redisClient, userInfoCacheExpiry, cache.SuffixNone),
		localEnvFile:  localEnvFile{},
		unsafeLocal:   false,
	}
}

func NewUnsafeManager(logger *slog.Logger, redisClient *redis.Client, suffix cache.Suffix, localEnvPath string) (*Manager, error) {
	file, err := os.Open(filepath.Clean(localEnvPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open local env file: %w", err)
	}
	defer o11y.LogDefer(context.Background(), logger, file.Close())

	bs, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read local env file: %w", err)
	}

	var data localEnvFile
	if err := json.Unmarshal(bs, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal local env file: %w", err)
	}

	return &Manager{
		logger:        logger.With(slog.String("component", "sessions")),
		sessionCache:  cache.New[Session](logger.With(slog.String("cache", "session")), redisClient, sessionCacheExpiry, cache.SuffixNone),
		userInfoCache: cache.New[CachedUserInfo](logger.With(slog.String("cache", "user_info")), redisClient, userInfoCacheExpiry, cache.SuffixNone),
		localEnvFile:  data,
		unsafeLocal:   true,
	}, nil
}

func (s *Manager) Authenticate(ctx context.Context, key string, canStubAuth bool) (context.Context, error) {
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
		ProjectID:            nil,
	})

	return ctx, nil
}

func (s *Manager) UpdateSession(ctx context.Context, session Session) error {
	return s.sessionCache.Update(ctx, session)
}

func (s *Manager) ClearSession(ctx context.Context, session Session) error {
	return s.sessionCache.Delete(ctx, session)
}
