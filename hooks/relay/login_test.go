package relay

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLoginRoundtrip drives the full browser sign-in without a real browser:
// the intercepted opener parses the callback URL and delivers a minted key, and
// the flow must cache it and mark the machine established.
func TestLoginRoundtrip(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_LOGIN_TIMEOUT_SECONDS", "10")
	os.Unsetenv("GRAM_HOOKS_API_KEY")
	os.Unsetenv("GRAM_API_KEY")

	orig := openBrowser
	t.Cleanup(func() { openBrowser = orig })
	openBrowser = func(target string) {
		u, err := url.Parse(target)
		if err != nil {
			return
		}
		callback := u.Query().Get("cli_callback_url")
		require.Equal(t, "hooks", u.Query().Get("key_scope"))
		cb, err := url.Parse(callback)
		if err != nil {
			return
		}
		q := cb.Query()
		q.Set("api_key", "minted-key-123")
		q.Set("project", "acme")
		q.Set("email", "dev@example.com")
		q.Set("organization_id", "org-9")
		cb.RawQuery = q.Encode()
		go func() {
			resp, err := http.Get(cb.String())
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	}

	cfg := Config{ServerURL: "https://app.example.test", ProjectSlug: "acme", OrgID: "org-9", Nonblocking: false}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	require.NoError(t, NewRelay(cfg).Login(ctx, true))

	values, err := readAuthFile(authFile)
	require.NoError(t, err)
	require.Equal(t, "minted-key-123", values["api_key"])
	require.Equal(t, "acme", values["project"])
	require.Equal(t, "dev@example.com", values["email"])
	require.Equal(t, "org-9", values["org"])
	require.Equal(t, "https://app.example.test", values["server_url"])
	require.True(t, authEstablished())
}

// TestLoginRejectsMismatchedState ensures a callback carrying the wrong state
// token is refused so a stray localhost request cannot inject a key.
func TestLoginRejectsMismatchedState(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_LOGIN_TIMEOUT_SECONDS", "2")
	os.Unsetenv("GRAM_HOOKS_API_KEY")
	os.Unsetenv("GRAM_API_KEY")

	orig := openBrowser
	t.Cleanup(func() { openBrowser = orig })
	openBrowser = func(target string) {
		u, _ := url.Parse(target)
		cb, _ := url.Parse(u.Query().Get("cli_callback_url"))
		q := cb.Query()
		q.Set("state", "wrong-state")
		q.Set("api_key", "attacker-key")
		cb.RawQuery = q.Encode()
		go func() {
			resp, err := http.Get(cb.String())
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	}

	cfg := Config{ServerURL: "https://app.example.test", ProjectSlug: "acme", OrgID: "", Nonblocking: false}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	err := NewRelay(cfg).Login(ctx, true)
	require.Error(t, err, "a mismatched state token must not complete sign-in")

	values, _ := readAuthFile(authFile)
	require.Empty(t, values["api_key"])
}
