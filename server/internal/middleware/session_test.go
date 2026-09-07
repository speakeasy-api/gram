package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestSessionMiddlewareDoesNotRenewAuthenticatedCookie(t *testing.T) {
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
	require.Empty(t, recorder.Result().Cookies())
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

	require.Empty(t, recorder.Result().Cookies())
}

func TestSessionMiddlewareDoesNotRefreshLogoutCookie(t *testing.T) {
	t.Parallel()

	handler := SessionMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		contextvalues.RefreshSessionCookie(r.Context(), "session-id")
	}))

	req := httptest.NewRequest(http.MethodPost, "/rpc/auth.logout", nil)
	req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: "session-id"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	require.Empty(t, recorder.Result().Cookies())
}

func TestSessionMiddlewareNeverAuthenticatesRefreshCookie(t *testing.T) {
	t.Parallel()
	var token string
	var found bool
	handler := SessionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, found = contextvalues.GetSessionTokenFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/auth/session/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "gram_refresh", Value: "fabricated-refresh"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	require.False(t, found)
	require.Empty(t, token)
	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Empty(t, recorder.Result().Cookies())
}
