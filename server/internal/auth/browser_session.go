package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const refreshCookieName = "gram_refresh"
const sessionCookiePath = "/auth/session"

type browserSessionKey struct{}
type browserSessionTransport struct {
	writer  http.ResponseWriter
	request *http.Request
}

// The refresh credential stays in the cookie transport, not generated payloads,
// response bodies, ordinary authentication middleware, or request context tokens.
func sessionTransportMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx := context.WithValue(r.Context(), browserSessionKey{}, browserSessionTransport{writer: w, request: r})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) browserTransport(ctx context.Context) (browserSessionTransport, error) {
	transport, ok := ctx.Value(browserSessionKey{}).(browserSessionTransport)
	if !ok || s.siteOrigin == "" {
		return browserSessionTransport{}, oops.C(oops.CodeForbidden)
	}
	origins := transport.request.Header.Values("Origin")
	if len(origins) != 1 || origins[0] != s.siteOrigin {
		return browserSessionTransport{}, oops.C(oops.CodeForbidden)
	}
	return transport, nil
}

func requestCookie(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func browserCookie(name, value, path string, expires time.Time, sameSite http.SameSite) *http.Cookie {
	maxAge := int(time.Until(expires).Seconds())
	if maxAge <= 0 {
		maxAge = -1
	}
	//nolint:exhaustruct // Set only the security and lifetime attributes; Domain must remain empty (host-only).
	return &http.Cookie{
		Name: name, Value: value, Path: path,
		Secure: true, HttpOnly: true, SameSite: sameSite,
		Expires: expires, MaxAge: maxAge,
	}
}

func writeBrowserSession(ctx context.Context, secret string, session sessions.Session) {
	transport, ok := ctx.Value(browserSessionKey{}).(browserSessionTransport)
	if !ok {
		return
	}
	WriteBrowserSessionCookies(transport.writer, secret, session)
}

// WriteBrowserSessionCookies is shared by the auth and invitation callbacks.
// Call only after CreateRefreshSession or Refresh succeeds.
func WriteBrowserSessionCookies(w http.ResponseWriter, secret string, session sessions.Session) {
	expires := time.Now().Add(sessions.RefreshIdleLifetime)
	if !session.SupportExpiresAt.IsZero() && session.SupportExpiresAt.Before(expires) {
		expires = session.SupportExpiresAt
	}
	http.SetCookie(w, browserCookie(refreshCookieName, secret, sessionCookiePath, expires, http.SameSiteStrictMode))
	http.SetCookie(w, browserCookie(constants.SessionCookie, session.SessionID, "/", session.ExpiresAt, http.SameSiteLaxMode))
	w.Header().Set(constants.SessionHeader, session.SessionID)
	w.Header().Set("Cache-Control", "no-store")
}

func (s *Service) RefreshSession(ctx context.Context) error {
	transport, err := s.browserTransport(ctx)
	if err != nil {
		return err
	}
	secret := requestCookie(transport.request, refreshCookieName)
	session, err := s.sessions.Refresh(ctx, secret)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return oops.C(oops.CodeUnauthorized)
	}
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "error refreshing browser session")
	}
	writeBrowserSession(ctx, secret, session)
	return nil
}

func (s *Service) LogoutSession(ctx context.Context) error {
	transport, err := s.browserTransport(ctx)
	if err != nil {
		return err
	}
	if err := s.sessions.Logout(ctx, requestCookie(transport.request, refreshCookieName), requestCookie(transport.request, constants.SessionCookie)); err != nil {
		return oops.E(oops.CodeUnavailable, err, "error clearing browser session")
	}
	// Also honor header-only clients of this endpoint without requiring live access.
	if access := transport.request.Header.Get(constants.SessionHeader); access != "" {
		if err := s.sessions.Logout(ctx, "", access); err != nil {
			return oops.E(oops.CodeUnavailable, err, "error clearing access session")
		}
	}
	http.SetCookie(transport.writer, browserCookie(refreshCookieName, "", sessionCookiePath, time.Unix(1, 0), http.SameSiteStrictMode))
	http.SetCookie(transport.writer, browserCookie(constants.SessionCookie, "", "/", time.Unix(1, 0), http.SameSiteLaxMode))
	transport.writer.Header().Set("Clear-Site-Data", `"cache", "cookies", "storage"`)
	return nil
}
