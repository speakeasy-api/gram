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
	idp := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idpHits.Add(1)
		for _, name := range []string{sessionCookieName, accessCookieName} {
			if _, err := r.Cookie(name); err == nil {
				http.Error(w, "dashboard cookie leaked to IDP", http.StatusBadRequest)
				return
			}
		}
		if r.URL.Path == "/rpc/auth.refresh" {
			for _, name := range []string{sessionCookieName, accessCookieName} {
				http.SetCookie(w, &http.Cookie{Name: name, Value: "forged-credential", Path: "/", Secure: true})
			}
			http.SetCookie(w, &http.Cookie{Name: "idp_session", Value: "fabricated-idp-session", Path: "/idp", Secure: true})
			http.Redirect(w, r, "/idp/continue", http.StatusFound)
			return
		}
		cookie, err := r.Cookie("idp_session")
		if err != nil || cookie.Value != "fabricated-idp-session" {
			http.Error(w, "missing IDP cookie", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, base.String()+"/rpc/auth.callback", http.StatusFound)
	}))
	defer idp.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rpc/auth.login":
			http.SetCookie(w, &http.Cookie{Name: "login_nonce", Value: "fabricated-nonce", Path: "/rpc/auth.callback", Secure: true})
			http.Redirect(w, r, idp.URL+"/rpc/auth.refresh", http.StatusFound)
		case "/rpc/auth.callback":
			cookie, err := r.Cookie("login_nonce")
			if err != nil || cookie.Value != "fabricated-nonce" {
				http.Error(w, "missing login nonce", http.StatusBadRequest)
				return
			}
			callbackHits.Add(1)
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
	client.hc.Jar.SetCookies(base, []*http.Cookie{
		{Name: sessionCookieName, Value: "fabricated-refresh", Path: "/", Secure: true},
		{Name: accessCookieName, Value: "fabricated-access", Path: "/", Secure: true},
	})
	require.NoError(t, client.login(context.Background()))
	require.EqualValues(t, 2, idpHits.Load())
	require.EqualValues(t, 1, callbackHits.Load())
	cookies := client.hc.Jar.Cookies(base.ResolveReference(&url.URL{Path: "/rpc/auth.refresh"}))
	require.Len(t, cookies, 2)
	values := make(map[string]string, len(cookies))
	for _, cookie := range cookies {
		values[cookie.Name] = cookie.Value
	}
	require.Equal(t, map[string]string{
		sessionCookieName: "fabricated-refresh",
		accessCookieName:  "fabricated-access",
	}, values, "IDP must not overwrite either dashboard cookie")
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
