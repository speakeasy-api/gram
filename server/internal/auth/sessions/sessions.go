package sessions

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// sessionTokenBytes is the number of random bytes drawn for a session token.
// 32 bytes yields 256 bits of entropy, base64url-encoded to a 43-character
// opaque string.
const sessionTokenBytes = 32

// NewSessionID generates a cryptographically secure, opaque session token.
//
// Session tokens are bearer credentials: possession of the token string is
// sufficient to authenticate as the user (validation is a bare cache lookup in
// Authenticate). They must therefore be unguessable. A v4 UUID carries only 122
// bits of entropy in a recognizable, structured format and is not intended as a
// security token, so we draw 256 bits directly from crypto/rand instead.
func NewSessionID() (string, error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SessionRevoker invalidates an IDP session. Implemented by the WorkOS
// adapter so sessions.Manager doesn't depend on the WorkOS SDK directly.
type SessionRevoker interface {
	RevokeSession(ctx context.Context, sessionID string) error
}

// SessionLister lists a user's IDP sessions.
type SessionLister interface {
	ListSessionIDs(ctx context.Context, idpUserID string) ([]string, error)
}

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
	redis        *redis.Client
	flags        feature.Provider
	idpClient    SessionRevoker
	orgRepo      *orgRepo.Queries
	userRepo     *userRepo.Queries
	billingRepo  billing.Repository
	identity     UserResolver
}

func NewManager(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	redisClient *redis.Client,
	suffix cache.Suffix,
	idpClient SessionRevoker,
	billingRepo billing.Repository,
	identity UserResolver,
) *Manager {
	logger = logger.With(attr.SlogComponent("sessions"))

	return &Manager{
		logger:       logger,
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/auth/sessions"),
		sessionCache: cache.NewTypedObjectCache[Session](logger.With(attr.SlogCacheNamespace("session")), cache.NewRedisCacheAdapter(redisClient), suffix),
		redis:        redisClient,
		flags:        nil,
		idpClient:    idpClient,
		orgRepo:      orgRepo.New(db),
		userRepo:     userRepo.New(db),
		billingRepo:  billingRepo,
		identity:     identity,
	}
}

func validSupportSession(session Session, isAdmin bool, now time.Time) bool {
	return isAdmin && !session.SupportExpiresAt.IsZero() && now.Before(session.SupportExpiresAt) &&
		session.SupportOrganizationID == session.ActiveOrganizationID
}

func (s *Manager) Authenticate(ctx context.Context, key string) (context.Context, error) {
	if key == "" {
		// This may have been set via cookie from http middleware, GOA does not support natively
		key, _ = contextvalues.GetSessionTokenFromContext(ctx)
	}

	session, err := s.sessionCache.Get(ctx, SessionCacheKey(key))
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	if err != nil {
		return ctx, oops.E(oops.CodeUnavailable, err, "error checking auth session").LogError(ctx, s.logger)
	}
	if err := s.checkValidAfter(ctx, session); err != nil {
		return ctx, err
	}

	validatedSupportAdmin := false
	if session.SupportOrganizationID != "" {
		user, userErr := s.userRepo.GetUser(ctx, session.UserID)
		if errors.Is(userErr, pgx.ErrNoRows) {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		if userErr != nil {
			return ctx, oops.E(oops.CodeUnexpected, userErr, "error checking support session user").LogError(ctx, s.logger)
		}
		if !validSupportSession(session, user.Admin, time.Now()) {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		validatedSupportAdmin = true
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
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		IsAdmin:               false,
		SupportOrganizationID: session.SupportOrganizationID,
	}

	if session.ActiveOrganizationID == "" {
		// Organization-less sessions still need identity attributes for
		// request handling and audit attribution.
		userInfo, _, err := s.identity.GetUserInfo(ctx, session.UserID)
		if err == nil {
			email := userInfo.Email
			authCtx.Email = &email
			authCtx.IsAdmin = userInfo.Admin
		}
		if err := s.refreshSession(ctx, session); err != nil {
			return ctx, err
		}
		ctx = contextvalues.WithValidatedGramSession(ctx, authCtx, session.ImpersonatorEmail != "")
		return ctx, nil
	}

	// HasAccessToOrganization calls GetUserInfo internally, which populates
	// the user info cache on a miss. We check IsAdmin AFTER this call so the
	// cache is guaranteed to be warm — avoids a false-negative on cold cache.
	_, email, ok := s.identity.HasAccessToOrganization(ctx, session.ActiveOrganizationID, session.UserID)
	authCtx.IsAdmin = validatedSupportAdmin || s.identity.IsAdmin(ctx, session.UserID)

	if !ok {
		// The shared demo org has no membership rows by design — any
		// authenticated user may hold a session pointed at it. A platform admin
		// may access a foreign org only through a validated support session.
		isDemo := session.ActiveOrganizationID == constants.DemoOrganizationID
		if !isDemo && !validatedSupportAdmin {
			return ctx, oops.C(oops.CodeForbidden)
		}
		// Admin visiting a customer org they don't belong to (or any user in
		// the demo org) — fall back to cached user info for the email the
		// auth context needs.
		if userInfo, _, err := s.identity.GetUserInfo(ctx, session.UserID); err == nil {
			email = userInfo.Email
		}
	}

	orgMetadata, err := mv.DescribeOrganization(ctx, s.logger, s.orgRepo, s.billingRepo, session.ActiveOrganizationID)
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "error getting organization metadata").LogError(ctx, s.logger)
	}
	if orgMetadata.DisabledAt.Valid {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "this organization is disabled, please reach out to support@speakeasy.com for more information").LogError(ctx, s.logger)
	}

	authCtx.AccountType = orgMetadata.GramAccountType
	authCtx.HasActiveSubscription = orgMetadata.HasActiveSubscription
	authCtx.Whitelisted = orgMetadata.Whitelisted
	authCtx.OrganizationSlug = orgMetadata.Slug
	authCtx.Email = &email

	if err := s.refreshSession(ctx, session); err != nil {
		return ctx, err
	}

	ctx = contextvalues.WithValidatedGramSession(ctx, authCtx, session.ImpersonatorEmail != "")
	if validatedSupportAdmin {
		validatedAuthCtx, _ := contextvalues.GetAuthContext(ctx)
		ctx = contextvalues.WithValidatedSupportSession(ctx, validatedAuthCtx)
	}

	return ctx, nil
}

func (s *Manager) refreshSession(ctx context.Context, session Session) error {
	refreshed, err := s.sessionCache.CompareAndSwap(ctx, session, session)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error refreshing session expiry").LogError(ctx, s.logger)
	}
	if !refreshed {
		return oops.C(oops.CodeUnauthorized)
	}
	contextvalues.RefreshSessionCookie(ctx, session.SessionID)
	return nil
}

func (s *Manager) AuthenticateWithCookie(ctx context.Context) (context.Context, error) {
	return s.Authenticate(ctx, "")
}

// IsPlatformAdmin reads the authoritative users.admin and deletion state
// directly from the database. Break-glass authorization must not rely on the
// identity cache because an administrator may have been revoked after that cache was populated.
func (s *Manager) IsPlatformAdmin(ctx context.Context, userID string) (bool, error) {
	user, err := s.userRepo.GetUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get user for platform admin check: %w", err)
	}
	return isCurrentPlatformAdmin(user.Admin, user.DeletedAt.Valid), nil
}

func isCurrentPlatformAdmin(admin, deleted bool) bool {
	return admin && !deleted
}

func (s *Manager) Billing() billing.Repository {
	return s.billingRepo
}

func (s *Manager) GetSession(ctx context.Context, sessionID string) (Session, error) {
	session, err := s.sessionCache.Get(ctx, SessionCacheKey(sessionID))
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	return session, nil
}

// StoreSession stores a session. A session stored again keeps its original
// issuance, so re-storing never lifts it past the sign-out-everywhere epoch;
// one stored for the first time this way has unknown issuance (EpochZero).
// New sessions use StoreSessionIssuedAt with when authentication started.
func (s *Manager) StoreSession(ctx context.Context, session Session) error {
	if err := s.sessionCache.Store(ctx, session); err != nil {
		return fmt.Errorf("store session: %w", err)
	}
	return s.storeIssuedAt(ctx, session, func() error {
		return s.redis.SetNX(ctx, sessionIssuedAtKey(session.SessionID), EpochZero.UnixMicro(), sessionMarkerTTL).Err()
	})
}

// StoreSessionIssuedAt stores a session and records when it was issued, for
// the sign-out-everywhere epoch. Issuance lives beside the session so the
// session's serialized form stays unchanged across versions.
func (s *Manager) StoreSessionIssuedAt(ctx context.Context, session Session, issuedAt time.Time) error {
	if err := s.sessionCache.Store(ctx, session); err != nil {
		return fmt.Errorf("store session: %w", err)
	}
	return s.storeIssuedAt(ctx, session, func() error {
		return s.redis.Set(ctx, sessionIssuedAtKey(session.SessionID), issuedAt.UnixMicro(), sessionMarkerTTL).Err()
	})
}

func (s *Manager) UpdateSession(ctx context.Context, expected, replacement Session) error {
	updated, err := s.sessionCache.CompareAndSwap(ctx, expected, replacement)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	if !updated {
		return errors.New("update session: session changed or no longer exists")
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
	if session.WorkOSSessionID != "" && s.idpClient != nil {
		if err := s.idpClient.RevokeSession(ctx, session.WorkOSSessionID); err != nil {
			// Non-fatal: the Speakeasy session is still cleared, and the WorkOS
			// session will expire naturally.
			s.logger.ErrorContext(ctx, "failed to revoke WorkOS session", attr.SlogError(err))
		}
	}

	err := s.sessionCache.Delete(ctx, session)
	if err != nil {
		return fmt.Errorf("clear session: %w", err)
	}
	if s.redis != nil {
		if err := s.redis.Del(ctx, sessionIssuedAtKey(session.SessionID)).Err(); err != nil {
			s.logger.WarnContext(ctx, "clear session issuance", attr.SlogError(err))
		}
	}

	return nil
}

// GetUserInfo delegates to the identity resolver.
func (s *Manager) GetUserInfo(ctx context.Context, userID string) (*CachedUserInfo, bool, error) {
	info, ok, err := s.identity.GetUserInfo(ctx, userID)
	if err != nil {
		return nil, false, fmt.Errorf("get user info: %w", err)
	}
	return info, ok, nil
}

// HasAccessToOrganization delegates to the identity resolver.
func (s *Manager) HasAccessToOrganization(ctx context.Context, organizationID, userID string) (*Organization, string, bool) {
	return s.identity.HasAccessToOrganization(ctx, organizationID, userID)
}

// InvalidateUserInfoCache delegates to the identity resolver.
func (s *Manager) InvalidateUserInfoCache(ctx context.Context, userID string) error {
	if err := s.identity.InvalidateUserInfoCache(ctx, userID); err != nil {
		return fmt.Errorf("invalidate user info cache: %w", err)
	}
	return nil
}

// SetFlags wires the flag provider gating sign-in enforcement.
func (s *Manager) SetFlags(flags feature.Provider) { s.flags = flags }

// SignInEnforced reports whether sign-in enforcement is on for the
// organization.
func (s *Manager) SignInEnforced(ctx context.Context, organizationID string) bool {
	return feature.SignInEnforced(ctx, s.flags, organizationID)
}

// storeIssuedAt records issuance. It is best effort while enforcement is off
// for the session's organization; the epoch check treats a missing marker as
// issued before any epoch.
func (s *Manager) storeIssuedAt(ctx context.Context, session Session, write func() error) error {
	if s.redis == nil {
		return nil
	}
	err := write()
	if err == nil {
		return nil
	}
	if s.SignInEnforced(ctx, session.ActiveOrganizationID) {
		return fmt.Errorf("store session issuance: %w", err)
	}
	s.logger.WarnContext(ctx, "store session issuance", attr.SlogError(err))
	return nil
}

// sessionMarkerTTL outlives any dashboard session the markers describe.
const sessionMarkerTTL = 400 * 24 * time.Hour

func sessionIssuedAtKey(sessionID string) string { return "sessions:issued_at:" + sessionID }

func userValidAfterKey(userID string) string { return "sessions:valid_after:" + userID }

// raiseValidAfter stores max(current, ARGV[1]) so concurrent writers never
// move the epoch backwards.
var raiseValidAfter = redis.NewScript(`
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
local proposed = tonumber(ARGV[1])
if proposed > current then
  redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
  return proposed
end
redis.call("PEXPIRE", KEYS[1], ARGV[2])
return current
`)

// IssuedAt is when a dashboard session was issued; ok is false for sessions
// stored before issuance was recorded.
func (s *Manager) IssuedAt(ctx context.Context, sessionID string) (time.Time, bool, error) {
	if s.redis == nil {
		return time.Time{}, false, nil
	}
	v, err := s.redis.Get(ctx, sessionIssuedAtKey(sessionID)).Int64()
	if errors.Is(err, redis.Nil) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("get session issuance: %w", err)
	}
	return time.UnixMicro(v), true, nil
}

// EpochZero is the issuance recorded for a session whose real issuance is
// unknown: it predates any sign-out-everywhere epoch.
var EpochZero = time.UnixMicro(0)

// ListUserIDPSessionIDs lists every IDP session a user holds, or nothing when
// the IDP client cannot list them or the user has no IDP identity.
func (s *Manager) ListUserIDPSessionIDs(ctx context.Context, userID string) ([]string, error) {
	lister, ok := s.idpClient.(SessionLister)
	if !ok || lister == nil {
		return nil, nil
	}
	user, err := s.userRepo.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if !user.WorkosID.Valid || user.WorkosID.String == "" {
		return nil, nil
	}
	ids, err := lister.ListSessionIDs(ctx, user.WorkosID.String)
	if err != nil {
		return nil, fmt.Errorf("list idp sessions: %w", err)
	}
	return ids, nil
}

// SessionPredatesEpoch reports whether a session was issued at or before its
// user signed out everywhere, reading both markers in one round trip. A
// session of unknown issuance predates any epoch. Enforced whenever an epoch
// exists, whatever the enforcement flag says.
func (s *Manager) SessionPredatesEpoch(ctx context.Context, userID, sessionID string) (bool, error) {
	if s.redis == nil {
		return false, nil
	}
	values, err := s.redis.MGet(ctx, userValidAfterKey(userID), sessionIssuedAtKey(sessionID)).Result()
	if err != nil {
		return false, fmt.Errorf("get session epoch: %w", err)
	}
	epoch, hasEpoch, err := markerValue(values, 0)
	if err != nil || !hasEpoch {
		return false, err
	}
	issuedAt, known, err := markerValue(values, 1)
	if err != nil {
		return false, err
	}
	return !known || issuedAt <= epoch, nil
}

// IssuedBeforeEpoch reports whether a session issued at issuedAt (known false
// when unrecorded) predates its user's sign-out-everywhere epoch.
func (s *Manager) IssuedBeforeEpoch(ctx context.Context, userID string, issuedAt time.Time, known bool) (bool, error) {
	if s.redis == nil {
		return false, nil
	}
	epoch, err := s.redis.Get(ctx, userValidAfterKey(userID)).Int64()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get session epoch: %w", err)
	}
	return !known || issuedAt.UnixMicro() <= epoch, nil
}

func markerValue(values []any, i int) (int64, bool, error) {
	if i >= len(values) || values[i] == nil {
		return 0, false, nil
	}
	raw, ok := values[i].(string)
	if !ok {
		return 0, false, fmt.Errorf("session marker has type %T", values[i])
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse session marker: %w", err)
	}
	return v, true, nil
}

// checkValidAfter rejects a session issued before its user signed out
// everywhere.
func (s *Manager) checkValidAfter(ctx context.Context, session Session) error {
	predates, err := s.SessionPredatesEpoch(ctx, session.UserID, session.SessionID)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "error checking session validity").LogError(ctx, s.logger)
	}
	if predates {
		return oops.C(oops.CodeUnauthorized)
	}
	return nil
}

// RaiseUserEpoch invalidates every dashboard session the user was issued at or
// before at. The stored epoch only ever moves forward.
func (s *Manager) RaiseUserEpoch(ctx context.Context, userID string, at time.Time) error {
	return EpochStore{redis: s.redis}.RaiseUserEpoch(ctx, userID, at)
}

// EpochStore raises users' sign-out-everywhere epochs, for processes that
// deliver revocations without a session manager.
type EpochStore struct{ redis *redis.Client }

func NewEpochStore(redisClient *redis.Client) EpochStore { return EpochStore{redis: redisClient} }

// RaiseUserEpoch moves the user's epoch forward to at.
func (e EpochStore) RaiseUserEpoch(ctx context.Context, userID string, at time.Time) error {
	if e.redis == nil {
		return errors.New("session epoch store is not configured")
	}
	if err := raiseValidAfter.Run(ctx, e.redis, []string{userValidAfterKey(userID)}, at.UnixMicro(), sessionMarkerTTL.Milliseconds()).Err(); err != nil {
		return fmt.Errorf("raise session epoch: %w", err)
	}
	return nil
}

// RevokeSession ends one identity provider session.
func (s *Manager) RevokeSession(ctx context.Context, idpSessionID string) error {
	if idpSessionID == "" || s.idpClient == nil {
		return nil
	}
	if err := s.idpClient.RevokeSession(ctx, idpSessionID); err != nil {
		return fmt.Errorf("revoke idp session: %w", err)
	}
	return nil
}
