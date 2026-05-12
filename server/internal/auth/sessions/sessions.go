package sessions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// UserResolver provides identity-layer operations that Authenticate needs.
// Implemented by the identity.Resolver to avoid a circular import.
type UserResolver interface {
	HasAccessToOrganization(ctx context.Context, organizationID, userID string) (*Organization, string, bool)
	IsAdmin(ctx context.Context, userID string) bool
	GetUserInfo(ctx context.Context, userID string) (*CachedUserInfo, bool, error)
	InvalidateUserInfoCache(ctx context.Context, userID string) error
}

type Manager struct {
	logger       *slog.Logger
	tracer       trace.Tracer
	sessionCache cache.TypedCacheObject[Session]
	umClient     *usermanagement.Client // for RevokeSession only
	orgRepo      *orgRepo.Queries
	billingRepo  billing.Repository
	identity     UserResolver
}

func NewManager(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	redisClient *redis.Client,
	suffix cache.Suffix,
	umClient *usermanagement.Client,
	billingRepo billing.Repository,
	identity UserResolver,
) *Manager {
	logger = logger.With(attr.SlogComponent("sessions"))

	return &Manager{
		logger:       logger,
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/auth/sessions"),
		sessionCache: cache.NewTypedObjectCache[Session](logger.With(attr.SlogCacheNamespace("session")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
		umClient:     umClient,
		orgRepo:      orgRepo.New(db),
		billingRepo:  billingRepo,
		identity:     identity,
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
	authCtx.IsAdmin = s.identity.IsAdmin(ctx, session.UserID)

	if session.ActiveOrganizationID == "" {
		ctx = contextvalues.SetAuthContext(ctx, authCtx)
		return ctx, nil
	}

	_, email, ok := s.identity.HasAccessToOrganization(ctx, session.ActiveOrganizationID, session.UserID)
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
	// Look up the full cached session to retrieve the WorkOS session ID
	// before deleting it.
	if stored, err := s.sessionCache.Get(ctx, SessionCacheKey(session.SessionID)); err == nil {
		session = stored
	}

	// Revoke the WorkOS AuthKit session so the user is prompted to sign in
	// again on next login rather than being auto-authenticated.
	if session.WorkOSSessionID != "" && s.umClient != nil {
		if err := s.umClient.RevokeSession(ctx, usermanagement.RevokeSessionOpts{
			SessionID: session.WorkOSSessionID,
		}); err != nil {
			// Non-fatal: the Gram session is still cleared, and the WorkOS
			// session will expire naturally.
			s.logger.ErrorContext(ctx, "failed to revoke WorkOS session", attr.SlogError(err))
		}
	}

	err := s.sessionCache.Delete(ctx, session)
	if err != nil {
		return fmt.Errorf("clear session: %w", err)
	}

	return nil
}

// GetUserInfo delegates to the identity resolver.
func (s *Manager) GetUserInfo(ctx context.Context, userID string) (*CachedUserInfo, bool, error) {
	return s.identity.GetUserInfo(ctx, userID)
}

// HasAccessToOrganization delegates to the identity resolver.
func (s *Manager) HasAccessToOrganization(ctx context.Context, organizationID, userID string) (*Organization, string, bool) {
	return s.identity.HasAccessToOrganization(ctx, organizationID, userID)
}

// InvalidateUserInfoCache delegates to the identity resolver.
func (s *Manager) InvalidateUserInfoCache(ctx context.Context, userID string) error {
	return s.identity.InvalidateUserInfoCache(ctx, userID)
}
