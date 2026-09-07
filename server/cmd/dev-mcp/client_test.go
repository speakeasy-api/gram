package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAPIClientRefreshesExpiredAccessAndKeepsRefreshCookieScoped(t *testing.T) {
	t.Parallel()
	var logins, refreshes, calls atomic.Int32
	const origin = "https://dashboard.example.test"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rpc/auth.login":
			logins.Add(1)
			http.Redirect(w, r, "/rpc/auth.callback", http.StatusFound)
		case "/rpc/auth.callback":
			for _, path := range []string{"/rpc/auth.refresh", "/rpc/auth.logout"} {
				http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "fabricated-refresh", Path: path, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
			}
			http.Redirect(w, r, "/dashboard-not-running", http.StatusFound)
		case "/rpc/auth.refresh":
			if r.Method != http.MethodPost || r.Header.Get("Origin") != origin {
				http.Error(w, "invalid refresh request", http.StatusForbidden)
				return
			}
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil || cookie.Value != "fabricated-refresh" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Gram-Session", fmt.Sprintf("fabricated-access-%d", refreshes.Add(1)))
			w.WriteHeader(http.StatusNoContent)
		case "/rpc/example":
			if _, err := r.Cookie(sessionCookieName); err == nil {
				http.Error(w, "refresh cookie leaked to RPC", http.StatusBadRequest)
				return
			}
			count := calls.Add(1)
			if count == 2 { // The second call observes expiration; retry uses a fresh access token.
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			want := "fabricated-access-1"
			if count > 2 {
				want = "fabricated-access-2"
			}
			if r.Header.Get("Gram-Session") != want {
				http.Error(w, "wrong access header", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{ "ok": true }`)
		default:
			http.Error(w, "dashboard must not be followed", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	require.NoError(t, err)
	client := newAPIClient(base, origin, true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for range 2 {
		body, err := client.call(context.Background(), http.MethodGet, "/rpc/example", nil, "", nil)
		require.NoError(t, err)
		require.JSONEq(t, `{"ok":true}`, string(body))
	}
	require.EqualValues(t, 1, logins.Load())
	require.EqualValues(t, 2, refreshes.Load())
	require.EqualValues(t, 3, calls.Load())
}

func TestAPIClientRequiresRefreshAccessHeader(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	require.NoError(t, err)
	client := newAPIClient(base, server.URL, true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	token, status, err := client.refresh(context.Background())
	require.ErrorContains(t, err, "missing Gram-Session header")
	require.Empty(t, token)
	require.Equal(t, http.StatusNoContent, status)
}

func TestAPIClientDoesNotForwardAccessAcrossOrigins(t *testing.T) {
	t.Parallel()
	var receivedAccess atomic.Bool
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAccess.Store(r.Header.Get("Gram-Session") != "")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	base, err := url.Parse(source.URL)
	require.NoError(t, err)
	client := newAPIClient(base, source.URL, true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	client.accessToken = "fabricated-access"
	_, status, err := client.doOnce(context.Background(), http.MethodGet, "/rpc/example", nil, "", nil)
	require.ErrorContains(t, err, "cross-origin API redirect blocked")
	require.Zero(t, status)
	require.False(t, receivedAccess.Load())
}

func TestAPIClientBlocksCrossOriginRefreshRedirects(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			var hits atomic.Int32
			target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer target.Close()
			source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := r.Cookie(sessionCookieName); err != nil {
					http.Error(w, "missing refresh cookie", http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, target.URL+"/rpc/auth.refresh", status)
			}))
			defer source.Close()
			base, err := url.Parse(source.URL)
			require.NoError(t, err)
			client := newAPIClient(base, source.URL, true, slog.New(slog.NewTextHandler(io.Discard, nil)))
			client.hc.Jar.SetCookies(base, []*http.Cookie{{Name: sessionCookieName, Value: "fabricated-refresh", Path: "/rpc/auth.refresh", Secure: true}})
			_, _, err = client.refresh(context.Background())
			require.ErrorContains(t, err, "cross-origin API redirect blocked")
			require.Zero(t, hits.Load(), "same-host different-port redirect must not be requested")
		})
	}
}

func TestAPIClientLoginCrossesIDPOriginWithoutDashboardCredentials(t *testing.T) {
	t.Parallel()
	var idpHits, callbackHits atomic.Int32
	var base *url.URL
	// Include arbitrary cookies, not only the two credential names. The real
	// auth middleware sets gram_auth_nonce at / and binds it to the callback.
	names := []string{sessionCookieName, accessCookieName, "gram_auth_nonce", "dashboard_preference"}
	idp := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idpHits.Add(1)
		if r.URL.Path == "/authorize" {
			if len(r.Cookies()) != 0 {
				http.Error(w, "dashboard cookies leaked to IDP", http.StatusBadRequest)
				return
			}
			for _, name := range names {
				http.SetCookie(w, &http.Cookie{Name: name, Value: "idp-" + name, Path: "/", Secure: true})
			}
			http.Redirect(w, r, "/continue", http.StatusFound)
			return
		}
		if r.URL.Path != "/continue" {
			http.Error(w, "unexpected IDP path", http.StatusBadRequest)
			return
		}
		for _, name := range names {
			cookie, err := r.Cookie(name)
			if err != nil || cookie.Value != "idp-"+name {
				http.Error(w, "missing independent IDP cookie", http.StatusBadRequest)
				return
			}
		}
		http.Redirect(w, r, base.String()+"/rpc/auth.callback", http.StatusFound)
	}))
	defer idp.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rpc/auth.login":
			http.SetCookie(w, &http.Cookie{Name: "gram_auth_nonce", Value: "dashboard-gram_auth_nonce", Path: "/", Secure: true})
			http.Redirect(w, r, idp.URL+"/authorize", http.StatusFound)
		case "/rpc/auth.callback":
			for _, name := range names {
				cookie, err := r.Cookie(name)
				if err != nil || cookie.Value != "dashboard-"+name {
					http.Error(w, "dashboard cookie or callback nonce overwritten", http.StatusBadRequest)
					return
				}
			}
			callbackHits.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "gram_auth_nonce", MaxAge: -1, Path: "/", Secure: true})
			http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "new-refresh", Path: "/rpc/auth.refresh", Secure: true})
			http.Redirect(w, r, idp.URL+"/dashboard-not-running", http.StatusFound)
		default:
			http.Error(w, "unexpected path", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	var err error
	base, err = url.Parse(server.URL)
	require.NoError(t, err)
	client := newAPIClient(base, server.URL, true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Broad-path cookies also exercise net/http's initial Cookie header copy.
	for _, name := range names {
		client.hc.Jar.SetCookies(base, []*http.Cookie{{Name: name, Value: "dashboard-" + name, Path: "/", Secure: true}})
	}
	require.NoError(t, client.login(context.Background()))
	require.EqualValues(t, 2, idpHits.Load())
	require.EqualValues(t, 1, callbackHits.Load())
	dashboardCookies := client.hc.Jar.Cookies(base)
	require.Len(t, dashboardCookies, len(names)-1)
	for _, cookie := range dashboardCookies {
		require.NotEqual(t, "gram_auth_nonce", cookie.Name, "callback must clear its nonce")
		require.Equal(t, "dashboard-"+cookie.Name, cookie.Value)
	}
	refreshCookies := client.hc.Jar.Cookies(base.ResolveReference(&url.URL{Path: "/rpc/auth.refresh"}))
	require.Len(t, refreshCookies, len(names))
	require.Equal(t, sessionCookieName, refreshCookies[0].Name)
	require.Equal(t, "new-refresh", refreshCookies[0].Value, "callback must store its path-scoped session")
	idpURL, err := url.Parse(idp.URL)
	require.NoError(t, err)
	idpCookies := client.hc.Jar.Cookies(idpURL)
	require.Len(t, idpCookies, len(names))
	for _, cookie := range idpCookies {
		require.Equal(t, "idp-"+cookie.Name, cookie.Value, "callback must not change IDP cookies")
	}
}

func TestAPIClientRedirectOriginPolicy(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://API.example.test")
	require.NoError(t, err)
	client := newAPIClient(base, "https://api.example.test", true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tt := range []struct {
		url  string
		want bool
	}{
		{"https://api.example.test:0443/redirect", true},
		{"https://api.example.test:8443/redirect", false},
		{"http://api.example.test:443/redirect", false},
		{"https://other.example.test/redirect", false},
	} {
		target, err := url.Parse(tt.url)
		require.NoError(t, err)
		err = client.hc.CheckRedirect(&http.Request{URL: target}, []*http.Request{{URL: base}})
		if tt.want {
			require.NoError(t, err, tt.url)
		} else {
			require.ErrorContains(t, err, "cross-origin API redirect blocked", tt.url)
		}
	}
}

func TestAuthCookieJarOriginPartitions(t *testing.T) {
	t.Parallel()
	jar := &authCookieJar{}
	parse := func(raw string) *url.URL {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		return u
	}
	base := parse("https://api.example.test/login")
	names := []string{sessionCookieName, accessCookieName, "gram_auth_nonce", "arbitrary_cookie"}
	for _, name := range names {
		jar.SetCookies(base, []*http.Cookie{{Name: name, Value: "dashboard", Path: "/", Domain: ".example.test", Secure: true}})
	}
	for _, raw := range []string{
		"https://api.example.test:8443/login",
		"http://api.example.test:443/login",
		"https://idp.example.test/login",
	} {
		target := parse(raw)
		require.Empty(t, jar.Cookies(target), "no dashboard cookies may reach %s", raw)
		for _, name := range names {
			jar.SetCookies(target, []*http.Cookie{{Name: name, Value: "idp", Path: "/", Domain: ".example.test"}})
		}
		cookies := jar.Cookies(target)
		require.Len(t, cookies, len(names))
		for _, cookie := range cookies {
			require.Equal(t, "idp", cookie.Value)
		}
		// Deletion is also a write and must not delete dashboard cookies.
		for _, name := range names {
			jar.SetCookies(target, []*http.Cookie{{Name: name, MaxAge: -1, Path: "/", Domain: ".example.test"}})
		}
		require.Empty(t, jar.Cookies(target))
		cookies = jar.Cookies(base)
		require.Len(t, cookies, len(names))
		for _, cookie := range cookies {
			require.Equal(t, "dashboard", cookie.Value)
		}
	}
	for _, raw := range []string{"https://API.EXAMPLE.TEST:0443/callback", "https://api.example.test:443/"} {
		require.Len(t, jar.Cookies(parse(raw)), len(names), "canonical equivalents must share state")
	}
	for _, raw := range []string{"/relative", "ftp://api.example.test/", "https://user@api.example.test/"} {
		u := parse(raw)
		jar.SetCookies(u, []*http.Cookie{{Name: "invalid", Value: "ignored"}})
		require.Empty(t, jar.Cookies(u))
	}
}

func TestAuthCookieJarPreservesCookieSemantics(t *testing.T) {
	t.Parallel()
	jar := &authCookieJar{}
	base, err := url.Parse("https://api.example.test/rpc/login")
	require.NoError(t, err)
	jar.SetCookies(base, []*http.Cookie{
		{Name: "scoped", Value: "root", Path: "/", Secure: true},
		{Name: "scoped", Value: "rpc", Path: "/rpc", Secure: true},
		{Name: "default_path", Value: "rpc"},
		{Name: "domain", Value: "accepted", Domain: ".example.test", Path: "/"},
		{Name: "invalid_domain", Value: "rejected", Domain: "other.example.test", Path: "/"},
		{Name: "expired", Value: "rejected", Path: "/", Expires: time.Unix(1, 0)},
	})
	for _, tt := range []struct {
		path string
		want []string
	}{
		{"/rpc/callback", []string{"scoped=rpc", "default_path=rpc", "scoped=root", "domain=accepted"}},
		{"/rpc-other", []string{"scoped=root", "domain=accepted"}},
		{"/", []string{"scoped=root", "domain=accepted"}},
	} {
		var got []string
		for _, cookie := range jar.Cookies(base.ResolveReference(&url.URL{Path: tt.path})) {
			got = append(got, cookie.Name+"="+cookie.Value)
		}
		require.Equal(t, tt.want, got, tt.path)
	}
	jar.SetCookies(base, []*http.Cookie{{Name: "scoped", Path: "/rpc", MaxAge: -1}})
	for _, cookie := range jar.Cookies(base) {
		if cookie.Name == "scoped" {
			require.Equal(t, "root", cookie.Value, "deletion must respect path")
		}
	}
}

func TestAuthCookieJarConcurrentUse(t *testing.T) {
	t.Parallel()
	jar := &authCookieJar{}
	for i := range 32 {
		t.Run(fmt.Sprintf("worker%d", i), func(t *testing.T) {
			t.Parallel()
			// Exercise concurrent creation and access of shared and separate jars.
			for _, port := range []int{443, 8443 + i} {
				u, err := url.Parse(fmt.Sprintf("https://api.example.test:%d/", port))
				require.NoError(t, err)
				jar.SetCookies(u, []*http.Cookie{{Name: fmt.Sprintf("cookie%d", i), Value: "value", Path: "/"}})
				require.NotEmpty(t, jar.Cookies(u))
			}
		})
	}
}
