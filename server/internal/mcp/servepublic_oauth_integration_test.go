// servepublic_oauth_integration_test.go drives the /mcp/{slug} runtime
// against a live dev-idp instance acting as the upstream OAuth server,
// covering external/passthrough mode for both OAuth 2.1 and OAuth 2.0
// (including the OAuth 2.0 refresh-no-rotation invariant).
package mcp_test

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
)

const integrationRedirectURI = "http://localhost:8080/callback"

// TestServePublic_ExternalOAuth21_PassthroughAcceptsUpstreamToken drives
// a full DCR + auth-code flow against dev-idp/oauth2-1, then sends the
// resulting access token to /mcp/{slug} on a Gram external-mode toolset
// wired to dev-idp's metadata. Gram passes the bearer through without
// validating it, so the request must not 401.
func TestServePublic_ExternalOAuth21_PassthroughAcceptsUpstreamToken(t *testing.T) {
	t.Parallel()

	idp := devidptest.Launch(t, devidptest.LaunchOpts{})
	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "ext-oauth21-passthrough",
		IsPublic: true,
		Metadata: idp.OAuth21Metadata(t),
	})
	mcpSlug := result.Toolset.McpSlug.String

	upstream := newUpstreamFlow(t, idp.OAuth21URL, true /* PKCE required */)
	access := upstream.runFullFlow(t)

	w, err := servePublicHTTP(t, t.Context(), ti, mcpSlug, makeInitializeBody(), access, nil)
	require.NoError(t, err)
	require.Empty(t, w.Header().Get("WWW-Authenticate"),
		"upstream-issued access token should pass Gram's external-mode bearer check")
}

// TestServePublic_ExternalOAuth21_RefreshRotatesUpstreamPair verifies
// that dev-idp/oauth2-1 rotates both access and refresh on /token
// refresh, and that the rotated access token is accepted by /mcp/{slug}
// just like the original was. The realistic "MCP client refreshes its
// own token" scenario.
func TestServePublic_ExternalOAuth21_RefreshRotatesUpstreamPair(t *testing.T) {
	t.Parallel()

	idp := devidptest.Launch(t, devidptest.LaunchOpts{})
	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	result := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "ext-oauth21-refresh",
		IsPublic: true,
		Metadata: idp.OAuth21Metadata(t),
	})
	mcpSlug := result.Toolset.McpSlug.String

	upstream := newUpstreamFlow(t, idp.OAuth21URL, true)
	upstream.runFullFlow(t)
	originalAccess := upstream.accessToken
	originalRefresh := upstream.refreshToken

	rotated := upstream.refresh(t)
	require.NotEqual(t, originalAccess, rotated.access, "oauth2-1 must rotate access on refresh")
	require.NotEqual(t, originalRefresh, rotated.refresh, "oauth2-1 must rotate refresh on refresh")

	w, err := servePublicHTTP(t, t.Context(), ti, mcpSlug, makeInitializeBody(), rotated.access, nil)
	require.NoError(t, err)
	require.Empty(t, w.Header().Get("WWW-Authenticate"))

	// Old refresh token must now be invalid (rotation invalidates it
	// upstream — that's the security property under test).
	resp := upstream.refreshRaw(t, originalRefresh)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"old refresh token must be invalidated after rotation")
}

// TestServePublic_ExternalOAuth20_RefreshDoesNotRotateUpstreamPair
// verifies the OAuth 2.0 refresh-no-rotation invariant end-to-end:
// dev-idp/oauth2 issues a new access token on refresh but reuses the
// same refresh token (see the package header on
// dev-idp/internal/modes/oauth2/handler.go).
func TestServePublic_ExternalOAuth20_RefreshDoesNotRotateUpstreamPair(t *testing.T) {
	t.Parallel()

	idp := devidptest.Launch(t, devidptest.LaunchOpts{})
	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	result := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "ext-oauth20-refresh",
		IsPublic: true,
		Metadata: idp.OAuth20Metadata(t),
	})
	mcpSlug := result.Toolset.McpSlug.String

	upstream := newUpstreamFlow(t, idp.OAuth20URL, false /* no PKCE */)
	upstream.runFullFlow(t)
	originalAccess := upstream.accessToken
	originalRefresh := upstream.refreshToken

	rotated := upstream.refresh(t)
	require.NotEqual(t, originalAccess, rotated.access, "even oauth2 still rotates access")
	require.Equal(t, originalRefresh, rotated.refresh,
		"oauth2 must NOT rotate refresh — see dev-idp/oauth2/handler.go header comment")

	w, err := servePublicHTTP(t, t.Context(), ti, mcpSlug, makeInitializeBody(), rotated.access, nil)
	require.NoError(t, err)
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
}

// TestServePublic_ExternalOAuth21_PostRefreshTokenForwards simulates the
// "client's upstream-token expired, refresh, retry" loop. Gram doesn't
// track upstream token lifetimes in passthrough mode, so the test value
// is that the rotated token works end-to-end with no Gram-side
// cooperation needed.
//
// Distinct from TestServePublic_ExternalOAuth21_RefreshRotatesUpstreamPair:
// that test focuses on the upstream rotation/invalidation property
// (rotated pair != original AND original refresh is rejected post-rotation).
// This test focuses on the client-side retry shape — a token obtained via
// refresh (not the initial issuance) must be accepted by /mcp/{slug}
// without any prior Gram-side handshake.
func TestServePublic_ExternalOAuth21_PostRefreshTokenForwards(t *testing.T) {
	t.Parallel()

	idp := devidptest.Launch(t, devidptest.LaunchOpts{})
	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	result := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "ext-oauth21-post-refresh",
		IsPublic: true,
		Metadata: idp.OAuth21Metadata(t),
	})
	mcpSlug := result.Toolset.McpSlug.String

	upstream := newUpstreamFlow(t, idp.OAuth21URL, true)
	upstream.runFullFlow(t)

	rotated := upstream.refresh(t)
	require.NotEmpty(t, rotated.access)

	w, err := servePublicHTTP(t, t.Context(), ti, mcpSlug, makeInitializeBody(), rotated.access, nil)
	require.NoError(t, err)
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
}

// ===========================================================================
// upstreamFlow — drives a real DCR + auth-code-flow against dev-idp.
// ===========================================================================

// upstreamFlow holds the state of a test client driving an OAuth flow
// against a single dev-idp mode (oauth21 OR oauth2). It encapsulates the
// mechanical bits — DCR (where supported), PKCE construction, the
// /authorize redirect-then-extract-code dance, and the /token exchange —
// so test bodies stay focused on the assertion under test.
type upstreamFlow struct {
	modeURL      string
	pkceRequired bool

	clientID     string
	clientSecret string

	accessToken  string
	refreshToken string
}

func newUpstreamFlow(t *testing.T, modeURL string, pkceRequired bool) *upstreamFlow {
	t.Helper()
	return &upstreamFlow{
		modeURL:      modeURL,
		pkceRequired: pkceRequired,
		// dev-idp/oauth2 has no /register; use the package-wide default
		// client_id. oauth2-1 callers overwrite this in
		// dynamicallyRegisterClient.
		clientID: devidptest.DefaultClientID,
	}
}

// runFullFlow registers a client (oauth2-1 only), then drives /authorize
// and /token to obtain an initial access+refresh pair. The fields
// accessToken and refreshToken are populated on success and the access
// token is also returned for callers that want to use it inline.
func (u *upstreamFlow) runFullFlow(t *testing.T) string {
	t.Helper()

	if u.pkceRequired {
		u.dynamicallyRegisterClient(t)
	}

	verifier := pkceVerifier(t)
	challenge := pkceChallenge(verifier)

	authQ := url.Values{}
	authQ.Set("response_type", "code")
	authQ.Set("client_id", u.clientID)
	authQ.Set("redirect_uri", integrationRedirectURI)
	authQ.Set("state", "integration-state")
	authQ.Set("scope", "openid")
	if u.pkceRequired {
		authQ.Set("code_challenge", challenge)
		authQ.Set("code_challenge_method", "S256")
	}

	resp := httpGetNoFollow(t, u.modeURL+"/authorize?"+authQ.Encode())
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusFound, resp.StatusCode, "authorize should redirect")

	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	tokForm := url.Values{}
	tokForm.Set("grant_type", "authorization_code")
	tokForm.Set("code", code)
	tokForm.Set("client_id", u.clientID)
	tokForm.Set("redirect_uri", integrationRedirectURI)
	if u.pkceRequired {
		tokForm.Set("code_verifier", verifier)
	}
	if u.clientSecret != "" {
		tokForm.Set("client_secret", u.clientSecret)
	}

	out := postTokenForm(t, u.modeURL+"/token", tokForm)
	u.accessToken = out.access
	u.refreshToken = out.refresh
	return u.accessToken
}

// refresh exchanges the current refresh token for a new access (and, for
// oauth2-1, a new refresh) at the upstream /token endpoint.
func (u *upstreamFlow) refresh(t *testing.T) tokenPair {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", u.refreshToken)
	form.Set("client_id", u.clientID)
	if u.clientSecret != "" {
		form.Set("client_secret", u.clientSecret)
	}

	out := postTokenForm(t, u.modeURL+"/token", form)
	u.accessToken = out.access
	u.refreshToken = out.refresh
	return out
}

// refreshRaw posts a refresh-token request and returns the raw HTTP
// response, so callers can assert on rejection paths (e.g. an
// already-rotated refresh token producing a 400).
func (u *upstreamFlow) refreshRaw(t *testing.T, refreshToken string) *http.Response {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", u.clientID)
	if u.clientSecret != "" {
		form.Set("client_secret", u.clientSecret)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		u.modeURL+"/token", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// dynamicallyRegisterClient runs DCR (RFC 7591) against /register to
// obtain a client_id/client_secret pair. Only oauth2-1 supports DCR;
// oauth2 callers leave clientID at its fixed test-client value.
func (u *upstreamFlow) dynamicallyRegisterClient(t *testing.T) {
	t.Helper()

	body := map[string]any{
		"redirect_uris":              []string{integrationRedirectURI},
		"token_endpoint_auth_method": "client_secret_post",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"client_name":                "integration-test-client",
	}
	bs, err := json.Marshal(body)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		u.modeURL+"/register", strings.NewReader(string(bs)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var reg map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&reg))
	u.clientID, _ = reg["client_id"].(string)
	u.clientSecret, _ = reg["client_secret"].(string)
	require.NotEmpty(t, u.clientID)
	require.NotEmpty(t, u.clientSecret)
}

// tokenPair is the access+refresh pair surfaced by /token responses.
type tokenPair struct {
	access  string
	refresh string
}

func postTokenForm(t *testing.T, tokenURL string, form url.Values) tokenPair {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		tokenURL, strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"token endpoint should accept the request: %s", string(body))

	var doc map[string]any
	require.NoError(t, json.Unmarshal(body, &doc))

	access, _ := doc["access_token"].(string)
	refresh, _ := doc["refresh_token"].(string)
	require.NotEmpty(t, access, "access_token must be present in token response")
	return tokenPair{access: access, refresh: refresh}
}

func httpGetNoFollow(t *testing.T, requestURL string) *http.Response {
	t.Helper()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, requestURL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

// pkceVerifier returns a fresh 32-byte URL-safe random string for use as
// the PKCE code_verifier (RFC 7636 §4.1).
func pkceVerifier(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 32)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(buf)
}

// pkceChallenge returns the S256 code_challenge for a given verifier.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
