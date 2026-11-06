package cataloglocalclient

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSync(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, stage, target, email string }{
		{name: "success"},
		{name: "selected user", email: "alternate@example.test"},
		{name: "missing user", email: "absent@example.test", stage: "missing"},
		{name: "remote login redirect", stage: "login", target: "https://example.test/authorize"},
		{name: "wrong login path", stage: "login", target: "/unexpected"},
		{name: "remote callback", stage: "authorize", target: "https://example.test/admin/auth.callback?state=state&code=code"},
		{name: "wrong callback path", stage: "authorize", target: "/unexpected?state=state&code=code"},
		{name: "wrong state", stage: "authorize", target: "/admin/auth.callback?state=wrong&code=code"},
		{name: "remote final redirect", stage: "callback", target: "https://example.test/"},
		{name: "unauthorized sync", stage: "sync"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var adminURL string
			var synced atomic.Bool
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/authorize", r.URL.Path)
				assert.Empty(t, r.Cookies(), "Admin cookies must not reach the mock provider, even on the same host")
				switch r.Method {
				case http.MethodGet:
					assert.Equal(t, "state", r.URL.Query().Get("state"))
					assert.Equal(t, adminURL+"/admin/auth.callback", r.URL.Query().Get("redirect_uri"))
					w.Header().Set("Content-Type", "text/html")
					_, _ = fmt.Fprint(w, `<form method="POST" action="/authorize"><input name="state_token" value="signed&amp;token"><input name="user_email" value="eng@speakeasyapi.dev"></form><form method="POST" action="/authorize"><input name="state_token" value="signed&amp;token"><input name="user_email" value="alternate@example.test"></form>`)
				case http.MethodPost:
					assert.NoError(t, r.ParseForm())
					email := tc.email
					if email == "" {
						email = DefaultEmail
					}
					assert.Equal(t, email, r.PostForm.Get("user_email"))
					assert.Equal(t, "signed&token", r.PostForm.Get("state_token"))
					target := adminURL + "/admin/auth.callback?state=state&code=code"
					if tc.stage == "authorize" {
						target = tc.target
						if strings.HasPrefix(target, "/") {
							target = adminURL + target
						}
					}
					http.Redirect(w, r, target, http.StatusFound)
				default:
					t.Errorf("unexpected method %s", r.Method)
				}
			}))
			defer provider.Close()
			admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/admin/auth.login":
					http.SetCookie(w, &http.Cookie{Name: "login-state", Value: "state", Path: "/"})
					target := provider.URL + "/authorize?" + url.Values{"state": {"state"}, "redirect_uri": {adminURL + "/admin/auth.callback"}}.Encode()
					if tc.stage == "login" {
						target = tc.target
					}
					http.Redirect(w, r, target, http.StatusFound)
				case "/admin/auth.callback":
					cookie, err := r.Cookie("login-state")
					assert.NoError(t, err)
					if cookie != nil {
						assert.Equal(t, "state", cookie.Value)
					}
					assert.Equal(t, "code", r.URL.Query().Get("code"))
					http.SetCookie(w, &http.Cookie{Name: "admin-session", Value: "verified", Path: "/"})
					target := "/"
					if tc.stage == "callback" {
						target = tc.target
					}
					http.Redirect(w, r, target, http.StatusFound)
				case "/admin/catalog.sync":
					synced.Store(true)
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					assert.Equal(t, adminURL, r.Header.Get("Origin"))
					body, err := io.ReadAll(r.Body)
					assert.NoError(t, err)
					assert.Equal(t, "{}", string(body))
					cookie, err := r.Cookie("admin-session")
					assert.NoError(t, err)
					if cookie != nil {
						assert.Equal(t, "verified", cookie.Value)
					}
					if tc.stage == "sync" {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"created":["example"],"conflicts":[]}`)
				default:
					t.Errorf("unexpected Admin request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer admin.Close()
			adminURL = admin.URL
			result, err := Sync(t.Context(), admin.URL, provider.URL, tc.email)
			if tc.stage != "" {
				require.Error(t, err)
				require.Equal(t, tc.stage == "sync", synced.Load())
				return
			}
			require.NoError(t, err)
			require.True(t, synced.Load())
			require.Equal(t, []string{"example"}, result.Created)
		})
	}
}

func TestLoopbackOnly(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "https://example.test", "http://192.168.1.1", "http://127.0.0.1.evil.test", "http://user@127.0.0.1", "http://127.0.0.1/path", "http://127.0.0.1?x=y", "http://127.0.0.1#fragment", "file:///tmp/local"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			_, err := Sync(t.Context(), raw, "http://127.0.0.1:1", "")
			require.Error(t, err)
			_, err = Sync(t.Context(), "http://127.0.0.1:1", raw, "")
			require.Error(t, err)
		})
	}
	for _, raw := range []string{"http://localhost:8084", "http://127.0.0.1:8084", "http://[::1]:8084"} {
		_, err := localOrigin(raw)
		require.NoError(t, err)
	}
}

func TestLocalClientTimeoutExceedsServerOperationDeadline(t *testing.T) {
	t.Parallel()
	client, err := localClient()
	require.NoError(t, err)
	t.Cleanup(client.CloseIdleConnections)
	// CatalogSyncHandler permits two minutes after the request reaches Admin.
	require.Greater(t, client.Timeout, 2*time.Minute)
}
