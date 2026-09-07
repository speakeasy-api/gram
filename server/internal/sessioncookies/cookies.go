// Package sessioncookies writes the host-only cookies shared by browser login entry points.
package sessioncookies

import (
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

const RefreshCookieName = "gram_refresh"
const RefreshCookiePath = "/rpc/auth.refresh"
const LogoutCookiePath = "/rpc/auth.logout"

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

// WriteBrowserSessionCookies emits access and endpoint-scoped refresh cookies.
// Call only after CreateRefreshSession or Refresh succeeds.
func WriteBrowserSessionCookies(w http.ResponseWriter, secret string, session sessions.Session) {
	expires := time.Now().Add(sessions.RefreshIdleLifetime)
	if !session.SupportExpiresAt.IsZero() && session.SupportExpiresAt.Before(expires) {
		expires = session.SupportExpiresAt
	}
	// RPC names use dots, so /rpc/auth is not a matching cookie path.
	// Duplicate the same secret only onto the two endpoints that consume it.
	for _, path := range []string{RefreshCookiePath, LogoutCookiePath} {
		http.SetCookie(w, browserCookie(RefreshCookieName, secret, path, expires, http.SameSiteStrictMode))
	}
	http.SetCookie(w, browserCookie(constants.SessionCookie, session.SessionID, "/", session.ExpiresAt, http.SameSiteLaxMode))
	w.Header().Set(constants.SessionHeader, session.SessionID)
	w.Header().Set("Cache-Control", "no-store")
}

// ClearRefreshCookies removes both endpoint-scoped copies of the refresh cookie.
func ClearRefreshCookies(w http.ResponseWriter) {
	for _, path := range []string{RefreshCookiePath, LogoutCookiePath} {
		http.SetCookie(w, browserCookie(RefreshCookieName, "", path, time.Unix(1, 0), http.SameSiteStrictMode))
	}
}
