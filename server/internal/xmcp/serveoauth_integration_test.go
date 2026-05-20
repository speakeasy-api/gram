// serveoauth_integration_test.go drives the issuer-gated OAuth dance
// end-to-end through the chi mux built by [xmcp.Attach]:
//
//   - register → authorize → consent → token → ServeMCP (no upstream IDP
//     roundtrip; remote_session_client is not configured so the consent
//     form posts immediately).
//   - handleRemoteLoginCallback: a separate flow exercising
//     /x/mcp/remote_login_callback against a live dev-idp upstream, the
//     /x/mcp parallel of [mcp.TestRemoteLoginCallback_AnonymousSubject].
//
// These tests catch route-wiring regressions in [xmcp.Attach] beyond what
// the per-adapter smoke tests do — they verify the adapter family
// composes correctly across an entire OAuth flow.
package xmcp_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

// TestAttach_OAuthFullDance_PublicRemoteBackend drives register → authorize
// → consent (GET + POST) → token → ServeMCP entirely through the chi mux
// built by [xmcp.Attach]. Uses a public visibility endpoint without an
// upstream remote_session_client so the consent form can be posted
// immediately (no IDP roundtrip required). Catches route-wiring
// regressions across the entire adapter family that the per-adapter
// smoke tests can't catch in isolation.
func TestAttach_OAuthFullDance_PublicRemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	slug, _, _ := seedIssuerGatedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, upstream.URL, "public")

	mux := goahttp.NewMuxer()
	xmcp.Attach(mux, ti.service, nil)

	clientRedirectURI := "http://localhost:3000/callback"

	// Step 1: POST /register — DCR mint a client.
	regBody := []byte(`{"client_name":"full dance","redirect_uris":["` + clientRedirectURI + `"],"token_endpoint_auth_method":"none"}`)
	regReq := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x/mcp/"+slug+"/register", bytes.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regW := httptest.NewRecorder()
	mux.ServeHTTP(regW, regReq)
	require.Equal(t, http.StatusCreated, regW.Code, "register; body=%s", regW.Body.String())
	var regResp struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(regW.Body.Bytes(), &regResp))
	require.NotEmpty(t, regResp.ClientID)

	// Step 2: GET /authorize — public visibility 302s to /connect.
	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(sum[:])

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", regResp.ClientID)
	q.Set("redirect_uri", clientRedirectURI)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", "client-state")

	authReq := httptest.NewRequestWithContext(ctx, http.MethodGet, "/x/mcp/"+slug+"/authorize?"+q.Encode(), nil)
	authW := httptest.NewRecorder()
	mux.ServeHTTP(authW, authReq)
	require.Equal(t, http.StatusFound, authW.Code, "authorize; body=%s", authW.Body.String())

	consentLoc, err := url.Parse(authW.Header().Get("Location"))
	require.NoError(t, err)
	require.Contains(t, consentLoc.Path, "/x/mcp/"+slug+"/connect")
	challengeID := consentLoc.Query().Get("state")
	require.NotEmpty(t, challengeID)

	// Step 3: GET /connect — renders the consent form with a CSRF token
	// embedded as a hidden input.
	consentGetReq := httptest.NewRequestWithContext(ctx, http.MethodGet, consentLoc.String(), nil)
	consentGetW := httptest.NewRecorder()
	mux.ServeHTTP(consentGetW, consentGetReq)
	require.Equal(t, http.StatusOK, consentGetW.Code, "consent GET; body=%s", consentGetW.Body.String())
	csrfToken := extractCSRFToken(t, consentGetW.Body.Bytes())
	require.NotEmpty(t, csrfToken)

	// Step 4: POST /connect — approve consent, mint a code, 302 back to
	// the client's redirect_uri with `code=` + the original `state`.
	consentForm := url.Values{}
	consentForm.Set("state", challengeID)
	consentForm.Set("csrf_token", csrfToken)
	consentForm.Set("action", "approve")
	consentPostReq := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x/mcp/"+slug+"/connect", strings.NewReader(consentForm.Encode()))
	consentPostReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	consentPostW := httptest.NewRecorder()
	mux.ServeHTTP(consentPostW, consentPostReq)
	// 303 See Other is the canonical POST-redirect-GET response for
	// the consent submission.
	require.Equal(t, http.StatusSeeOther, consentPostW.Code, "consent POST; body=%s", consentPostW.Body.String())

	redirectLoc, err := url.Parse(consentPostW.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "client-state", redirectLoc.Query().Get("state"))
	code := redirectLoc.Query().Get("code")
	require.NotEmpty(t, code, "consent POST must mint an auth code; redirect=%s", redirectLoc.String())

	// Step 5: POST /token — exchange the code for an access token.
	tokenForm := url.Values{}
	tokenForm.Set("grant_type", "authorization_code")
	tokenForm.Set("code", code)
	tokenForm.Set("redirect_uri", clientRedirectURI)
	tokenForm.Set("client_id", regResp.ClientID)
	tokenForm.Set("code_verifier", verifier)
	tokenReq := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x/mcp/"+slug+"/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenW := httptest.NewRecorder()
	mux.ServeHTTP(tokenW, tokenReq)
	require.Equal(t, http.StatusOK, tokenW.Code, "token; body=%s", tokenW.Body.String())

	var tokResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	require.NoError(t, json.Unmarshal(tokenW.Body.Bytes(), &tokResp))
	require.NotEmpty(t, tokResp.AccessToken)
	require.Equal(t, "Bearer", tokResp.TokenType)

	// Step 6: POST /x/mcp/{slug} with the Bearer — proxied upstream.
	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(tokResp.AccessToken), []byte(initializeBody))
	require.Equal(t, http.StatusOK, rr.Code, "ServeMCP after full dance; body=%s", rr.Body.String())
}

// TestHandleRemoteLoginCallback_AnonymousSubject covers the /x/mcp
// remote-login callback handler against a live dev-idp upstream and the
// xmcptest-created issuer/client trio. Mirrors
// [mcp.TestRemoteLoginCallback_AnonymousSubject] for the /x/mcp surface,
// closing the zero-coverage gap on [Service.handleRemoteLoginCallback].
// The mounted route under /x/mcp/remote_login_callback delegates to
// mcp.Service.HandleRemoteLoginCallback; this test exercises that
// delegation through the chi mux end-to-end so a routing or RouteBase
// regression would be observed.
func TestHandleRemoteLoginCallback_AnonymousSubject(t *testing.T) {
	t.Parallel()

	idp := devidptest.Launch(t, devidptest.LaunchOpts{})
	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := createIssuerGatedMcpServer(t, ctx, ti.conn, ti.enc, authCtx, issuerGatedMcpServerOpts{
		Backend:                      issuerGatedBackendRemote,
		Slug:                         "xmcp-rlc-anon",
		Visibility:                   "public",
		UpstreamMetadata:             idp.OAuth21Metadata(t),
		RemoteSessionCallbackBaseURL: ti.serverURL.String(),
		RemoteUpstreamURL:            "https://upstream.invalid/mcp",
	})

	mgr, authnCache := buildXmcpChallengeManagerForTest(t, ti)

	parentID := uuid.NewString()
	anonymousSubject := urn.NewAnonymousSubject(uuid.NewString())
	require.NoError(t, authnCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  parentID,
		UserSessionIssuerID: result.UserSessionIssuer.ID,
		Endpoint: mcp.EndpointRef{
			McpSlug:        result.Slug,
			CustomDomainID: uuid.NullUUID{},
			McpServerID:    uuid.NullUUID{UUID: result.McpServer.ID, Valid: true},
			RouteBase:      "x/mcp",
		},
		ClientID:            "test-mcp-client",
		RedirectURI:         "http://example.com/cb",
		CodeChallenge:       "",
		CodeChallengeMethod: "",
		CSRFToken:           "csrf-token",
		Subject:             &anonymousSubject,
		CreatedAt:           time.Now(),
	}))

	_, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:   result.UserSessionIssuer.ID,
		ClientID:              "test-mcp-client",
		ClientSecretHash:      pgtype.Text{Valid: false},
		ClientName:            "test-mcp-client",
		RedirectUris:          []string{"http://example.com/cb"},
		ClientSecretExpiresAt: pgtype.Timestamptz{Valid: false},
	})
	require.NoError(t, err)

	clients, err := mgr.ListClients(ctx, result.McpServer.ProjectID, result.UserSessionIssuer.ID)
	require.NoError(t, err)
	require.Len(t, clients, 1)

	parent := remotesessions.ParentChallenge{
		ID:                  parentID,
		ProjectID:           result.McpServer.ProjectID,
		UserSessionIssuerID: result.UserSessionIssuer.ID,
		Subject:             &anonymousSubject,
		McpSlug:             result.Slug,
		RouteBase:           "x/mcp",
		FinalRedirectURI:    "",
	}
	authURL, err := mgr.BuildAuthorizationUrl(ctx, parent, clients[0])
	require.NoError(t, err)

	upstreamResp := httpGetNoFollow(t, authURL)
	defer func() { _ = upstreamResp.Body.Close() }()
	require.Equal(t, http.StatusFound, upstreamResp.StatusCode, "upstream /authorize should redirect")

	loc, err := url.Parse(upstreamResp.Header.Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	state := loc.Query().Get("state")
	require.NotEmpty(t, code)
	require.NotEmpty(t, state)

	// Hit the /x/mcp/remote_login_callback route through the chi mux
	// built by [xmcp.Attach] — the whole point of this test is to
	// exercise the route-wiring, not the inner handler directly.
	mux := goahttp.NewMuxer()
	xmcp.Attach(mux, ti.service, nil)

	cbReq := httptest.NewRequestWithContext(ctx, http.MethodGet,
		"/x/mcp/remote_login_callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), nil)
	cbW := httptest.NewRecorder()
	mux.ServeHTTP(cbW, cbReq)
	require.Equal(t, http.StatusSeeOther, cbW.Code, "callback adapter must redirect; body=%s", cbW.Body.String())
	require.Contains(t, cbW.Header().Get("Location"), "/x/mcp/"+result.Slug+"/connect",
		"callback should redirect back to /x/mcp consent (not /mcp/) — RouteBase regression check")
	require.Contains(t, cbW.Header().Get("Location"), parentID)

	sessions, err := remotesessions_repo.New(ti.conn).ListRemoteSessionsByProjectID(ctx, remotesessions_repo.ListRemoteSessionsByProjectIDParams{
		ProjectID:  result.McpServer.ProjectID,
		LimitValue: 10,
	})
	require.NoError(t, err)
	require.Len(t, sessions, 1, "exactly one remote_sessions row should exist after callback")
	require.Equal(t, anonymousSubject.String(), sessions[0].SubjectUrn.String())
}

// extractCSRFToken yanks the csrf_token hidden input value out of the
// consent-form HTML. The template stamps a literal
// `<input type="hidden" name="csrf_token" value="...">` so a string
// scan is sufficient; a real parser would be overkill for a test
// helper.
func extractCSRFToken(t *testing.T, body []byte) string {
	t.Helper()

	const marker = `name="csrf_token" value="`
	idx := bytes.Index(body, []byte(marker))
	if idx < 0 {
		// The template orders attributes value-first in some variants —
		// try the alternate ordering.
		const alt = `value="`
		valueIdx := bytes.Index(body, []byte(`name="csrf_token"`))
		require.GreaterOrEqual(t, valueIdx, 0, "csrf_token input missing from consent body: %s", string(body))
		// Search back from the name= for the nearest value=
		segment := body[:valueIdx]
		valStart := bytes.LastIndex(segment, []byte(alt))
		require.GreaterOrEqual(t, valStart, 0)
		valStart += len(alt)
		end := bytes.IndexByte(segment[valStart:], '"')
		require.GreaterOrEqual(t, end, 0)
		return string(segment[valStart : valStart+end])
	}
	start := idx + len(marker)
	end := bytes.IndexByte(body[start:], '"')
	require.GreaterOrEqual(t, end, 0)
	return string(body[start : start+end])
}

// httpGetNoFollow issues a GET with redirects disabled so the test can
// observe the 302 from the upstream IDP. Mirrors the same helper in the
// /mcp integration tests.
func httpGetNoFollow(t *testing.T, urlStr string) *http.Response {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, urlStr, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

// buildXmcpChallengeManagerForTest is the /x/mcp companion of the
// /mcp test helper of the same shape. Constructs a ChallengeManager
// wired to the same Redis + DB as the service under test, and a
// TypedCacheObject for AuthnChallengeState keyed identically to the
// service's internal cache.
func buildXmcpChallengeManagerForTest(
	t *testing.T,
	ti *testInstance,
) (*remotesessions.ChallengeManager, cache.TypedCacheObject[mcp.AuthnChallengeState]) {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(ti.tracerProvider, []string{})
	require.NoError(t, err)

	mgr := remotesessions.NewChallengeManager(ti.logger, ti.conn, ti.enc, policy, ti.cacheAdapter, ti.serverURL)
	authnCache := cache.NewTypedObjectCache[mcp.AuthnChallengeState](
		ti.logger.With(attr.SlogCacheNamespace("authn_challenge")),
		ti.cacheAdapter,
		cache.SuffixNone,
	)
	return mgr, authnCache
}
