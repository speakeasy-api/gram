package sessions

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/internal/cache"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/internal/organizations/repo"
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
	sessionCache           cache.Cache[Session]
	userInfoCache          cache.Cache[CachedUserInfo]
	localEnvFile           localEnvFile
	unsafeLocal            bool
	speakeasyServerAddress string
	speakeasySecretKey     string
	orgRepo                *orgRepo.Queries
}

func NewManager(logger *slog.Logger, db *pgxpool.Pool, redisClient *redis.Client, suffix cache.Suffix, speakeasyServerAddress string, speakeasySecretKey string) *Manager {
	return &Manager{
		logger:                 logger.With(slog.String("component", "sessions")),
		sessionCache:           cache.New[Session](logger.With(slog.String("cache", "session")), redisClient, sessionCacheExpiry, cache.SuffixNone),
		userInfoCache:          cache.New[CachedUserInfo](logger.With(slog.String("cache", "user_info")), redisClient, userInfoCacheExpiry, cache.SuffixNone),
		localEnvFile:           localEnvFile{},
		unsafeLocal:            false,
		speakeasyServerAddress: speakeasyServerAddress,
		speakeasySecretKey:     speakeasySecretKey,
		orgRepo:                orgRepo.New(db),
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
		AccountType:          "",
		ProjectSlug:          nil,
	}

	if _, ok := s.HasAccessToOrganization(ctx, session.ActiveOrganizationID, session.UserID, session.SessionID); !ok {
		return ctx, oops.C(oops.CodeForbidden)
	}

	orgMetadata, err := s.orgRepo.GetOrganizationMetadata(ctx, session.ActiveOrganizationID)
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "error loading organization metadata").Log(ctx, s.logger)
	}
	authCtx.AccountType = orgMetadata.GramAccountType
	authCtx.OrganizationSlug = orgMetadata.Slug

	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	return ctx, nil
}

func (s *Manager) StoreSession(ctx context.Context, session Session) error {
	return s.sessionCache.Store(ctx, session)
}

func (s *Manager) UpdateSession(ctx context.Context, session Session) error {
	return s.sessionCache.Update(ctx, session)
}

func (s *Manager) ClearSession(ctx context.Context, session Session) error {
	return s.sessionCache.Delete(ctx, session)
}
