package relay

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// forceInteractiveEnv clears the signals loginViable treats as non-interactive
// (CI runners set CI and have no display) so the sign-in flow runs under test.
func forceInteractiveEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	t.Setenv("DISPLAY", ":0")
}

// loginURLFromOpener resolves the sign-in URL the browser would land on. The
// opener must receive a redirect-file path — argv is world-readable, so the
// state-bearing URL itself must never appear there.
func loginURLFromOpener(t *testing.T, target string) *url.URL {
	t.Helper()
	require.False(t, strings.HasPrefix(target, "http"), "the opener must receive a redirect file, not the state-bearing URL")
	b, err := os.ReadFile(target)
	require.NoError(t, err)
	m := regexp.MustCompile(`0;url=([^"]+)"`).FindStringSubmatch(string(b))
	require.NotNil(t, m, "redirect file must carry the sign-in URL")
	u, err := url.Parse(html.UnescapeString(m[1]))
	require.NoError(t, err)
	return u
}

// TestLoginRoundtrip drives the full browser sign-in without a real browser:
// the intercepted opener parses the callback URL and delivers a minted key, and
// the flow must cache it and mark the machine established.
func TestLoginRoundtrip(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_LOGIN_TIMEOUT_SECONDS", "10")
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	forceInteractiveEnv(t)

	orig := openBrowser
	t.Cleanup(func() { openBrowser = orig })
	openBrowser = func(target string) {
		u := loginURLFromOpener(t, target)
		callback := u.Query().Get("cli_callback_url")
		require.Equal(t, "hooks", u.Query().Get("key_scope"))
		require.Equal(t, "post", u.Query().Get("callback_method"))
		cb, err := url.Parse(callback)
		if err != nil {
			return
		}
		form := url.Values{}
		form.Set("api_key", "minted-key-123")
		form.Set("project", "acme")
		form.Set("email", "dev@example.com")
		form.Set("organization_id", "org-9")
		go func() {
			resp, err := http.PostForm(cb.String(), form)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	}

	cfg := Config{ServerURL: "https://app.example.test", ProjectSlug: "acme", OrgID: "org-9", HooksAPIKey: "", BrowserLogin: true, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
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

// TestLoginLegacyGETRoundtrip keeps compatibility with dashboards released
// before callback_method=post, which append credentials to the callback URL.
func TestLoginLegacyGETRoundtrip(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_LOGIN_TIMEOUT_SECONDS", "10")
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	forceInteractiveEnv(t)

	orig := openBrowser
	t.Cleanup(func() { openBrowser = orig })
	openBrowser = func(target string) {
		u := loginURLFromOpener(t, target)
		cb, err := url.Parse(u.Query().Get("cli_callback_url"))
		if err != nil {
			return
		}
		q := cb.Query()
		q.Set("api_key", "legacy-key")
		q.Set("project", "legacy-project")
		cb.RawQuery = q.Encode()
		go func() {
			resp, err := http.Get(cb.String())
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	}

	cfg := Config{ServerURL: "https://app.example.test", ProjectSlug: "acme", OrgID: "", HooksAPIKey: "", BrowserLogin: true, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
	require.NoError(t, NewRelay(cfg).Login(t.Context(), true))

	values, err := readAuthFile(authFile)
	require.NoError(t, err)
	require.Equal(t, "legacy-key", values["api_key"])
	require.Equal(t, "legacy-project", values["project"])
}

func TestLoginDisabledPointsToManualKey(t *testing.T) {
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	cfg := Config{ServerURL: "https://app.example.test", ProjectSlug: "acme", OrgID: "", HooksAPIKey: "org-key", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
	err := NewRelay(cfg).Login(t.Context(), true)
	require.ErrorContains(t, err, "GRAM_HOOKS_API_KEY")
}

// TestLoginRejectsMismatchedState ensures a callback carrying the wrong state
// token is refused so a stray localhost request cannot inject a key.
func TestLoginRejectsMismatchedState(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_LOGIN_TIMEOUT_SECONDS", "2")
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	forceInteractiveEnv(t)

	orig := openBrowser
	t.Cleanup(func() { openBrowser = orig })
	openBrowser = func(target string) {
		u := loginURLFromOpener(t, target)
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

	cfg := Config{ServerURL: "https://app.example.test", ProjectSlug: "acme", OrgID: "", HooksAPIKey: "", BrowserLogin: true, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	err := NewRelay(cfg).Login(ctx, true)
	require.Error(t, err, "a mismatched state token must not complete sign-in")

	values, _ := readAuthFile(authFile)
	require.Empty(t, values["api_key"])
}

// TestLoginRefusesBrokenConfig: a sign-in under an unreadable plugin config
// would mint a key against the default server, not this plugin's workspace.
func TestLoginRefusesBrokenConfig(t *testing.T) {
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	forceInteractiveEnv(t)

	cfg := Config{ServerURL: "https://app.example.test", ProjectSlug: "acme", OrgID: "", HooksAPIKey: "", BrowserLogin: true, Nonblocking: false, DebugLog: "", ConfigPath: "/missing/speakeasy.json", ConfigError: "open /missing/speakeasy.json: no such file or directory"}
	err := NewRelay(cfg).Login(t.Context(), true)
	require.ErrorContains(t, err, "reinstall")
}

// TestMarkAttemptCreatesAuthDir: on a fresh machine the auth directory only
// appears after a successful sign-in, but the cooldown marker for a dismissed
// attempt must record regardless or every session reopens the browser.
func TestMarkAttemptCreatesAuthDir(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "config", "gram", "hooks-auth.env")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)

	l := newLoginFlow(Config{ServerURL: "https://app.example.test", ProjectSlug: "acme", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""})
	l.markAttempt()
	require.FileExists(t, authFile+".login-attempt")
	require.False(t, l.cooldownElapsed(false), "a fresh attempt marker must hold the cooldown")
}
