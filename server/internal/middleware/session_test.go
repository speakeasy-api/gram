package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestSessionMiddlewareRefreshesAuthenticatedCookie(t *testing.T) {
	t.Parallel()

	var observedSessionID string
	var foundSession bool
	handler := SessionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedSessionID, foundSession = contextvalues.GetSessionTokenFromContext(r.Context())
		contextvalues.RefreshSessionCookie(r.Context(), observedSessionID)
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/rpc/projects.list", nil)
	req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: "session-id"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	require.True(t, foundSession)
	require.Equal(t, "session-id", observedSessionID)
	var refreshed *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == constants.SessionCookie && cookie.Path == "/" {
			refreshed = cookie
		}
	}
	require.NotNil(t, refreshed)
	require.Equal(t, "session-id", refreshed.Value)
	require.Equal(t, constants.SessionCookieMaxAgeSeconds, refreshed.MaxAge)
	require.True(t, refreshed.Secure)
	require.True(t, refreshed.HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, refreshed.SameSite)
}

func TestSessionMiddlewareDoesNotRefreshDifferentSession(t *testing.T) {
	t.Parallel()

	handler := SessionMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		contextvalues.RefreshSessionCookie(r.Context(), "header-session")
	}))

	req := httptest.NewRequest(http.MethodGet, "/rpc/projects.list", nil)
	req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: "cookie-session"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	for _, cookie := range recorder.Result().Cookies() {
		require.NotEqual(t, "header-session", cookie.Value)
	}
}

func TestSessionMiddlewareDoesNotRefreshLogoutCookie(t *testing.T) {
	t.Parallel()

	require.False(t, shouldRefreshSessionCookie("/rpc/auth.logout"))
	require.False(t, shouldRefreshSessionCookie("/rpc/auth.info"))
	require.False(t, shouldRefreshSessionCookie("/rpc/auth.switchScopes"))
	require.False(t, shouldRefreshSessionCookie("/rpc/auth.enterDemo"))

	handler := SessionMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		contextvalues.RefreshSessionCookie(r.Context(), "session-id")
	}))

	req := httptest.NewRequest(http.MethodPost, "/rpc/auth.logout", nil)
	req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: "session-id"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	require.Empty(t, recorder.Result().Cookies())
}
