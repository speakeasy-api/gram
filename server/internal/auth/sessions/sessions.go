package sessions

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/speakeasyclient"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type Manager struct {
	logger                 *slog.Logger
	tracer                 trace.Tracer
	sessionCache           cache.TypedCacheObject[Session]
	userInfoCache          cache.TypedCacheObject[CachedUserInfo]
	speakeasyServerAddress string
	speakeasySecretKey     string
	httpClient             *guardian.HTTPClient
	orgRepo                *orgRepo.Queries
	userRepo               *userRepo.Queries
	pylon                  *pylon.Pylon
	posthog                *posthog.Posthog // posthog metrics will no-op if the dependency is not provided
	billingRepo            billing.Repository
	workos                 *workos.Client
	speakeasyClient        *speakeasyclient.Client
}

func NewManager(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	guardianPolicy *guardian.Policy,
	db *pgxpool.Pool,
	redisClient *redis.Client,
	suffix cache.Suffix,
	speakeasyServerAddress string,
	speakeasySecretKey string,
	pylon *pylon.Pylon,
	posthog *posthog.Posthog,
	billingRepo billing.Repository,
	workos *workos.Client,
	speakeasyClient *speakeasyclient.Client,
) *Manager {
	logger = logger.With(attr.SlogComponent("sessions"))
	httpClient := guardianPolicy.PooledClient()
	httpClient.Timeout = 10 * time.Second

	return &Manager{
		logger:                 logger.With(attr.SlogComponent("sessions")),
		tracer:                 tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/auth/sessions"),
		sessionCache:           cache.NewTypedObjectCache[Session](logger.With(attr.SlogCacheNamespace("session")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		userInfoCache:          cache.NewTypedObjectCache[CachedUserInfo](logger.With(attr.SlogCacheNamespace("user_info")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		speakeasyServerAddress: speakeasyServerAddress,
		speakeasySecretKey:     speakeasySecretKey,
		httpClient:             httpClient,
		orgRepo:                orgRepo.New(db),
		userRepo:               userRepo.New(db),
		pylon:                  pylon,
		posthog:                posthog,
		billingRepo:            billingRepo,
		workos:                 workos,
		speakeasyClient:        speakeasyClient,
	}
}

func (s *Manager) Authenticate(ctx context.Context, key string) (context.Context, error) {
	if key == "" {
		// This may have been set via cookie from http middleware, GOA does not support natively
		key, _ = contextvalues.GetSessionTokenFromContext(ctx)
	}

	session, err := s.sessionCache.Get(ctx, SessionCacheKey(key))
	if err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	authCtx := &contextvalues.AuthContext{
		SessionID:             &session.SessionID,
		ActiveOrganizationID:  session.ActiveOrganizationID,
		UserID:                session.UserID,
		ExternalUserID:        "",
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		APIKeyID:              "",
		IsAdmin:               false,
	}

	// Populate IsAdmin from the cached user info so the access manager can gate
	// the RBAC scope-override header to admins only. A cache miss leaves IsAdmin
	// false, which is the safe default.
	if userInfo, err := s.userInfoCache.Get(ctx, UserInfoCacheKey(session.UserID)); err == nil {
		authCtx.IsAdmin = userInfo.Admin
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
	authCtx.HasActiveSubscription = orgMetadata.HasActiveSubscription
	authCtx.Whitelisted = orgMetadata.Whitelisted
	authCtx.OrganizationSlug = orgMetadata.Slug
	authCtx.Email = &email

	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	return ctx, nil
}

func (s *Manager) AuthenticateWithCookie(ctx context.Context) (context.Context, error) {
	return s.Authenticate(ctx, "")
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
