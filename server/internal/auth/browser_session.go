package auth

import (
	"context"
	"errors"
	"net/http"

	redisCache "github.com/go-redis/cache/v9"
	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessioncookies"
)

const refreshCookieName = sessioncookies.RefreshCookieName
const refreshCookiePath = sessioncookies.RefreshCookiePath
const logoutCookiePath = sessioncookies.LogoutCookiePath

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

func writeBrowserSession(ctx context.Context, secret string, session sessions.Session) {
	transport, ok := ctx.Value(browserSessionKey{}).(browserSessionTransport)
	if !ok {
		return
	}
	sessioncookies.WriteBrowserSessionCookies(transport.writer, secret, session)
}

func (s *Service) Refresh(ctx context.Context) (*gen.RefreshResult, error) {
	transport, err := s.browserTransport(ctx)
	if err != nil {
		return nil, err
	}
	secret := requestCookie(transport.request, refreshCookieName)
	session, err := s.sessions.Refresh(ctx, secret)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "error refreshing browser session")
	}
	writeBrowserSession(ctx, secret, session)
	return &gen.RefreshResult{SessionToken: session.SessionID}, nil
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
	sessioncookies.ClearRefreshCookies(transport.writer)
	// The generated logout response clears the access cookie at /.
	transport.writer.Header().Set("Clear-Site-Data", `"cache", "cookies", "storage"`)
	return nil
}
