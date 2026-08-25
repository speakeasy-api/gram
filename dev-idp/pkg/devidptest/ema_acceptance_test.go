package devidptest_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/ema"
	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
)

const (
	acceptanceResourceSlug = "chat"
	acceptanceResourceID   = "https://mcp.chat.example/"
	acceptanceClientID     = "acceptance-app"
)

// postForm posts a form and returns status plus the fully-read body.
func postForm(t *testing.T, endpoint string, form url.Values) (int, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	require.NoError(t, err, "build request")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "post %s", endpoint)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read body")
	return resp.StatusCode, body
}

// loginIDToken drives the real non-interactive login to get an id_token for
// the instance's default user, the same way a client would before asking for
// an ID-JAG. Nothing about this leg is EMA-specific, which is the point:
// the mint leg has to accept what ordinary login actually produces.
func loginIDToken(t *testing.T, inst *devidptest.Instance) string {
	t.Helper()

	authorizeURL := inst.OAuth21URL + "/authorize?" + url.Values{
		"response_type": {"code"},
		"client_id":     {devidptest.LoginClientID},
		"redirect_uri":  {fixtureRedirectURI},
		"scope":         {"openid email profile"},
	}.Encode()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, authorizeURL, nil)
	require.NoError(t, err)
	resp, err := noRedirectClient().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusFound, resp.StatusCode)

	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code, "authorize should mint a code")

	status, body := postForm(t, inst.OAuth21URL+"/token", url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"client_id":    {devidptest.LoginClientID},
		"redirect_uri": {fixtureRedirectURI},
	})
	require.Equal(t, http.StatusOK, status, "token: %s", body)

	var tokens struct {
		IDToken string `json:"id_token"`
	}
	require.NoError(t, json.Unmarshal(body, &tokens))
	require.NotEmpty(t, tokens.IDToken, "login should return an id_token")
	return tokens.IDToken
}

// TestEMA_LoginToMintToRedeem walks the whole enterprise-managed
// authorization flow across
// both halves of the dev-idp, which no single package owns: log in at the
// oauth2-1 IdP, exchange the resulting id_token for an ID-JAG naming a
// resource, and redeem that ID-JAG at the resource's own authorization
// server for an audience-restricted access token.
//
// Each leg has unit coverage in its own package. What this test protects is
// that the two agree on the wire -- the audience the mint leg stamps into
// `aud` is exactly the issuer identifier the redeem leg computes for itself,
// and the typ header one writes is the one the other requires.
func TestEMA_LoginToMintToRedeem(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: false, Key: nil})
	ctx := t.Context()

	app := devidptest.CreateEmaApp(t, ctx, inst.Repo, devidptest.EmaAppOpts{
		ClientID: acceptanceClientID, ClientSecret: "", Name: "", Disabled: false,
	})
	resource := devidptest.CreateEmaResource(t, ctx, inst.Repo, devidptest.EmaResourceOpts{
		Slug: acceptanceResourceSlug, Name: "Chat", ResourceIdentifier: acceptanceResourceID,
	})
	devidptest.AssignEmaApp(t, ctx, inst.Repo, app, inst.DefaultUser.ID, resource.ID, "chat.read chat.history")
	devidptest.TrustEmaIssuer(t, ctx, inst.Repo, resource.ID, inst.OAuth21URL, devidptest.EmaTrustRuleOpts{
		AllowedScopes: "", AllowedClientIDs: "", Disabled: false,
	})

	audience := inst.ResourceASURL(acceptanceResourceSlug)

	// Leg 1: ordinary login.
	idToken := loginIDToken(t, inst)

	// Leg 2: exchange it for an ID-JAG naming the resource.
	status, body := postForm(t, inst.OAuth21URL+"/token", url.Values{
		"grant_type":           {ema.GrantTypeTokenExchange},
		"requested_token_type": {ema.TokenTypeIDJAG},
		"audience":             {audience},
		"resource":             {acceptanceResourceID},
		"scope":                {"chat.read"},
		"subject_token":        {idToken},
		"subject_token_type":   {ema.TokenTypeIDToken},
		"client_id":            {acceptanceClientID},
	})
	require.Equal(t, http.StatusOK, status, "mint: %s", body)

	var minted struct {
		IssuedTokenType string `json:"issued_token_type"`
		AccessToken     string `json:"access_token"`
		TokenType       string `json:"token_type"`
		Scope           string `json:"scope"`
	}
	require.NoError(t, json.Unmarshal(body, &minted))
	require.Equal(t, ema.TokenTypeIDJAG, minted.IssuedTokenType)
	require.Equal(t, "N_A", minted.TokenType)
	require.Equal(t, "chat.read", minted.Scope, "the request narrowed to one of the two assigned scopes")

	// Leg 3: redeem it at the resource's own authorization server.
	status, body = postForm(t, audience+"/token", url.Values{
		"grant_type": {ema.GrantTypeJWTBearer},
		"assertion":  {minted.AccessToken},
		"client_id":  {acceptanceClientID},
	})
	require.Equal(t, http.StatusOK, status, "redeem: %s", body)

	var redeemed struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
	}
	require.NoError(t, json.Unmarshal(body, &redeemed))
	require.NotEmpty(t, redeemed.AccessToken)
	require.Equal(t, "Bearer", redeemed.TokenType)
	require.Equal(t, "chat.read", redeemed.Scope)

	// The issued token must be bound to the MCP server, not to the
	// authorization server that minted it.
	status, body = postForm(t, audience+"/introspect", url.Values{"token": {redeemed.AccessToken}})
	require.Equal(t, http.StatusOK, status, "introspect: %s", body)

	var introspection struct {
		Active   bool   `json:"active"`
		Aud      string `json:"aud"`
		Sub      string `json:"sub"`
		Username string `json:"username"`
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(body, &introspection))
	require.True(t, introspection.Active)
	require.Equal(t, acceptanceResourceID, introspection.Aud)
	require.Equal(t, inst.DefaultUser.ID.String(), introspection.Sub)
	require.Equal(t, inst.DefaultUser.Email, introspection.Username)
	require.Equal(t, acceptanceClientID, introspection.ClientID)

	// An ID-JAG is single use. Replaying the same assertion must not yield a
	// second access token.
	status, body = postForm(t, audience+"/token", url.Values{
		"grant_type": {ema.GrantTypeJWTBearer},
		"assertion":  {minted.AccessToken},
		"client_id":  {acceptanceClientID},
	})
	require.Equal(t, http.StatusBadRequest, status, "replay should be refused: %s", body)
}

// TestEMA_DiscoveryAdvertisesBothHalves covers what an MCP client reads
// before it attempts any of this: the IdP has to say it can mint the grant,
// and the resource authorization server has to say it accepts the profile.
// A client that cannot see both will not try the flow at all.
func TestEMA_DiscoveryAdvertisesBothHalves(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: false, Key: nil})
	devidptest.CreateEmaResource(t, t.Context(), inst.Repo, devidptest.EmaResourceOpts{
		Slug: acceptanceResourceSlug, Name: "Chat", ResourceIdentifier: acceptanceResourceID,
	})

	var idp struct {
		GrantTypesSupported []string `json:"grant_types_supported"`
		RequestedTokenTypes []string `json:"identity_chaining_requested_token_types_supported"`
	}
	require.NoError(t, json.Unmarshal(inst.OAuth21Metadata(t), &idp))
	require.Contains(t, idp.GrantTypesSupported, ema.GrantTypeTokenExchange)
	require.Contains(t, idp.RequestedTokenTypes, ema.TokenTypeIDJAG)

	var resourceAS struct {
		Issuer                              string   `json:"issuer"`
		GrantTypesSupported                 []string `json:"grant_types_supported"`
		AuthorizationGrantProfilesSupported []string `json:"authorization_grant_profiles_supported"`
	}
	require.NoError(t, json.Unmarshal(inst.ResourceASMetadata(t, acceptanceResourceSlug), &resourceAS))
	require.Equal(t, inst.ResourceASURL(acceptanceResourceSlug), resourceAS.Issuer)
	require.Contains(t, resourceAS.GrantTypesSupported, ema.GrantTypeJWTBearer)
	require.Contains(t, resourceAS.AuthorizationGrantProfilesSupported, ema.GrantProfileIDJAG)
}
