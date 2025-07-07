package sessions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
)

type localEnvFile map[string]struct {
	UserEmail     string `json:"user_email"`
	Admin         bool   `json:"admin"`
	Organizations []struct {
		OrganizationID   string `json:"organization_id"`
		OrganizationName string `json:"organization_name"`
		OrganizationSlug string `json:"organization_slug"`
		AccountType      string `json:"account_type"`
	} `json:"organizations"`
}

type Manager struct {
	logger                 *slog.Logger
	sessionCache           cache.TypedCacheObject[Session]
	userInfoCache          cache.TypedCacheObject[CachedUserInfo]
	localEnvFile           localEnvFile
	unsafeLocal            bool
	speakeasyServerAddress string
	speakeasySecretKey     string
	orgRepo                *orgRepo.Queries
	pylon                  *pylon.Pylon
}

func NewManager(logger *slog.Logger, db *pgxpool.Pool, redisClient *redis.Client, suffix cache.Suffix, speakeasyServerAddress string, speakeasySecretKey string, pylon *pylon.Pylon) *Manager {
	return &Manager{
		logger:                 logger.With(slog.String("component", "sessions")),
		sessionCache:           cache.NewTypedObjectCache[Session](logger.With(slog.String("cache", "session")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		userInfoCache:          cache.NewTypedObjectCache[CachedUserInfo](logger.With(slog.String("cache", "user_info")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		localEnvFile:           localEnvFile{},
		unsafeLocal:            false,
		speakeasyServerAddress: speakeasyServerAddress,
		speakeasySecretKey:     speakeasySecretKey,
		orgRepo:                orgRepo.New(db),
		pylon:                  pylon,
	}
}

func (s *Manager) IsUnsafeLocalDevelopment() bool {
	return s.unsafeLocal
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
			return ctx, oops.C(oops.CodeUnauthorized)
		}
	}

	session, err := s.sessionCache.Get(ctx, SessionCacheKey(key))
	if err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	authCtx := &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		UserID:               session.UserID,
		ProjectID:            nil,
		OrganizationSlug:     "",
		Email:                nil,
		AccountType:          "",
		ProjectSlug:          nil,
	}

	if session.ActiveOrganizationID == "" {
		ctx = contextvalues.SetAuthContext(ctx, authCtx)
		return ctx, nil
	}

	_, email, ok := s.HasAccessToOrganization(ctx, session.ActiveOrganizationID, session.UserID, session.SessionID)
	if !ok {
		return ctx, oops.C(oops.CodeForbidden)
	}

	var orgMetadata orgRepo.OrganizationMetadatum
	orgMetadata, err = s.orgRepo.GetOrganizationMetadata(ctx, session.ActiveOrganizationID)
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "error getting organization metadata").Log(ctx, s.logger)
	}
	authCtx.AccountType = orgMetadata.GramAccountType
	authCtx.OrganizationSlug = orgMetadata.Slug
	authCtx.Email = &email

	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	return ctx, nil
}

func (s *Manager) StoreSession(ctx context.Context, session Session) error {
	err := s.sessionCache.Store(ctx, session)
	if err != nil {
		return fmt.Errorf("store session: %w", err)
	}

	return nil
}

func (s *Manager) UpdateSession(ctx context.Context, session Session) error {
	err := s.sessionCache.Update(ctx, session)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}

	return nil
}

func (s *Manager) ClearSession(ctx context.Context, session Session) error {
	err := s.sessionCache.Delete(ctx, session)
	if err != nil {
		return fmt.Errorf("clear session: %w", err)
	}

	return nil
}
