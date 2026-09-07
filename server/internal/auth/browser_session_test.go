package auth

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/sessioncookies"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newBrowserSessionService(t *testing.T) (*Service, *miniredis.Miniredis, http.Handler) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	provider := testenv.NewTracerProvider(t)
	manager := sessions.NewManager(testenv.NewLogger(t), provider, nil, client, cache.SuffixNone, nil, nil, browserSessionIdentity{})
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
	for _, path := range []string{refreshCookiePath, logoutCookiePath} {
		for _, origin := range []string{"", "null", "https://attacker.example.com", service.siteOrigin + ".attacker.example.com", service.siteOrigin + "/", "http://dashboard.example.com"} {
			recorder := browserRequest(handler, path, origin, "unused", "")
			require.Equal(t, http.StatusForbidden, recorder.Code, "%s %q: %s", path, origin, recorder.Body.String())
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			require.Empty(t, recorder.Result().Cookies())
		}
	}
	recorder := browserRequest(handler, refreshCookiePath, service.siteOrigin, "", "access-is-not-refresh")
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestBrowserRefreshAndExpiredAccessLogout(t *testing.T) {
	t.Parallel()
	service, mr, handler := newBrowserSessionService(t)
	require.NoError(t, service.sessions.StoreSession(t.Context(), sessions.Session{SessionID: "access"}))
	secret, _, err := service.sessions.CreateRefreshSession(t.Context(), "access")
	require.NoError(t, err)
	mr.FastForward(sessions.AccessLifetime + time.Second)
	recorder := browserRequest(handler, refreshCookiePath, service.siteOrigin, secret, "access")
	require.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())
	require.Empty(t, recorder.Body.String())
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	access := recorder.Header().Get(constants.SessionHeader)
	require.NotEmpty(t, access)
	require.NotEqual(t, "access", access)
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 3)
	for _, cookie := range cookies {
		require.Empty(t, cookie.Domain)
		require.True(t, cookie.Secure)
		require.True(t, cookie.HttpOnly)
		require.Positive(t, cookie.MaxAge)
		if cookie.Name == refreshCookieName {
			require.Equal(t, secret, cookie.Value)
			require.Contains(t, []string{refreshCookiePath, logoutCookiePath}, cookie.Path)
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
	recorder = browserRequest(handler, logoutCookiePath, service.siteOrigin, secret, access)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	for _, cookie := range recorder.Result().Cookies() {
		require.Equal(t, -1, cookie.MaxAge)
		require.Empty(t, cookie.Value)
	}
	require.Len(t, recorder.Result().Cookies(), 3)
	require.Empty(t, mr.Keys())
	recorder = browserRequest(handler, refreshCookiePath, service.siteOrigin, secret, access)
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
	require.Len(t, recorder.Result().Cookies(), 3)
	for _, cookie := range recorder.Result().Cookies() {
		require.LessOrEqual(t, cookie.MaxAge, 45)
		require.WithinDuration(t, deadline, cookie.Expires, time.Second)
	}
}

// The browser transport tests need only organization-less session identities.
type browserSessionIdentity struct{}

func (browserSessionIdentity) HasAccessToOrganization(context.Context, string, string) (*sessions.Organization, string, bool) {
	return nil, "", false
}
func (browserSessionIdentity) IsAdmin(context.Context, string) bool { return false }
func (browserSessionIdentity) GetUserInfo(context.Context, string) (*sessions.CachedUserInfo, bool, error) {
	return &sessions.CachedUserInfo{}, true, nil
}
func (browserSessionIdentity) InvalidateUserInfoCache(context.Context, string) error { return nil }

func TestBrowserRefreshCookieEndpointIsolation(t *testing.T) {
	t.Parallel()
	service, _, handler := newBrowserSessionService(t)
	require.NoError(t, service.sessions.StoreSession(t.Context(), sessions.Session{SessionID: "access"}))
	secret, session, err := service.sessions.CreateRefreshSession(t.Context(), "access")
	require.NoError(t, err)
	initial := httptest.NewRecorder()
	sessioncookies.WriteBrowserSessionCookies(initial, secret, session)
	refreshed := browserRequest(handler, refreshCookiePath, service.siteOrigin, secret, session.SessionID)
	require.Equal(t, http.StatusNoContent, refreshed.Code)
	for _, response := range []*httptest.ResponseRecorder{initial, refreshed} {
		require.Len(t, response.Result().Cookies(), 3)
		jar, err := cookiejar.New(nil)
		require.NoError(t, err)
		base, err := url.Parse("https://api.example.com/rpc/auth.callback")
		require.NoError(t, err)
		jar.SetCookies(base, response.Result().Cookies())
		for _, path := range []string{refreshCookiePath, logoutCookiePath, "/rpc/auth.info", "/rpc/auth.switchScopes", "/rpc/chat.send", "/chat", "/rpc/auth.refreshOther", "/rpc/auth.logoutOther"} {
			endpoint := base.ResolveReference(&url.URL{Path: path})
			request := httptest.NewRequest(http.MethodPost, endpoint.String(), nil)
			refreshCount := 0
			for _, cookie := range jar.Cookies(endpoint) {
				request.AddCookie(cookie)
				if cookie.Name == refreshCookieName {
					refreshCount++
				}
			}
			if path == refreshCookiePath || path == logoutCookiePath {
				require.Equal(t, 1, refreshCount, path)
				require.Equal(t, secret, requestCookie(request, refreshCookieName), path)
			} else {
				require.Zero(t, refreshCount, path)
			}
			require.NotEmpty(t, requestCookie(request, constants.SessionCookie), path)
		}
		for _, host := range []string{"https://other.example.com", "https://sub.api.example.com", "http://api.example.com"} {
			endpoint, err := url.Parse(host + refreshCookiePath)
			require.NoError(t, err)
			require.Empty(t, jar.Cookies(endpoint), host)
		}
		logout := browserRequest(handler, logoutCookiePath, service.siteOrigin, secret, response.Header().Get(constants.SessionHeader))
		require.Equal(t, http.StatusOK, logout.Code)
		jar.SetCookies(base.ResolveReference(&url.URL{Path: logoutCookiePath}), logout.Result().Cookies())
		for _, path := range []string{refreshCookiePath, logoutCookiePath, "/rpc/auth.info"} {
			require.Empty(t, jar.Cookies(base.ResolveReference(&url.URL{Path: path})), path)
		}
	}
}

func TestBrowserLogoutHeaderOnlyCompatibility(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		header  string
		cookie  bool
		origin  string
		expired bool
		status  int
	}{
		{name: "valid header", header: "access", status: http.StatusOK},
		{name: "invalid header", header: "invalid", status: http.StatusUnauthorized},
		{name: "missing header", status: http.StatusUnauthorized},
		{name: "expired header", header: "access", expired: true, status: http.StatusUnauthorized},
		{name: "cookie needs origin", header: "access", cookie: true, status: http.StatusForbidden},
		{name: "empty cookie needs origin", cookie: true, status: http.StatusForbidden},
		{name: "untrusted origin", header: "access", origin: "https://attacker.example.com", status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service, mr, handler := newBrowserSessionService(t)
			require.NoError(t, service.sessions.StoreSession(t.Context(), sessions.Session{SessionID: "access"}))
			_, _, err := service.sessions.CreateRefreshSession(t.Context(), "access")
			require.NoError(t, err)
			if test.expired {
				mr.FastForward(sessions.AccessLifetime + time.Second)
			}
			request := httptest.NewRequest(http.MethodPost, logoutCookiePath, nil)
			request.Header.Set(constants.SessionHeader, test.header)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.cookie {
				request.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: test.header})
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			require.Equal(t, test.status, recorder.Code, recorder.Body.String())
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			if test.status == http.StatusOK {
				require.Len(t, recorder.Result().Cookies(), 3)
				require.Empty(t, mr.Keys())
			} else {
				require.Empty(t, recorder.Result().Cookies())
				require.NotEmpty(t, mr.Keys())
			}
		})
	}
}

func TestBrowserSessionRejectsDuplicateOriginAndLegacyRoutes(t *testing.T) {
	t.Parallel()
	service, _, handler := newBrowserSessionService(t)
	for _, path := range []string{refreshCookiePath, logoutCookiePath} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.Header.Add("Origin", service.siteOrigin)
		request.Header.Add("Origin", service.siteOrigin)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusForbidden, recorder.Code)
		require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		require.Empty(t, recorder.Result().Cookies())
	}
	for _, path := range []string{"/auth/session/refresh", "/auth/session/logout"} {
		recorder := browserRequest(handler, path, service.siteOrigin, "unused", "")
		require.Equal(t, http.StatusNotFound, recorder.Code)
	}
}
