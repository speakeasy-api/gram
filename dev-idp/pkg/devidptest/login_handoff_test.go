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

// TestLogin_AuthorizeCodeIsRedeemableAtAuthenticate covers the handoff that
// makes non-interactive login work, and that no single package owns: the
// OAuth 2.1 /authorize leg mints a code, and the WorkOS-shaped
// /user_management/authenticate leg redeems it. The Gram server drives login
// through the WorkOS user-management SDK, so it never touches the OAuth token
// endpoint — if these two legs disagree about which codes they speak,
// dashboard login breaks with everything still passing its own unit tests.
func TestLogin_AuthorizeCodeIsRedeemableAtAuthenticate(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{EnableMockWorkos: true, Key: nil})

	authorizeURL := inst.OAuth21URL + "/authorize?" + url.Values{
		"response_type": {"code"},
		"client_id":     {devidptest.LoginClientID},
		"redirect_uri":  {fixtureRedirectURI},
		"state":         {"login-state"},
		"scope":         {"openid email profile"},
	}.Encode()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, authorizeURL, nil)
	require.NoError(t, err)

	resp, err := noRedirectClient().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusFound, resp.StatusCode, "login authorize should redirect without a prompt")

	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	body, err := json.Marshal(map[string]string{
		"client_id":     devidptest.LoginClientID,
		"client_secret": "unset",
		"grant_type":    "authorization_code",
		"code":          code,
	})
	require.NoError(t, err)

	authReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		inst.MockWorkosURL+"/user_management/authenticate", strings.NewReader(string(body)))
	require.NoError(t, err)
	authReq.Header.Set("Content-Type", "application/json")

	authResp, err := http.DefaultClient.Do(authReq)
	require.NoError(t, err)
	defer func() { _ = authResp.Body.Close() }()

	raw, err := io.ReadAll(authResp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, authResp.StatusCode,
		"authenticate must redeem the code /authorize minted: %s", string(raw))

	var out struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.NotEmpty(t, out.User.ID, "authenticate must report a subject for the server to store as workos_id")
	require.Equal(t, inst.DefaultUser.Email, out.User.Email)
	require.NotEmpty(t, out.AccessToken)

	// Codes are single-use.
	replayReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		inst.MockWorkosURL+"/user_management/authenticate", strings.NewReader(string(body)))
	require.NoError(t, err)
	replayReq.Header.Set("Content-Type", "application/json")

	replayResp, err := http.DefaultClient.Do(replayReq)
	require.NoError(t, err)
	defer func() { _ = replayResp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, replayResp.StatusCode, "an auth code must not be redeemable twice")
}
