package platformmcp

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
)

// newFinishElsewhereChallenge registers a client that listens at redirectURI
// and stores a challenge ready for the connect page.
func newFinishElsewhereChallenge(t *testing.T, redirectURI string) (*OAuthHTTP, oauthChallenge) {
	t.Helper()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	require.NoError(t, store.RegisterClient(t.Context(), platformoauth.Client{ID: "client-1", Name: "Codex", RedirectURIs: []string{redirectURI}}))
	challenge := oauthChallenge{ID: "challenge-1", ClientID: "client-1", RedirectURI: redirectURI, State: "client-state", CSRFToken: "csrf", OrganizationID: "org-1", Subject: "user:user-1", CreatedAt: time.Now()}
	require.NoError(t, service.cache.Store(t.Context(), challenge))
	return service, challenge
}

func connectGetPage(t *testing.T, service *OAuthHTTP, challenge oauthChallenge) *httptest.ResponseRecorder {
	t.Helper()

	response := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/platform-mcp/connect?state="+challenge.ID, nil))
	require.Equal(t, http.StatusOK, response.Code)
	return response
}

func connectApprove(t *testing.T, service *OAuthHTTP, challenge oauthChallenge, finishElsewhere bool) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{"state": {challenge.ID}, "csrf_token": {challenge.CSRFToken}, "action": {"approve"}}
	if finishElsewhere {
		form.Set("finish_elsewhere", "1")
	}
	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/connect", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(response, request)
	return response
}

func TestOAuthHTTPConnectOffersFinishElsewhereForLoopbackRedirect(t *testing.T) {
	t.Parallel()

	service, challenge := newFinishElsewhereChallenge(t, "http://127.0.0.1:3000/callback")
	body := connectGetPage(t, service, challenge).Body.String()
	require.Contains(t, body, `name="finish_elsewhere"`)
	require.Contains(t, body, `form="platform-approve-form"`)
	require.Contains(t, body, `id="platform-approve-form"`)
}

func TestOAuthHTTPConnectHidesFinishElsewhereForWebRedirect(t *testing.T) {
	t.Parallel()

	service, challenge := newFinishElsewhereChallenge(t, "https://client.example.com/callback")
	require.NotContains(t, connectGetPage(t, service, challenge).Body.String(), `name="finish_elsewhere"`)
}

// Ticked, an approval renders the authorization response for the person to
// deliver from the client's machine instead of redirecting this browser.
func TestOAuthHTTPConnectPostFinishElsewhereShowsCallbackCommand(t *testing.T) {
	t.Parallel()

	service, challenge := newFinishElsewhereChallenge(t, "http://127.0.0.1:3000/callback")
	response := connectApprove(t, service, challenge, true)
	require.Equal(t, http.StatusOK, response.Code)
	require.Empty(t, response.Header().Get("Location"))
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))

	body := response.Body.String()
	require.Contains(t, body, "Finish on the other machine")
	require.Contains(t, body, "You gave <span class=\"text-foreground\">Codex</span>")
	require.Contains(t, body, "curl -fsS -o /dev/null")
	_, rest, found := strings.Cut(body, `data-finish-url>`)
	require.True(t, found, "page shows the callback address")
	shown, _, found := strings.Cut(rest, "</code>")
	require.True(t, found)

	callback, err := url.Parse(html.UnescapeString(shown))
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:3000", callback.Host)
	require.Equal(t, "client-state", callback.Query().Get("state"))
	require.NotEmpty(t, callback.Query().Get("code"))
}

// Unticked, a loopback client keeps the redirect it always had.
func TestOAuthHTTPConnectPostLoopbackRedirectsWhenNotFinishingElsewhere(t *testing.T) {
	t.Parallel()

	service, challenge := newFinishElsewhereChallenge(t, "http://127.0.0.1:3000/callback")
	response := connectApprove(t, service, challenge, false)
	require.Equal(t, http.StatusSeeOther, response.Code)
	location, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:3000", location.Host)
	require.NotEmpty(t, location.Query().Get("code"))
}

// A browser can follow a web redirect itself, so the field is ignored there.
func TestOAuthHTTPConnectPostFinishElsewhereIgnoredForWebRedirect(t *testing.T) {
	t.Parallel()

	service, challenge := newFinishElsewhereChallenge(t, "https://client.example.com/callback")
	response := connectApprove(t, service, challenge, true)
	require.Equal(t, http.StatusSeeOther, response.Code)
	location, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "client.example.com", location.Host)
	require.NotEmpty(t, location.Query().Get("code"))
}
