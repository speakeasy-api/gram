// Integration tests for inbound CIMD (Client ID Metadata Documents) on the
// user-session OAuth surface: a URL-shaped client_id presented to
// /mcp/{slug}/authorize is resolved by fetching the metadata document from
// an httptest TLS server, gated by the gram-user-session-cimd flag.

package mcp_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// cimdDocServer hosts a Client ID Metadata Document at
// <TLS server>/oauth/client.json. Tests reshape the served response through
// set before issuing requests; mu covers every mutable field because the
// handler runs on the server's own goroutine and a loopback round trip is
// not a synchronization edge the race detector recognizes.
type cimdDocServer struct {
	srv      *httptest.Server
	clientID string

	mu sync.Mutex

	// doc is the document body, encoded on every 200.
	doc map[string]any

	// etag, when non-empty, is served as the document's ETag and matched
	// against an incoming If-None-Match, which a match answers with 304.
	etag string

	// cacheControl, when non-empty, is sent verbatim as the document's
	// Cache-Control header on both 200 and 304 responses.
	cacheControl string

	// status, when non-zero, replaces every response with that error status,
	// standing in for a document host that has started failing.
	status int

	// requests counts document fetches. Admission control's whole value is
	// that a denial costs no outbound request, and the document cache's is
	// that a warm client costs none either; both are only observable by
	// asserting on this.
	requests atomic.Int64

	// conditionalRequests counts the subset of requests that carried an
	// If-None-Match header.
	conditionalRequests atomic.Int64
}

// set applies a mutation to the served response under the same lock the
// handler reads with.
func (ds *cimdDocServer) set(t *testing.T, mutate func(ds *cimdDocServer)) {
	t.Helper()

	ds.mu.Lock()
	defer ds.mu.Unlock()
	mutate(ds)
}

func startCIMDDocServer(t *testing.T) *cimdDocServer {
	t.Helper()

	ds := &cimdDocServer{
		srv:                 nil,
		clientID:            "",
		mu:                  sync.Mutex{},
		doc:                 nil,
		etag:                "",
		cacheControl:        "",
		status:              0,
		requests:            atomic.Int64{},
		conditionalRequests: atomic.Int64{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/client.json", func(w http.ResponseWriter, r *http.Request) {
		ds.requests.Add(1)
		conditional := r.Header.Get("If-None-Match")
		if conditional != "" {
			ds.conditionalRequests.Add(1)
		}

		// Held for the whole response so the doc map cannot change mid
		// encode; requests in these tests are sequential, so contention is
		// not a concern.
		ds.mu.Lock()
		defer ds.mu.Unlock()

		// Failure injection wins over revalidation: a host that has started
		// erroring does so whether or not the request was conditional.
		if ds.status != 0 {
			http.Error(w, "document host failure", ds.status)
			return
		}

		if ds.cacheControl != "" {
			w.Header().Set("Cache-Control", ds.cacheControl)
		}
		if ds.etag != "" && conditional == ds.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if ds.etag != "" {
			w.Header().Set("ETag", ds.etag)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(ds.doc); err != nil {
			t.Errorf("encode cimd document: %v", err)
		}
	})
	ds.srv = httptest.NewTLSServer(mux)
	t.Cleanup(ds.srv.Close)

	ds.clientID = ds.srv.URL + "/oauth/client.json"
	ds.doc = map[string]any{
		"client_id":                  ds.clientID,
		"client_name":                "CIMD Integration Client",
		"redirect_uris":              []any{"http://127.0.0.1:33418/callback"},
		"token_endpoint_auth_method": "none",
	}
	return ds
}

func (ds *cimdDocServer) certPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(ds.srv.Certificate())
	return pool
}

// newTestCIMDService builds a doc server plus an mcp service whose guardian
// policy trusts the doc server's TLS certificate, seeds a public
// issuer-gated toolset, and returns the org id used as the flag distinctID.
// The flag is NOT enabled here — tests opt in via ti.features.SetFlag.
//
// The seeded issuer is put in "open" admission mode. These tests exercise
// DOCUMENT validation, and the doc server's URL is (necessarily) not a
// catalog preset, so the default "presets" mode would deny every one of them
// before a document was ever fetched. Admission itself is covered by
// authnchallenge_cimd_admission_test.go, which seeds modes explicitly.
func newTestCIMDService(t *testing.T) (context.Context, *testInstance, *cimdDocServer, toolsets_repo.Toolset, string) {
	t.Helper()

	ds := startCIMDDocServer(t)
	ctx, ti := newTestMCPServiceWithGuardianOptions(t, guardian.WithTLSRootCAs(ds.certPool()))

	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModeOpen)
	toolset, err := toolsets_repo.New(ti.conn).UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	return ctx, ti, ds, toolset, authCtx.ActiveOrganizationID
}

func doCIMDAuthorize(t *testing.T, ti *testInstance, mcpSlug, clientID, redirectURI, codeChallenge string) *httptest.ResponseRecorder {
	t.Helper()

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/authorize?"+q.Encode(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, req))
	return w
}

func requireAuthorizeOAuthError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()

	require.Equal(t, status, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, code, body["error"])
}

// TestOAuthCIMD_FullFlow drives the complete authorize → consent → token
// flow with a URL-shaped client_id. The request's loopback redirect_uri
// deliberately uses a different port (51423) than the document registers
// (33418) — the Claude Code shape, where RFC 8252 §7.3 requires the AS to
// accept variable loopback ports.
func TestOAuthCIMD_FullFlow(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	mcpSlug := toolset.McpSlug.String
	redirectURI := "http://127.0.0.1:51423/callback"
	verifier := pkceVerifier(t)

	// Authorize: resolves the document, upserts the client row, redirects
	// to consent.
	w := doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(verifier))
	require.Equal(t, http.StatusFound, w.Code)
	consentLoc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Contains(t, consentLoc.Path, "/connect")
	stateID := consentLoc.Query().Get("state")
	require.NotEmpty(t, stateID)

	// The lazily-upserted row is a CIMD row carrying the fetch stamp.
	clientRow, err := usersessions_repo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            ds.clientID,
	})
	require.NoError(t, err)
	require.Equal(t, ds.clientID, clientRow.ClientIDMetadataUri.String)
	require.True(t, clientRow.ClientIDMetadataFetchedAt.Valid)
	require.Equal(t, "CIMD Integration Client", clientRow.ClientName)
	require.False(t, clientRow.ClientSecretHash.Valid)

	// Consent GET: shows the document host as the verifiable trust anchor
	// plus the loopback-redirect caution.
	consentReq := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/connect?state="+stateID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	consentReq = consentReq.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	cw := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(cw, consentReq))
	require.Equal(t, http.StatusOK, cw.Code)
	docHost := strings.TrimPrefix(ds.srv.URL, "https://")
	require.Contains(t, cw.Body.String(), "Client verified from")
	require.Contains(t, cw.Body.String(), docHost)
	require.Contains(t, cw.Body.String(), "local address on your")
	require.Contains(t, cw.Body.String(), "CIMD Integration Client")

	// Consent POST (approve): mints the authorization code.
	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", stored.CSRFToken)
	form.Set("action", "approve")
	approveReq := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug+"/connect", strings.NewReader(form.Encode()))
	approveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx = chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	approveReq = approveReq.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	aw := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(aw, approveReq))
	require.Equal(t, http.StatusSeeOther, aw.Code)
	codeLoc, err := url.Parse(aw.Header().Get("Location"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(aw.Header().Get("Location"), redirectURI), "code must be delivered to the variable-port loopback redirect")
	code := codeLoc.Query().Get("code")
	require.NotEmpty(t, code)

	// Token: public-client exchange with form client_id and PKCE only.
	tokenForm := url.Values{}
	tokenForm.Set("grant_type", "authorization_code")
	tokenForm.Set("code", code)
	tokenForm.Set("redirect_uri", redirectURI)
	tokenForm.Set("client_id", ds.clientID)
	tokenForm.Set("code_verifier", verifier)
	tokenReq := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug+"/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx = chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	tokenReq = tokenReq.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	tw := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleToken(tw, tokenReq))
	require.Equal(t, http.StatusOK, tw.Code, "token response: %s", tw.Body.String())
	var tokenBody map[string]any
	require.NoError(t, json.Unmarshal(tw.Body.Bytes(), &tokenBody))
	require.NotEmpty(t, tokenBody["access_token"])
}

func TestOAuthCIMD_FlagOff_RejectsURLClientID(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, _ := newTestCIMDService(t)

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:33418/callback", pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusUnauthorized, "invalid_client")
}

// TestOAuthCIMD_FlagOffAfterResolution_RejectsAtAuthorize pins the
// off-switch semantics: once the flag is disabled, even a
// previously-resolved CIMD client is rejected on new authorize flows.
func TestOAuthCIMD_FlagOffAfterResolution_RejectsAtAuthorize(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	redirectURI := "http://127.0.0.1:33418/callback"
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)

	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, false)
	w = doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusUnauthorized, "invalid_client")
}

func TestOAuthCIMD_ConfidentialAuthMethodRejected(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)
	ds.set(t, func(ds *cimdDocServer) { ds.doc["token_endpoint_auth_method"] = "client_secret_basic" })

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:33418/callback", pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_client_metadata")
}

func TestOAuthCIMD_DocumentClientIDMismatchRejected(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)
	ds.set(t, func(ds *cimdDocServer) { ds.doc["client_id"] = ds.srv.URL + "/oauth/other.json" })

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:33418/callback", pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_client_metadata")
}

func TestOAuthCIMD_CrossOriginRedirectURIRejected(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)
	ds.set(t, func(ds *cimdDocServer) { ds.doc["redirect_uris"] = []any{"https://elsewhere.example.com/callback"} })

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "https://elsewhere.example.com/callback", pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_redirect_uri")
}

func TestOAuthCIMD_UnregisteredRedirectURIRejected(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/other-path", pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_request")
}

// TestOAuthCIMD_NonLoopbackPortVarianceRejected: the loopback variable-port
// exception must not leak to non-loopback hosts — a same-origin https
// redirect registered in the document does not match a request that only
// differs by port.
func TestOAuthCIMD_NonLoopbackPortVarianceRejected(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)
	ds.set(t, func(ds *cimdDocServer) { ds.doc["redirect_uris"] = []any{ds.srv.URL + "/callback"} })

	docURL, err := url.Parse(ds.srv.URL)
	require.NoError(t, err)
	variedPort := "https://" + docURL.Hostname() + ":1/callback"

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, variedPort, pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_request")
}

func TestOAuthCIMD_UnknownExtensionFieldsAccepted(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)
	ds.set(t, func(ds *cimdDocServer) { ds.doc["software_statement"] = "eyJhbGciOiJub25lIn0.e30." })
	ds.set(t, func(ds *cimdDocServer) { ds.doc["client_id_expires_at"] = 4102444800 })
	ds.set(t, func(ds *cimdDocServer) { ds.doc["x_vendor_extension"] = map[string]any{"nested": true} })

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:33418/callback", pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code, "documents with unrecognized extension fields must be accepted: %s", w.Body.String())
}

// TestOAuthCIMD_TokenRejectsSecretForCIMDClient: a CIMD row is public by
// construction; presenting any client secret at /token is rejected even
// though the row exists and the secret column is NULL.
func TestOAuthCIMD_TokenRejectsSecretForCIMDClient(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	mcpSlug := toolset.McpSlug.String
	w := doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, "http://127.0.0.1:33418/callback", pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)

	tokenForm := url.Values{}
	tokenForm.Set("grant_type", "authorization_code")
	tokenForm.Set("code", "any-code")
	tokenForm.Set("redirect_uri", "http://127.0.0.1:33418/callback")
	tokenForm.Set("client_id", ds.clientID)
	tokenForm.Set("client_secret", "not-a-real-secret")
	tokenForm.Set("code_verifier", pkceVerifier(t))
	tokenReq := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug+"/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	tokenReq = tokenReq.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	tw := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleToken(tw, tokenReq))
	require.Equal(t, http.StatusUnauthorized, tw.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(tw.Body.Bytes(), &body))
	require.Equal(t, "invalid_client", body["error"])
}

func TestOAuthCIMD_ASMetadataAdvertisesSupportWhenFlagOn(t *testing.T) {
	t.Parallel()

	_, ti, _, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+mcpSlug, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleGetAuthorizationServer(w, req))
	require.Equal(t, http.StatusOK, w.Code)

	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	require.Equal(t, true, meta["client_id_metadata_document_supported"])
	require.Equal(t, []any{"authorization"}, meta["refresh_token_expiration_types_supported"])
}

func TestOAuthCIMD_ASMetadataOmitsSupportWhenFlagOff(t *testing.T) {
	t.Parallel()

	_, ti, _, toolset, _ := newTestCIMDService(t)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+mcpSlug, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleGetAuthorizationServer(w, req))
	require.Equal(t, http.StatusOK, w.Code)

	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	require.NotContains(t, meta, "client_id_metadata_document_supported")
}

func doCIMDConsentGet(t *testing.T, ti *testInstance, mcpSlug, stateID string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/connect?state="+stateID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(w, req))
	return w
}

// getCIMDClientRow reads the persisted client row for the doc server's
// client_id, which every cache assertion is made against.
func getCIMDClientRow(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, clientID string) usersessions_repo.UserSessionClient {
	t.Helper()

	row, err := usersessions_repo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            clientID,
	})
	require.NoError(t, err)
	return row
}

// lapseCIMDCache back-dates a client's cache expiry, leaving its stored
// validator in place so the next authorize revalidates conditionally. Tests
// need it because the floor on a cache lifetime is five minutes, which no
// test can wait out.
//
// It drives the same writer the 304 path uses, with a negative lifetime the
// [5m, 24h] clamp can never produce in production. Nothing in the query
// treats a negative interval specially, so the only effect is landing the
// expiry in the past. The returned row is the state the client is in when the
// authorize under test runs, so assertions about what that authorize changed
// should compare against it rather than against the pre-lapse row.
func lapseCIMDCache(t *testing.T, ctx context.Context, ti *testInstance, client usersessions_repo.UserSessionClient) usersessions_repo.UserSessionClient {
	t.Helper()

	row, err := usersessions_repo.New(ti.conn).UpdateUserSessionClientCIMDCache(ctx, usersessions_repo.UpdateUserSessionClientCIMDCacheParams{
		ID:                   client.ID,
		CacheTtlSeconds:      -60,
		ClientIDMetadataEtag: client.ClientIDMetadataEtag,
	})
	require.NoError(t, err)
	require.True(t, row.ClientIDMetadataCacheExpiresAt.Time.Before(time.Now()), "the cache must actually be lapsed for the test to exercise a refresh")
	return row
}

// TestOAuthCIMD_RepeatAuthorizeServesFromCache pins the headline behaviour: a
// client connecting twice in quick succession costs exactly one upstream
// fetch, and the second authorize answers from the stored row even though the
// document behind it has since changed.
func TestOAuthCIMD_RepeatAuthorizeServesFromCache(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	mcpSlug := toolset.McpSlug.String
	redirectURI := "http://127.0.0.1:33418/callback"

	w := doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	first := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)
	require.True(t, first.ClientIDMetadataCacheExpiresAt.Valid, "the first fetch must establish a cache expiry")
	require.True(t, first.ClientIDMetadataCacheExpiresAt.Time.After(time.Now()), "a freshly written expiry must be in the future")

	ds.set(t, func(ds *cimdDocServer) { ds.doc["client_name"] = "Renamed CIMD Client" })

	w = doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)

	require.Equal(t, int64(1), ds.requests.Load(), "a warm cache must cost no upstream request")
	second := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, "CIMD Integration Client", second.ClientName, "a cache hit writes nothing")
	require.Equal(t, first.ClientIDMetadataFetchedAt.Time, second.ClientIDMetadataFetchedAt.Time)
	require.Equal(t, first.ClientIDMetadataCacheExpiresAt.Time, second.ClientIDMetadataCacheExpiresAt.Time)
}

// TestOAuthCIMD_RepeatAuthorizeRefreshesClient exercises the ON CONFLICT DO
// UPDATE branch of the lazy upsert — the path a returning user hits once the
// cache has lapsed: the refetch must reuse the same row (stable id for
// consent/session FKs) while replacing the mutable metadata.
func TestOAuthCIMD_RepeatAuthorizeRefreshesClient(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	mcpSlug := toolset.McpSlug.String
	redirectURI := "http://127.0.0.1:33418/callback"

	w := doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	first := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)

	lapsed := lapseCIMDCache(t, ctx, ti, first)
	ds.set(t, func(ds *cimdDocServer) { ds.doc["client_name"] = "Renamed CIMD Client" })
	ds.set(t, func(ds *cimdDocServer) { ds.doc["redirect_uris"] = []any{redirectURI, "http://localhost:3000/other"} })

	w = doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)

	require.Equal(t, int64(2), ds.requests.Load(), "a lapsed cache must refetch")
	second := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)
	require.Equal(t, first.ID, second.ID, "repeat authorize must update the existing row, not create a new one")
	require.Equal(t, "Renamed CIMD Client", second.ClientName)
	require.Equal(t, []string{redirectURI, "http://localhost:3000/other"}, second.RedirectUris)
	require.True(t, second.ClientIDMetadataFetchedAt.Time.After(lapsed.ClientIDMetadataFetchedAt.Time), "fetch stamp must be refreshed")
	require.True(t, second.ClientIDMetadataCacheExpiresAt.Time.After(time.Now()), "the refetch must re-establish the expiry")
}

// TestOAuthCIMD_LapsedCacheRevalidatesWithETag drives the full validator
// round trip: the first authorize stores the host's ETag, and the refresh
// after the cache lapses is conditional. The 304 must leave the stored
// document alone — including a client_name the host has since changed, which
// it never gets to report because the body is not re-sent.
func TestOAuthCIMD_LapsedCacheRevalidatesWithETag(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)
	ds.set(t, func(ds *cimdDocServer) { ds.etag = `"v1"` })

	mcpSlug := toolset.McpSlug.String
	redirectURI := "http://127.0.0.1:33418/callback"

	w := doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	first := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)
	require.Equal(t, `"v1"`, first.ClientIDMetadataEtag.String, "the host's validator must be persisted")

	lapsed := lapseCIMDCache(t, ctx, ti, first)
	ds.set(t, func(ds *cimdDocServer) { ds.doc["client_name"] = "Never Served" })

	w = doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)

	require.Equal(t, int64(2), ds.requests.Load())
	require.Equal(t, int64(1), ds.conditionalRequests.Load(), "the stored validator must be replayed")

	second := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, "CIMD Integration Client", second.ClientName, "a 304 must not blow away the cached document")
	require.Equal(t, []string{redirectURI}, second.RedirectUris)
	require.Equal(t, `"v1"`, second.ClientIDMetadataEtag.String)
	require.True(t, second.ClientIDMetadataFetchedAt.Time.After(lapsed.ClientIDMetadataFetchedAt.Time), "a revalidation is a successful fetch")
	require.True(t, second.ClientIDMetadataCacheExpiresAt.Time.After(time.Now()), "a 304 must re-establish the expiry")
}

// TestOAuthCIMD_RevalidationPicksUpRotatedDocument is the other half of the
// conditional refresh: when the host's validator has moved on it answers the
// conditional request with a body, and the rotated metadata must land in the
// row. This is how a client that changes its redirect_uris keeps working.
func TestOAuthCIMD_RevalidationPicksUpRotatedDocument(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)
	ds.set(t, func(ds *cimdDocServer) { ds.etag = `"v1"` })

	mcpSlug := toolset.McpSlug.String
	redirectURI := "http://127.0.0.1:33418/callback"

	w := doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	first := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)

	lapseCIMDCache(t, ctx, ti, first)
	ds.set(t, func(ds *cimdDocServer) { ds.etag = `"v2"` })
	ds.set(t, func(ds *cimdDocServer) { ds.doc["client_name"] = "Rotated CIMD Client" })
	ds.set(t, func(ds *cimdDocServer) { ds.doc["redirect_uris"] = []any{redirectURI, "http://localhost:3000/other"} })

	w = doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)

	require.Equal(t, int64(1), ds.conditionalRequests.Load(), "the stale validator is still offered")
	second := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, "Rotated CIMD Client", second.ClientName)
	require.Equal(t, []string{redirectURI, "http://localhost:3000/other"}, second.RedirectUris)
	require.Equal(t, `"v2"`, second.ClientIDMetadataEtag.String, "the superseding validator replaces the stored one")
}

// TestOAuthCIMD_PurgeForcesUnconditionalReread pins the ops lever: purging
// clears the validator along with the expiry, so the next authorize re-reads
// and re-validates a full body rather than accepting a 304 that would confirm
// the very document being purged.
func TestOAuthCIMD_PurgeForcesUnconditionalReread(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)
	ds.set(t, func(ds *cimdDocServer) { ds.etag = `"v1"` })

	mcpSlug := toolset.McpSlug.String
	redirectURI := "http://127.0.0.1:33418/callback"

	w := doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	first := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)
	require.Equal(t, `"v1"`, first.ClientIDMetadataEtag.String)

	purged, err := usersessions_repo.New(ti.conn).PurgeUserSessionClientCIMDCache(ctx, usersessions_repo.PurgeUserSessionClientCIMDCacheParams{
		ID:        first.ID,
		ProjectID: first.ProjectID,
	})
	require.NoError(t, err)
	require.False(t, purged.ClientIDMetadataCacheExpiresAt.Valid)
	require.False(t, purged.ClientIDMetadataEtag.Valid)

	ds.set(t, func(ds *cimdDocServer) { ds.doc["client_name"] = "Renamed After Purge" })

	w = doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)

	require.Equal(t, int64(2), ds.requests.Load())
	require.Zero(t, ds.conditionalRequests.Load(), "a purge must leave nothing to revalidate against")
	second := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)
	require.Equal(t, "Renamed After Purge", second.ClientName)
	require.Equal(t, `"v1"`, second.ClientIDMetadataEtag.String, "the re-read stores the host's validator again")
}

// TestOAuthCIMD_UpstreamMaxAgeClampedToFloor pins the lower bound: a host
// asking for two minutes gets the five minute floor, so an unauthenticated
// endpoint cannot be pushed into fetching on every authorize.
func TestOAuthCIMD_UpstreamMaxAgeClampedToFloor(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)
	ds.set(t, func(ds *cimdDocServer) { ds.cacheControl = "public, max-age=120" })

	before := time.Now()
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:33418/callback", pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)

	row := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)
	require.True(t, row.ClientIDMetadataCacheExpiresAt.Time.After(before.Add(4*time.Minute)), "max-age below the floor must be raised to it")
	require.True(t, row.ClientIDMetadataCacheExpiresAt.Time.Before(before.Add(6*time.Minute)), "the floor must not be exceeded either")
}

// TestOAuthCIMD_RefreshFailureDoesNotPoisonCache pins the fail-closed rule: a
// failing document host aborts the authorize (-02 §5.1) and leaves every
// cache column exactly as it was, so nothing stale is served and no expiry is
// silently extended.
func TestOAuthCIMD_RefreshFailureDoesNotPoisonCache(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	mcpSlug := toolset.McpSlug.String
	redirectURI := "http://127.0.0.1:33418/callback"

	w := doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	first := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)

	lapsed := lapseCIMDCache(t, ctx, ti, first)
	ds.set(t, func(ds *cimdDocServer) { ds.status = http.StatusInternalServerError })

	w = doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusServiceUnavailable, w.Code, "a failed refresh aborts rather than serving stale")

	second := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)
	require.Equal(t, "CIMD Integration Client", second.ClientName)
	require.True(t, second.ClientIDMetadataCacheExpiresAt.Time.Equal(lapsed.ClientIDMetadataCacheExpiresAt.Time), "a failed refresh must not extend the expiry")
	require.Equal(t, lapsed.ClientIDMetadataFetchedAt.Time, second.ClientIDMetadataFetchedAt.Time, "a failed refresh must not move the fetch stamp")
}

// TestOAuthCIMD_InvalidDocumentOnRefreshDoesNotPoisonCache is the same rule
// for a host that answers 200 with something unparseable: an error response
// and an invalid document are both uncacheable (-02 §5.2).
func TestOAuthCIMD_InvalidDocumentOnRefreshDoesNotPoisonCache(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	mcpSlug := toolset.McpSlug.String
	redirectURI := "http://127.0.0.1:33418/callback"

	w := doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	first := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)

	lapsed := lapseCIMDCache(t, ctx, ti, first)
	// A document that no longer satisfies the spec: -02 §4.1 forbids a
	// client_secret in a document that is public by definition.
	ds.set(t, func(ds *cimdDocServer) { ds.doc["client_secret"] = "leaked" })

	w = doCIMDAuthorize(t, ti, mcpSlug, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_client_metadata")

	second := getCIMDClientRow(t, ctx, ti, toolset, ds.clientID)
	require.Equal(t, "CIMD Integration Client", second.ClientName)
	require.True(t, second.ClientIDMetadataCacheExpiresAt.Time.Equal(lapsed.ClientIDMetadataCacheExpiresAt.Time), "an invalid document must not refresh the expiry")
}

// TestOAuthCIMD_SecretBearingCollisionRejected pins the upsert guard: a
// pre-existing row sharing the client_id but carrying a secret hash must
// surface as invalid_client, not as a CHECK-constraint 500 and not as a
// silent rewrite of a confidential client into a CIMD one.
func TestOAuthCIMD_SecretBearingCollisionRejected(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	_, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:   toolset.UserSessionIssuerID.UUID,
		ClientID:              ds.clientID,
		ClientSecretHash:      conv.ToPGText("bcrypt-hash-placeholder"),
		ClientName:            "Confidential DCR Client",
		RedirectUris:          []string{"http://127.0.0.1:33418/callback"},
		ClientSecretExpiresAt: pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:33418/callback", pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusUnauthorized, "invalid_client")

	row, err := usersessions_repo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            ds.clientID,
	})
	require.NoError(t, err)
	require.Equal(t, "Confidential DCR Client", row.ClientName, "the secret-bearing row must be left untouched")
	require.False(t, row.ClientIDMetadataUri.Valid)
}

// TestOAuthCIMD_LoopbackQueryInjectionRejected: the loopback carve-out may
// vary ONLY the port — extra query parameters on the requested redirect_uri
// must not match a registered URI without them.
func TestOAuthCIMD_LoopbackQueryInjectionRejected(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback?injected=1", pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_request")
}

// TestOAuthCIMD_LoopbackUserinfoRejected: userinfo in the requested
// redirect_uri must never match — it would make the victim's browser send
// attacker-chosen Basic credentials to the local listener.
func TestOAuthCIMD_LoopbackUserinfoRejected(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://user:pass@127.0.0.1:51423/callback", pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_request")
}

// TestOAuthDCR_LoopbackPortVarianceRejected: the loopback variable-port
// carve-out is CIMD-only; DCR-registered rows keep byte-exact matching.
func TestOAuthDCR_LoopbackPortVarianceRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)

	// The seeded DCR client registers http://localhost:3000/callback.
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, client.ClientID, "http://localhost:9999/callback", pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_request")
}

// TestOAuthCIMD_FetchFailureReturns503 pins the fetch-failure wire contract:
// retryable status + code (a document host blip must not read as a permanent
// invalid_client) and a generic description with no internal error detail.
func TestOAuthCIMD_FetchFailureReturns503(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	missingDocURL := ds.srv.URL + "/missing.json"
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, missingDocURL, "http://127.0.0.1:33418/callback", pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "temporarily_unavailable", body["error"])
	require.Equal(t, "failed to fetch client metadata document", body["error_description"])
}

// TestOAuthCIMD_ConsentEscapesHostileClientName: client_name is
// attacker-chosen for any accepted document; the consent page must render it
// inert.
func TestOAuthCIMD_ConsentEscapesHostileClientName(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)
	ds.set(t, func(ds *cimdDocServer) { ds.doc["client_name"] = `<img src=x onerror=alert(1)> Client` })

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:33418/callback", pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	consentLoc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)

	cw := doCIMDConsentGet(t, ti, toolset.McpSlug.String, consentLoc.Query().Get("state"))
	require.Equal(t, http.StatusOK, cw.Code)
	require.NotContains(t, cw.Body.String(), "<img src=x")
	require.Contains(t, cw.Body.String(), "&lt;img src=x onerror=alert(1)&gt; Client")
}

// TestOAuthCIMD_ConsentNoLoopbackWarningForSameOriginRedirect: the loopback
// caution is scoped to loopback redirects; a same-origin https redirect
// shows the trust anchor without the warning.
func TestOAuthCIMD_ConsentNoLoopbackWarningForSameOriginRedirect(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)
	redirectURI := ds.srv.URL + "/callback"
	ds.set(t, func(ds *cimdDocServer) { ds.doc["redirect_uris"] = []any{redirectURI} })

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	consentLoc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)

	cw := doCIMDConsentGet(t, ti, toolset.McpSlug.String, consentLoc.Query().Get("state"))
	require.Equal(t, http.StatusOK, cw.Code)
	require.Contains(t, cw.Body.String(), "Client verified from")
	require.NotContains(t, cw.Body.String(), "local address on your")
}

// TestOAuthCIMD_LoopbackEncodedPathRejected: the variable-port exception
// compares escaped components, so a percent-encoding variant of a registered
// path must not match.
func TestOAuthCIMD_LoopbackEncodedPathRejected(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	// The document registers /callback; /%63allback decodes to the same
	// path but is a different URI byte-for-byte.
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/%63allback", pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_request")
}

// TestOAuthCIMD_LoopbackFragmentRejected: a fragment on the requested
// redirect_uri (prohibited by RFC 6749 section 3.1.2) must not match a
// registered URI without one.
func TestOAuthCIMD_LoopbackFragmentRejected(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback#frag", pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_request")
}

// TestOAuthCIMD_LoopbackEmptyFragmentRegistrationRejected: a registered URI
// carrying an explicit empty fragment ("...#") must not satisfy the
// variable-port exception for a fragment-less request. URL.String() drops a
// bare "#", so without the raw-string fragment guard the two would compare
// equal despite differing by more than the port.
func TestOAuthCIMD_LoopbackEmptyFragmentRegistrationRejected(t *testing.T) {
	t.Parallel()

	_, ti, ds, toolset, orgID := newTestCIMDService(t)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, orgID, true)
	ds.set(t, func(ds *cimdDocServer) { ds.doc["redirect_uris"] = []any{"http://127.0.0.1:33418/callback#"} })

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:51423/callback", pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_request")
}
