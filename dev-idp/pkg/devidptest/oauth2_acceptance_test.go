package devidptest_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
)

const fixtureRedirectURI = "http://localhost:8080/callback"

// TestOAuth20_AuthCodeFlowWithoutPKCE drives the dev-idp's OAuth 2.0 mode
// end-to-end: GET /authorize WITHOUT a code_challenge succeeds (PKCE is
// optional in OAuth 2.0), and the issued code can be exchanged at POST
// /token WITHOUT a code_verifier.
func TestOAuth20_AuthCodeFlowWithoutPKCE(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{})
	client := noRedirectClient()

	authorizeURL := inst.OAuth20URL + "/authorize?" + url.Values{
		"response_type": {"code"},
		"client_id":     {"test-client"},
		"redirect_uri":  {fixtureRedirectURI},
		"state":         {"xyz-state"},
		"scope":         {"openid"},
	}.Encode()

	authReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, authorizeURL, nil)
	require.NoError(t, err)

	authResp, err := client.Do(authReq)
	require.NoError(t, err)
	defer func() { _ = authResp.Body.Close() }()

	require.Equal(t, http.StatusFound, authResp.StatusCode, "authorize without PKCE should redirect")

	loc, err := url.Parse(authResp.Header.Get("Location"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(loc.String(), fixtureRedirectURI), "redirect target should be the registered redirect_uri")
	require.Equal(t, "xyz-state", loc.Query().Get("state"), "state should round-trip")

	code := loc.Query().Get("code")
	require.NotEmpty(t, code, "authorize redirect should carry a code")

	tokenForm := url.Values{}
	tokenForm.Set("grant_type", "authorization_code")
	tokenForm.Set("code", code)
	tokenForm.Set("client_id", "test-client")
	tokenForm.Set("redirect_uri", fixtureRedirectURI)
	// Deliberately NO code_verifier.

	tokenReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		inst.OAuth20URL+"/token", strings.NewReader(tokenForm.Encode()))
	require.NoError(t, err)
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	tokenResp, err := client.Do(tokenReq)
	require.NoError(t, err)
	defer func() { _ = tokenResp.Body.Close() }()

	body, err := io.ReadAll(tokenResp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, tokenResp.StatusCode,
		"token exchange without code_verifier should succeed: %s", string(body))

	var doc map[string]any
	require.NoError(t, json.Unmarshal(body, &doc))
	require.NotEmpty(t, doc["access_token"], "access_token should be present")
	require.NotEmpty(t, doc["refresh_token"], "refresh_token should be present")
	require.Equal(t, "Bearer", doc["token_type"])
}

// noRedirectClient returns an http.Client that surfaces 3xx responses to
// the caller instead of following them. The /authorize handler responds
// with a 302 to redirect_uri; the test needs to read the Location header
// rather than have the client chase the redirect.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
