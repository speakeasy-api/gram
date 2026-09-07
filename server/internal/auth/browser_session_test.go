package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newBrowserSessionService(t *testing.T) (*Service, *miniredis.Miniredis, http.Handler) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	provider := testenv.NewTracerProvider(t)
	manager := sessions.NewManager(testenv.NewLogger(t), provider, nil, client, cache.SuffixNone, nil, nil, nil)
	service := &Service{sessions: manager, siteOrigin: "https://dashboard.example.com", logger: testenv.NewLogger(t), tracer: provider.Tracer("auth-test")}
	mux := goahttp.NewMuxer()
	Attach(mux, service)
	return service, mr, mux
}

func browserRequest(handler http.Handler, path, origin, secret, access string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if secret != "" {
		req.AddCookie(&http.Cookie{Name: refreshCookieName, Value: secret})
	}
	if access != "" {
		req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: access})
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestBrowserSessionOriginValidation(t *testing.T) {
	t.Parallel()
	service, _, handler := newBrowserSessionService(t)
	for _, path := range []string{"/auth/session/refresh", "/auth/session/logout"} {
		for _, origin := range []string{"", "null", "https://attacker.example.com", service.siteOrigin + ".attacker.example.com", service.siteOrigin + "/", "http://dashboard.example.com"} {
			recorder := browserRequest(handler, path, origin, "unused", "")
			require.Equal(t, http.StatusForbidden, recorder.Code, "%s %q: %s", path, origin, recorder.Body.String())
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			require.Empty(t, recorder.Result().Cookies())
		}
	}
	recorder := browserRequest(handler, "/auth/session/refresh", service.siteOrigin, "", "access-is-not-refresh")
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestBrowserRefreshAndExpiredAccessLogout(t *testing.T) {
	t.Parallel()
	service, mr, handler := newBrowserSessionService(t)
	require.NoError(t, service.sessions.StoreSession(t.Context(), sessions.Session{SessionID: "access"}))
	secret, _, err := service.sessions.CreateRefreshSession(t.Context(), "access")
	require.NoError(t, err)
	mr.FastForward(sessions.AccessLifetime + time.Second)
	recorder := browserRequest(handler, "/auth/session/refresh", service.siteOrigin, secret, "access")
	require.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())
	require.Empty(t, recorder.Body.String())
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	access := recorder.Header().Get(constants.SessionHeader)
	require.NotEmpty(t, access)
	require.NotEqual(t, "access", access)
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 2)
	for _, cookie := range cookies {
		require.Empty(t, cookie.Domain)
		require.True(t, cookie.Secure)
		require.True(t, cookie.HttpOnly)
		require.Positive(t, cookie.MaxAge)
		if cookie.Name == refreshCookieName {
			require.Equal(t, secret, cookie.Value)
			require.Equal(t, sessionCookiePath, cookie.Path)
			require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
			require.InDelta(t, sessions.RefreshIdleLifetime.Seconds(), cookie.MaxAge, 2)
		} else {
			require.Equal(t, constants.SessionCookie, cookie.Name)
			require.Equal(t, access, cookie.Value)
			require.Equal(t, "/", cookie.Path)
			require.LessOrEqual(t, cookie.MaxAge, 600)
		}
	}
	mr.FastForward(sessions.AccessLifetime + time.Second)
	recorder = browserRequest(handler, "/auth/session/logout", service.siteOrigin, secret, access)
	require.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	for _, cookie := range recorder.Result().Cookies() {
		require.Equal(t, -1, cookie.MaxAge)
		require.Empty(t, cookie.Value)
	}
	require.Len(t, recorder.Result().Cookies(), 2)
	require.Empty(t, mr.Keys())
	recorder = browserRequest(handler, "/auth/session/refresh", service.siteOrigin, secret, access)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestBrowserCookiesPreserveSupportDeadline(t *testing.T) {
	t.Parallel()
	deadline := time.Now().Add(45 * time.Second)
	recorder := httptest.NewRecorder()
	middleware := sessionTransportMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeBrowserSession(r.Context(), "refresh", sessions.Session{SessionID: "access", ExpiresAt: deadline, SupportExpiresAt: deadline})
	}))
	middleware.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/rpc/auth.callback", nil))
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Len(t, recorder.Result().Cookies(), 2)
	for _, cookie := range recorder.Result().Cookies() {
		require.LessOrEqual(t, cookie.MaxAge, 45)
		require.WithinDuration(t, deadline, cookie.Expires, time.Second)
	}
}
