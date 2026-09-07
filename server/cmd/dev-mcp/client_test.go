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
			http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "fabricated-refresh", Path: "/auth/session", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/dashboard-not-running", http.StatusFound)
		case "/auth/session/refresh":
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
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, status)
	require.False(t, receivedAccess.Load())
}
