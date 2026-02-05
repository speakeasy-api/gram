package sessions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type localEnvFile map[string]struct {
	UserEmail   string  `json:"user_email"`
	DisplayName *string `json:"display_name"`
	Admin       bool    `json:"admin"`
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
	userRepo               *userRepo.Queries
	pylon                  *pylon.Pylon
	posthog                *posthog.Posthog // posthog metrics will no-op if the dependency is not provided
	billingRepo            billing.Repository
}

func NewManager(logger *slog.Logger, db *pgxpool.Pool, redisClient *redis.Client, suffix cache.Suffix, speakeasyServerAddress string, speakeasySecretKey string, pylon *pylon.Pylon, posthog *posthog.Posthog, billingRepo billing.Repository) *Manager {
	logger = logger.With(attr.SlogComponent("sessions"))

	return &Manager{
		logger:                 logger.With(attr.SlogComponent("sessions")),
		sessionCache:           cache.NewTypedObjectCache[Session](logger.With(attr.SlogCacheNamespace("session")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		userInfoCache:          cache.NewTypedObjectCache[CachedUserInfo](logger.With(attr.SlogCacheNamespace("user_info")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		localEnvFile:           localEnvFile{},
		unsafeLocal:            false,
		speakeasyServerAddress: speakeasyServerAddress,
		speakeasySecretKey:     speakeasySecretKey,
		orgRepo:                orgRepo.New(db),
		userRepo:               userRepo.New(db),
		pylon:                  pylon,
		posthog:                posthog,
		billingRepo:            billingRepo,
	}
}

func (s *Manager) IsUnsafeLocalDevelopment() bool {
	return s.unsafeLocal
}

func (s *Manager) Authenticate(ctx context.Context, key string, canStubAuth bool) (context.Context, error) {
	var stubLocalAuth = func() (string, error) {
		stubbable := canStubAuth && s.unsafeLocal
		if !stubbable {
			return "", oops.C(oops.CodeUnauthorized)
		}

		return s.PopulateLocalDevDefaultAuthSession(ctx)
	}

	if key == "" {
		// This may have been set via cookie from http middleware, GOA does not support natively
		key, _ = contextvalues.GetSessionTokenFromContext(ctx)
	}

	session, err := s.sessionCache.Get(ctx, SessionCacheKey(key))
	if err != nil {
		key, err = stubLocalAuth()
		if err != nil {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		session, err = s.sessionCache.Get(ctx, SessionCacheKey(key))
		if err != nil {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
	}

	authCtx := &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		UserID:               session.UserID,
		ExternalUserID:       "",
		ProjectID:            nil,
		OrganizationSlug:     "",
		Email:                nil,
		AccountType:          "",
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	}

	if session.ActiveOrganizationID == "" {
		ctx = contextvalues.SetAuthContext(ctx, authCtx)
		return ctx, nil
	}

	_, email, ok := s.HasAccessToOrganization(ctx, session.ActiveOrganizationID, session.UserID, session.SessionID)
	if !ok {
		return ctx, oops.C(oops.CodeForbidden)
	}

	orgMetadata, err := mv.DescribeOrganization(ctx, s.logger, s.orgRepo, s.billingRepo, session.ActiveOrganizationID)
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "error getting organization metadata").Log(ctx, s.logger)
	}
	if orgMetadata.DisabledAt.Valid {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "this organization is disabled, please reach out to support@speakeasy.com for more information").Log(ctx, s.logger)
	}

	authCtx.AccountType = orgMetadata.GramAccountType
	authCtx.OrganizationSlug = orgMetadata.Slug
	authCtx.Email = &email

	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	return ctx, nil
}

func (s *Manager) AuthenticateWithCookie(ctx context.Context) (context.Context, error) {
	return s.Authenticate(ctx, "", false)
}

func (s *Manager) Billing() billing.Repository {
	return s.billingRepo
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
