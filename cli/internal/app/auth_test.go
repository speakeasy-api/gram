package app

import (
	"crypto/sha256"
	"encoding/base64"
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

// fakeAuthServer answers keys.verify for victimKey only and cliAuth.redeem
// for codes whose PKCE challenge it knows, and records every key it was asked
// to verify. challenges maps a code to the challenge it was minted for; the
// server reads it at redeem time, so a test can fill it in once the CLI has
// sent its challenge to the browser.
func fakeAuthServer(t *testing.T, victimKey string, challenges *sync.Map) (*httptest.Server, func() []string) {
	t.Helper()

	var mu sync.Mutex
	var verified []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rpc/cliAuth.redeem":
			var body struct {
				Code         string `json:"code"`
				CodeVerifier string `json:"code_verifier"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			challenge, ok := challenges.LoadAndDelete(body.Code)
			sum := sha256.Sum256([]byte(body.CodeVerifier))
			if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": victimKey,
				"user_email":   "victim@example.test",
				"project_slug": "victim-project",
			})
		case "/rpc/keys.verify":
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
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), verified...)
	}
}

// authURLFromRedirect reads the redirect file the browser opener received and
// returns the sign-in URL it forwards to.
func authURLFromRedirect(t *testing.T, target string) *url.URL {
	t.Helper()

	page, err := os.ReadFile(filepath.Clean(target))
	require.NoError(t, err)
	m := loginRedirectPattern.FindStringSubmatch(string(page))
	require.NotNil(t, m)

	authURL, err := url.Parse(html.UnescapeString(m[1]))
	require.NoError(t, err)
	return authURL
}

func sendCallback(t *testing.T, cb url.URL, state, code string) int {
	t.Helper()

	q := url.Values{}
	if state != "" {
		q.Set("state", state)
	}
	q.Set("code", code)
	cb.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, cb.String(), nil)
	require.NoError(t, err)
	resp, err := callbackClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	return resp.StatusCode
}

func authenticateAgainst(t *testing.T, srv *httptest.Server, profilePath string, openBrowser func(string) error) error {
	t.Helper()

	apiURL, err := url.Parse(srv.URL)
	require.NoError(t, err)

	return authenticateNewProfile(
		t.Context(),
		slog.New(slog.DiscardHandler),
		profileNameFromURL(srv.URL),
		srv.URL,
		"https://app.example.test",
		api.NewKeysClientFromURL(apiURL),
		api.NewCliAuthClientFromURL(apiURL),
		profilePath,
		openBrowser,
	)
}

// TestAuthenticateNewProfile_OnlyBoundCallbackBecomesCurrentProfile races
// forged callbacks — with no state and with a guessed state — ahead of the
// dashboard's real callback. The forged codes must never be redeemed; the
// bound callback's code must redeem with this login's verifier and its key
// become the current profile.
func TestAuthenticateNewProfile_OnlyBoundCallbackBecomesCurrentProfile(t *testing.T) {
	t.Parallel()

	const victimKey = "victim-key"
	var challenges sync.Map
	srv, verifiedKeys := fakeAuthServer(t, victimKey, &challenges)
	profilePath := filepath.Join(t.TempDir(), "profile.json")

	openBrowser := func(target string) error {
		authURL := authURLFromRedirect(t, target)
		require.Equal(t, "producer", authURL.Query().Get("key_scope"))
		require.Equal(t, "S256", authURL.Query().Get("code_challenge_method"))
		challenge := authURL.Query().Get("code_challenge")
		require.NotEmpty(t, challenge)
		// The dashboard mints the code for the challenge the CLI sent.
		challenges.Store("victim-code", challenge)

		cb, err := url.Parse(authURL.Query().Get("cli_callback_url"))
		require.NoError(t, err)
		state := cb.Query().Get("state")
		require.NotEmpty(t, state, "callback URL must carry the login state")

		// The listener is already serving and buffers the accepted result, so
		// the callbacks can be delivered before the login starts waiting.
		require.Equal(t, http.StatusBadRequest, sendCallback(t, *cb, "", "attacker-code"))
		require.Equal(t, http.StatusBadRequest, sendCallback(t, *cb, "guessed-state", "attacker-code"))
		require.Equal(t, http.StatusOK, sendCallback(t, *cb, state, "victim-code"))
		return nil
	}

	require.NoError(t, authenticateAgainst(t, srv, profilePath, openBrowser))

	require.Equal(t, []string{victimKey}, verifiedKeys(), "only the redeemed key may be verified")

	current, err := profile.Load(profilePath)
	require.NoError(t, err)
	require.NotNil(t, current)
	require.Equal(t, profileNameFromURL(srv.URL), current.Name)
	require.Equal(t, victimKey, current.Secret)
	require.Equal(t, "victim-project", current.DefaultProjectSlug)
	require.Equal(t, "victim-org", current.Org.Slug)
}

// TestAuthenticateNewProfile_CodeForAnotherLoginIsNotRedeemed covers a caller
// that wins the callback race with the right state but a code minted for its
// own challenge. Redeeming it needs this login's verifier, so the login fails
// and no profile is written.
func TestAuthenticateNewProfile_CodeForAnotherLoginIsNotRedeemed(t *testing.T) {
	t.Parallel()

	var challenges sync.Map
	attackerSum := sha256.Sum256([]byte("attacker-verifier"))
	challenges.Store("attacker-code", base64.RawURLEncoding.EncodeToString(attackerSum[:]))
	srv, verifiedKeys := fakeAuthServer(t, "victim-key", &challenges)
	profilePath := filepath.Join(t.TempDir(), "profile.json")

	openBrowser := func(target string) error {
		authURL := authURLFromRedirect(t, target)
		cb, err := url.Parse(authURL.Query().Get("cli_callback_url"))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, sendCallback(t, *cb, cb.Query().Get("state"), "attacker-code"))
		return nil
	}

	require.Error(t, authenticateAgainst(t, srv, profilePath, openBrowser))
	require.Empty(t, verifiedKeys())

	current, err := profile.Load(profilePath)
	require.NoError(t, err)
	require.Nil(t, current)
}
