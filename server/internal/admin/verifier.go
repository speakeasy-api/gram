package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Verifier implements the Goa APIKeyAuth entry point for the admin service.
// It resolves the opaque admin session ID (either passed by Goa or taken
// from the request context by the admin-cookie middleware) to a server-side
// AdminSession, re-validates the stored OAuth access token with the OIDC
// provider on every call, refreshes it if necessary, and populates the request
// context with an AdminAuthContext on success.
type Verifier struct {
	logger   *slog.Logger
	sessions *SessionStore
	oidc     *OIDCClient
	locks    *cache.RedisCacheAdapter
}

func NewVerifier(logger *slog.Logger, sessions *SessionStore, oidc *OIDCClient, locks *cache.RedisCacheAdapter) *Verifier {
	return &Verifier{
		logger:   logger.With(attr.SlogComponent("adminauth")),
		sessions: sessions,
		oidc:     oidc,
		locks:    locks,
	}
}

// Authorize is the entry point that Goa-generated admin endpoints call via
// the Auther interface on the admin service.
func (v *Verifier) Authorize(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	if scheme == nil || scheme.Name != constants.AdminAuthSecurityScheme {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "unsupported security scheme").LogError(ctx, v.logger)
	}

	if key == "" {
		// Goa does not extract HTTP cookies into APIKey payloads natively.
		// The cookie value is forwarded by AdminSessionMiddleware.
		key, _ = contextvalues.GetAdminSessionTokenFromContext(ctx)
	}
	if key == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	session, err := v.sessions.Get(ctx, key)
	if err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	accessToken, err := v.sessions.DecryptAccessToken(session)
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "decrypt admin session").LogError(ctx, v.logger)
	}

	if NeedsRefresh(session.AccessTokenExpiresAt) {
		session, accessToken, err = v.refreshSession(ctx, session)
		switch {
		case errors.Is(err, errAdminSessionRefreshRequired):
			return ctx, oops.C(oops.CodeUnauthorized).LogError(ctx, v.logger, attr.SlogError(err))
		case err != nil:
			return ctx, oops.E(oops.CodeUnexpected, err, "refresh admin session").LogError(ctx, v.logger)
		}
	}

	info, err := v.oidc.Userinfo(ctx, accessToken)
	switch {
	case errors.Is(err, ErrOIDCUnauthenticated), errors.Is(err, ErrAdminDomainNotAllowed):
		_ = v.sessions.Delete(ctx, session.SessionID)
		return ctx, oops.C(oops.CodeUnauthorized).LogError(ctx, v.logger, attr.SlogError(err))
	case err != nil:
		return ctx, oops.E(oops.CodeUnexpected, err, "validate admin session with oidc provider").LogError(ctx, v.logger)
	}

	if info.OIDCSubject != session.OIDCSubject {
		// Token belongs to a different user than the cached session —
		// treat as hostile and invalidate the session immediately.
		_ = v.sessions.Delete(ctx, session.SessionID)
		return ctx, oops.C(oops.CodeUnauthorized).LogError(ctx, v.logger)
	}

	authCtx := &contextvalues.AdminAuthContext{
		SessionID:   session.SessionID,
		Email:       session.Email,
		OIDCSubject: session.OIDCSubject,
		Name:        session.Name,
		HD:          session.HD,
	}
	ctx = contextvalues.SetAdminAuthContext(ctx, authCtx)

	v.logger.InfoContext(ctx, "admin auth check passed",
		attr.SlogAuthScheme(constants.AdminAuthSecurityScheme),
		attr.SlogAuthUserEmail(session.Email),
	)

	return ctx, nil
}

var errAdminSessionRefreshRequired = errors.New("admin session requires reauthentication")

const (
	adminRefreshUpstreamTimeout = 10 * time.Second
	adminRefreshLockTTL         = 12 * time.Second
	adminRefreshWaitBudget      = adminRefreshLockTTL + adminRefreshUpstreamTimeout + time.Second
	adminRefreshWaitPoll        = 200 * time.Millisecond
	adminRefreshReleaseTimeout  = 5 * time.Second
)

func adminSessionRefreshLockKey(sessionID string) string {
	return "adminSession:refresh:" + sessionID
}

// refreshSession serializes refresh-token grants per admin session. Refresh
// tokens may be single-use, so waiters adopt the holder's persisted token pair
// instead of presenting the same refresh token again.
func (v *Verifier) refreshSession(ctx context.Context, session Session) (Session, string, error) {
	lockKey := adminSessionRefreshLockKey(session.SessionID)
	owner := uuid.NewString()
	held, err := v.locks.AcquireLease(ctx, lockKey, owner, adminRefreshLockTTL)
	if err != nil {
		return session, "", fmt.Errorf("acquire admin refresh lock: %w", err)
	}
	if !held {
		return v.awaitRefreshedSession(ctx, session.SessionID)
	}
	return v.refreshSessionLocked(ctx, session, lockKey, owner)
}

func (v *Verifier) refreshSessionLocked(ctx context.Context, session Session, lockKey, owner string) (Session, string, error) {
	defer o11y.LogDefer(ctx, v.logger, func() error {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), adminRefreshReleaseTimeout)
		defer cancel()
		return v.locks.ReleaseLease(releaseCtx, lockKey, owner)
	})

	// The caller's snapshot predates lock acquisition. Re-read so a refresh that
	// completed while this request was acquiring is adopted rather than replayed.
	current, err := v.sessions.Get(ctx, session.SessionID)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return session, "", errAdminSessionRefreshRequired
	}
	if err != nil {
		return session, "", fmt.Errorf("re-read admin session before refresh: %w", err)
	}
	if !NeedsRefresh(current.AccessTokenExpiresAt) {
		accessToken, err := v.sessions.DecryptAccessToken(current)
		return current, accessToken, err
	}

	refreshToken, err := v.sessions.DecryptRefreshToken(current)
	if err != nil {
		return current, "", fmt.Errorf("decrypt admin refresh token: %w", err)
	}
	if refreshToken == "" {
		deleted, err := v.sessions.DeleteIfUnchanged(ctx, current)
		if err != nil {
			return current, "", fmt.Errorf("delete unrefreshable admin session: %w", err)
		}
		if !deleted {
			return v.awaitRefreshedSession(ctx, current.SessionID)
		}
		return current, "", errAdminSessionRefreshRequired
	}

	renewed, err := v.locks.RenewLease(ctx, lockKey, owner, adminRefreshLockTTL)
	if err != nil {
		return current, "", fmt.Errorf("renew admin refresh lease before provider call: %w", err)
	}
	if !renewed {
		return v.awaitRefreshedSession(ctx, current.SessionID)
	}

	refreshCtx, cancel := context.WithTimeout(ctx, adminRefreshUpstreamTimeout)
	defer cancel()
	tok, refreshErr := v.oidc.Refresh(refreshCtx, refreshToken)
	renewed, err = v.locks.RenewLease(ctx, lockKey, owner, adminRefreshLockTTL)
	if err != nil {
		return current, "", fmt.Errorf("renew admin refresh lease after provider call: %w", err)
	}
	if !renewed {
		return v.awaitRefreshedSession(ctx, current.SessionID)
	}
	if refreshErr != nil {
		if !isInvalidGrant(refreshErr) {
			return current, "", fmt.Errorf("refresh oidc access token: %w", refreshErr)
		}
		deleted, err := v.sessions.DeleteIfUnchanged(ctx, current)
		if err != nil {
			return current, "", fmt.Errorf("delete rejected admin session: %w", err)
		}
		if deleted {
			return current, "", errAdminSessionRefreshRequired
		}

		current, err = v.sessions.Get(ctx, current.SessionID)
		if errors.Is(err, redisCache.ErrCacheMiss) {
			return current, "", errAdminSessionRefreshRequired
		}
		if err != nil {
			return current, "", fmt.Errorf("reload admin session after rejected refresh: %w", err)
		}
		if NeedsRefresh(current.AccessTokenExpiresAt) {
			return current, "", errors.New("admin session changed but still requires refresh")
		}
		accessToken, err := v.sessions.DecryptAccessToken(current)
		if err != nil {
			return current, "", fmt.Errorf("decrypt concurrently refreshed admin access token: %w", err)
		}
		return current, accessToken, nil
	}
	current, err = v.sessions.UpdateTokens(ctx, current, tok.AccessToken, tok.RefreshToken, tok.Expiry)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return current, "", errAdminSessionRefreshRequired
	}
	if err != nil {
		return current, "", fmt.Errorf("persist refreshed admin session: %w", err)
	}
	accessToken, err := v.sessions.DecryptAccessToken(current)
	if err != nil {
		return current, "", fmt.Errorf("decrypt persisted admin access token: %w", err)
	}
	return current, accessToken, nil
}

func (v *Verifier) awaitRefreshedSession(ctx context.Context, sessionID string) (Session, string, error) {
	waitCtx, cancel := context.WithTimeout(ctx, adminRefreshWaitBudget)
	defer cancel()
	ticker := time.NewTicker(adminRefreshWaitPoll)
	defer ticker.Stop()

	for {
		current, err := v.sessions.Get(waitCtx, sessionID)
		switch {
		case errors.Is(err, redisCache.ErrCacheMiss):
			return current, "", errAdminSessionRefreshRequired
		case err != nil:
			return current, "", fmt.Errorf("poll admin session during refresh: %w", err)
		case !NeedsRefresh(current.AccessTokenExpiresAt):
			accessToken, err := v.sessions.DecryptAccessToken(current)
			return current, accessToken, err
		}

		lockKey := adminSessionRefreshLockKey(sessionID)
		owner := uuid.NewString()
		held, err := v.locks.AcquireLease(waitCtx, lockKey, owner, adminRefreshLockTTL)
		if err != nil {
			return current, "", fmt.Errorf("reacquire admin refresh lock: %w", err)
		}
		if held {
			return v.refreshSessionLocked(waitCtx, current, lockKey, owner)
		}

		select {
		case <-waitCtx.Done():
			var session Session
			return session, "", fmt.Errorf("wait for concurrent admin session refresh: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}
