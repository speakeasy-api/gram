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
const refreshCookiePath = "/rpc/auth.refresh"
const logoutCookiePath = "/rpc/auth.logout"

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

// WriteBrowserSessionCookies emits access and endpoint-scoped refresh cookies.
// Call only after CreateRefreshSession or Refresh succeeds.
func WriteBrowserSessionCookies(w http.ResponseWriter, secret string, session sessions.Session) {
	expires := time.Now().Add(sessions.RefreshIdleLifetime)
	if !session.SupportExpiresAt.IsZero() && session.SupportExpiresAt.Before(expires) {
		expires = session.SupportExpiresAt
	}
	// RPC names use dots, so /rpc/auth is not a matching cookie path.
	// Duplicate the same secret only onto the two endpoints that consume it.
	for _, path := range []string{refreshCookiePath, logoutCookiePath} {
		http.SetCookie(w, browserCookie(refreshCookieName, secret, path, expires, http.SameSiteStrictMode))
	}
	http.SetCookie(w, browserCookie(constants.SessionCookie, session.SessionID, "/", session.ExpiresAt, http.SameSiteLaxMode))
	w.Header().Set(constants.SessionHeader, session.SessionID)
	w.Header().Set("Cache-Control", "no-store")
}

func (s *Service) Refresh(ctx context.Context) error {
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

func (s *Service) logoutBrowserSession(ctx context.Context) error {
	transport, ok := ctx.Value(browserSessionKey{}).(browserSessionTransport)
	if !ok {
		return oops.C(oops.CodeForbidden)
	}
	_, accessCookieErr := transport.request.Cookie(constants.SessionCookie)
	_, refreshCookieErr := transport.request.Cookie(refreshCookieName)
	access := transport.request.Header.Get(constants.SessionHeader)
	if len(transport.request.Header.Values("Origin")) == 0 && errors.Is(accessCookieErr, http.ErrNoCookie) && errors.Is(refreshCookieErr, http.ErrNoCookie) {
		// Preserve non-browser clients without admitting cookie-authenticated CSRF.
		// Do not let Authenticate fall back to a cookie-derived context token.
		if access == "" {
			return oops.C(oops.CodeUnauthorized)
		}
		if _, err := s.sessions.Authenticate(ctx, access); err != nil {
			return err
		}
	} else if _, err := s.browserTransport(ctx); err != nil {
		return err
	}
	if err := s.sessions.Logout(ctx, requestCookie(transport.request, refreshCookieName), requestCookie(transport.request, constants.SessionCookie)); err != nil {
		return oops.E(oops.CodeUnavailable, err, "error clearing browser session")
	}
	if access != "" {
		if err := s.sessions.Logout(ctx, "", access); err != nil {
			return oops.E(oops.CodeUnavailable, err, "error clearing access session")
		}
	}
	for _, path := range []string{refreshCookiePath, logoutCookiePath} {
		http.SetCookie(transport.writer, browserCookie(refreshCookieName, "", path, time.Unix(1, 0), http.SameSiteStrictMode))
	}
	// The generated logout response clears the access cookie at /.
	transport.writer.Header().Set("Clear-Site-Data", `"cache", "cookies", "storage"`)
	return nil
}
