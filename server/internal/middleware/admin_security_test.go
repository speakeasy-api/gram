package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminCORS_AllowsConfiguredOrigin(t *testing.T) {
	t.Parallel()

	called := false
	handler := AdminCORS([]string{"https://admin.speakeasy.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://gram-admin.speakeasy.com/admin/organizations.list", nil)
	req.Header.Set("Origin", "https://admin.speakeasy.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "https://admin.speakeasy.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
	require.Contains(t, rec.Header().Get("Vary"), "Origin")
}

func TestAdminCORS_OmitsOriginHeaderForDisallowedOrigin(t *testing.T) {
	t.Parallel()

	handler := AdminCORS([]string{"https://admin.speakeasy.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://gram-admin.speakeasy.com/admin/organizations.list", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestAdminCORS_PreflightShortCircuits(t *testing.T) {
	t.Parallel()

	called := false
	handler := AdminCORS([]string{"https://admin.speakeasy.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodOptions, "https://gram-admin.speakeasy.com/admin/auth.logout", nil)
	req.Header.Set("Origin", "https://admin.speakeasy.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.False(t, called)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestAdminOriginCheck_AllowsSafeMethods(t *testing.T) {
	t.Parallel()

	called := false
	handler := AdminOriginCheck([]string{"https://admin.speakeasy.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(method, "https://gram-admin.speakeasy.com/admin/organizations.list", nil)
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		require.True(t, called)
		called = false
	}
}

func TestAdminOriginCheck_RejectsUnsafeMethodWithDisallowedOrigin(t *testing.T) {
	t.Parallel()

	called := false
	handler := AdminOriginCheck([]string{"https://admin.speakeasy.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "https://gram-admin.speakeasy.com/admin/auth.logout", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.False(t, called)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAdminOriginCheck_AllowsUnsafeMethodWithAllowedOrigin(t *testing.T) {
	t.Parallel()

	called := false
	handler := AdminOriginCheck([]string{"https://admin.speakeasy.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "https://gram-admin.speakeasy.com/admin/auth.logout", nil)
	req.Header.Set("Origin", "https://admin.speakeasy.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.True(t, called)
}

func TestAdminOriginCheck_FallsBackToReferer(t *testing.T) {
	t.Parallel()

	called := false
	handler := AdminOriginCheck([]string{"https://admin.speakeasy.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "https://gram-admin.speakeasy.com/admin/auth.logout", nil)
	req.Header.Set("Referer", "https://admin.speakeasy.com/gram/organizations?cursor=abc")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.True(t, called)
}

func TestAdminOriginCheck_RejectsMissingOriginAndReferer(t *testing.T) {
	t.Parallel()

	handler := AdminOriginCheck([]string{"https://admin.speakeasy.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodPost, "https://gram-admin.speakeasy.com/admin/auth.logout", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAdminCookieAttributes_RewritesAdminCookies(t *testing.T) {
	t.Parallel()

	handler := AdminCookieAttributes(true, ".speakeasy.com")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "gram_admin=abc123; HttpOnly; SameSite=Lax; Path=/")
		w.Header().Add("Set-Cookie", "gram_admin_login_state=xyz; HttpOnly; SameSite=Lax; Path=/")
		w.Header().Add("Set-Cookie", "irrelevant=keepme; HttpOnly")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://gram-admin.speakeasy.com/admin/auth.login", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 3)

	byName := map[string]*http.Cookie{}
	for _, c := range cookies {
		byName[c.Name] = c
	}

	require.Equal(t, ".speakeasy.com", byName["gram_admin"].Domain)
	require.Equal(t, http.SameSiteNoneMode, byName["gram_admin"].SameSite)
	require.True(t, byName["gram_admin"].Secure)
	require.True(t, byName["gram_admin"].HttpOnly)

	require.Equal(t, ".speakeasy.com", byName["gram_admin_login_state"].Domain)
	require.Equal(t, http.SameSiteNoneMode, byName["gram_admin_login_state"].SameSite)
	require.True(t, byName["gram_admin_login_state"].Secure)

	// Untouched.
	require.Empty(t, byName["irrelevant"].Domain)
	require.Equal(t, "keepme", byName["irrelevant"].Value)
}

func TestAdminCookieAttributes_PassThroughWhenCrossSiteDisabled(t *testing.T) {
	t.Parallel()

	handler := AdminCookieAttributes(false, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "gram_admin=abc123; HttpOnly; SameSite=Lax; Path=/")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://gram-admin.speakeasy.com/admin/auth.login", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("Set-Cookie")
	require.Equal(t, "gram_admin=abc123; HttpOnly; SameSite=Lax; Path=/", got)
	require.NotContains(t, got, "Domain=")
}

func TestAdminCookieAttributes_NoDomainWhenEmpty(t *testing.T) {
	t.Parallel()

	handler := AdminCookieAttributes(true, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "gram_admin=abc123; HttpOnly; SameSite=Lax; Path=/")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://localhost:8083/admin/auth.login", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("Set-Cookie")
	require.Contains(t, got, "SameSite=None")
	require.Contains(t, got, "Secure")
	require.NotContains(t, got, "Domain=")
}

func TestRewriteAdminCookie_LeavesNonAdminCookieUntouched(t *testing.T) {
	t.Parallel()

	in := "session=abc; HttpOnly; SameSite=Lax; Path=/"
	require.Equal(t, in, rewriteAdminCookie(in, ".speakeasy.com"))
}

func TestRewriteAdminCookie_AddsSecureWhenMissing(t *testing.T) {
	t.Parallel()

	out := rewriteAdminCookie("gram_admin=abc; HttpOnly; Path=/", ".speakeasy.com")
	require.Contains(t, out, "Secure")
	require.Contains(t, out, "SameSite=None")
	require.Contains(t, out, "Domain=.speakeasy.com")
}
