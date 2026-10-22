package app

import (
	"encoding/json"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/profile"
)

// callbackClient keeps the test from leaving idle connections behind, which
// would hold up the listener's graceful shutdown.
var callbackClient = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

var loginRedirectPattern = regexp.MustCompile(`0;url=([^"]+)"`)

// fakeVerifyServer answers keys.verify for victimKey only and records every
// key it was asked to verify.
func fakeVerifyServer(t *testing.T, victimKey string) (*httptest.Server, func() []string) {
	t.Helper()

	var mu sync.Mutex
	var verified []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc/keys.verify" {
			http.NotFound(w, r)
			return
		}

		key := r.Header.Get("Gram-Key")
		mu.Lock()
		verified = append(verified, key)
		mu.Unlock()

		if key != victimKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"organization": map[string]any{"id": "org-victim", "name": "Victim Org", "slug": "victim-org"},
			"projects":     []map[string]any{{"id": "proj-victim", "name": "Victim Project", "slug": "victim-project"}},
			"scopes":       []string{"producer"},
		})
	}))
	t.Cleanup(srv.Close)

	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), verified...)
	}
}

// callbackURLFromRedirect reads the redirect file the browser opener received
// and returns the CLI callback URL embedded in the sign-in URL.
func callbackURLFromRedirect(t *testing.T, target string) *url.URL {
	t.Helper()

	page, err := os.ReadFile(filepath.Clean(target))
	require.NoError(t, err)
	m := loginRedirectPattern.FindStringSubmatch(string(page))
	require.NotNil(t, m)

	authURL, err := url.Parse(html.UnescapeString(m[1]))
	require.NoError(t, err)

	cb, err := url.Parse(authURL.Query().Get("cli_callback_url"))
	require.NoError(t, err)
	return cb
}

func sendCallback(t *testing.T, cb url.URL, state, apiKey, project string) int {
	t.Helper()

	q := url.Values{}
	if state != "" {
		q.Set("state", state)
	}
	q.Set("api_key", apiKey)
	q.Set("project", project)
	cb.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, cb.String(), nil)
	require.NoError(t, err)
	resp, err := callbackClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	return resp.StatusCode
}

// TestAuthenticateNewProfile_OnlyBoundCallbackBecomesCurrentProfile races
// forged callbacks — with no state and with a guessed state — ahead of the
// dashboard's real callback. The forged keys must never be verified or saved;
// the bound callback's key must become the current profile.
func TestAuthenticateNewProfile_OnlyBoundCallbackBecomesCurrentProfile(t *testing.T) {
	t.Parallel()

	const victimKey = "victim-key"
	srv, verifiedKeys := fakeVerifyServer(t, victimKey)
	apiURL, err := url.Parse(srv.URL)
	require.NoError(t, err)

	profilePath := filepath.Join(t.TempDir(), "profile.json")
	profileName := profileNameFromURL(srv.URL)

	openBrowser := func(target string) error {
		cb := callbackURLFromRedirect(t, target)
		state := cb.Query().Get("state")
		require.NotEmpty(t, state, "callback URL must carry the login state")

		// The listener is already serving and buffers the accepted result, so
		// the callbacks can be delivered before the login starts waiting.
		require.Equal(t, http.StatusBadRequest, sendCallback(t, *cb, "", "attacker-key", "attacker-project"))
		require.Equal(t, http.StatusBadRequest, sendCallback(t, *cb, "guessed-state", "attacker-key", "attacker-project"))
		require.Equal(t, http.StatusOK, sendCallback(t, *cb, state, victimKey, "victim-project"))
		return nil
	}

	err = authenticateNewProfile(
		t.Context(),
		slog.New(slog.DiscardHandler),
		profileName,
		srv.URL,
		"https://app.example.test",
		api.NewKeysClientFromURL(apiURL),
		profilePath,
		openBrowser,
	)
	require.NoError(t, err)

	require.Equal(t, []string{victimKey}, verifiedKeys(), "only the bound callback's key may be verified")

	current, err := profile.Load(profilePath)
	require.NoError(t, err)
	require.NotNil(t, current)
	require.Equal(t, profileName, current.Name)
	require.Equal(t, victimKey, current.Secret)
	require.Equal(t, "victim-project", current.DefaultProjectSlug)
	require.Equal(t, "victim-org", current.Org.Slug)
}
