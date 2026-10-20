package mcp_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

func attachedMCPMux(ti *testInstance) goahttp.Muxer {
	mux := goahttp.NewMuxer()
	mcp.Attach(mux, ti.service, nil)
	return mux
}

func serveAttached(t *testing.T, ctx context.Context, mux goahttp.Muxer, method, target, bearer string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func mintEndpointUserSession(t *testing.T, ti *testInstance, issuerID uuid.UUID, slug string, subject urn.SessionSubject) string {
	t.Helper()

	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  subject,
		Audience: urn.NewUserSessionIssuer(issuerID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, subject, jti)
	return token
}

// A Speakeasy API key from another project in the same org is refused at the
// issuer gate, so no Speakeasy credential ever reaches the upstream.
func TestServePublic_McpEndpoint_PrivateRemote_SameOrgCrossProjectAPIKeyRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	upstreamHit := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHit <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	otherSlug := "other-project-" + uuid.NewString()[:8]
	other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           otherSlug,
		Slug:           otherSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, other.ID)
	slug := "cross-project-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, other.ID, upstream.URL, slug, "private", issuerID)
	key := ti.createTestAPIKey(ctx, t)

	w := serveAttached(t, ctx, attachedMCPMux(ti), http.MethodPost, "/mcp/"+slug, key, makeInitializeBody(), nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, "body=%s", w.Body.String())
	select {
	case <-upstreamHit:
		t.Fatal("a rejected API key must never reach the upstream")
	default:
	}
}

func TestServePublic_McpEndpoint_PublicRemote_DeleteForwardsSessionTermination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	var gotMethod, gotSession string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotSession = r.Header.Get("Mcp-Session-Id")
		w.WriteHeader(http.StatusNoContent)
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "delete-forward-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, slug, "public", issuerID)
	token := mintEndpointUserSession(t, ti, issuerID, slug, urn.NewAnonymousSubject(uuid.NewString()))

	w := serveAttached(t, context.Background(), attachedMCPMux(ti), http.MethodDelete, "/mcp/"+slug, token, nil, map[string]string{"Mcp-Session-Id": "abc-session"})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("upstream not invoked; status=%d body=%s", w.Code, w.Body.String())
	}
	require.Equal(t, http.StatusNoContent, w.Code, "body=%s", w.Body.String())
	require.Equal(t, http.MethodDelete, gotMethod)
	require.Equal(t, "abc-session", gotSession)
}

func TestServePublic_McpEndpoint_IssuerGatedRemote_CrossIssuerTokenRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	issuerA := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slugA := "issuer-a-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, slugA, "public", issuerA)

	issuerB := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slugB := "issuer-b-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, slugB, "public", issuerB)

	token := mintEndpointUserSession(t, ti, issuerA, slugA, urn.NewAnonymousSubject(uuid.NewString()))

	w := serveAttached(t, context.Background(), attachedMCPMux(ti), http.MethodPost, "/mcp/"+slugB, token, makeInitializeBody(), nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, "issuer-A bearer must not authorise issuer-B endpoint; body=%s", w.Body.String())
	require.Equal(t, fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp/%s"`, ti.serverURL.String(), slugB), w.Header().Get("WWW-Authenticate"))
}

// RFC 9728: the challenge's resource_metadata URL must serve a document whose
// resource is the URL the client addressed.
func TestServePublic_McpEndpoint_IssuerGatedRFC9728Invariant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "rfc9728-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "public", issuerID)
	mux := attachedMCPMux(ti)

	w := serveAttached(t, context.Background(), mux, http.MethodPost, "/mcp/"+slug, "", makeInitializeBody(), nil)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	metadataURL := ti.serverURL.String() + "/.well-known/oauth-protected-resource/mcp/" + slug
	require.Equal(t, `Bearer resource_metadata="`+metadataURL+`"`, w.Header().Get("WWW-Authenticate"))

	parsed, err := url.Parse(metadataURL)
	require.NoError(t, err)
	w = serveAttached(t, ctx, mux, http.MethodGet, parsed.Path, "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, ti.serverURL.String()+"/mcp/"+slug, metadata["resource"])
}

// A cached challenge whose mcp_server no longer matches the live endpoint
// cannot resume.
func TestHandleIDPCallback_McpServerMismatchRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "idp-mismatch-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "public", issuerID)

	challengeID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: issuerID,
		Endpoint: mcp.EndpointRef{
			McpSlug:        slug,
			CustomDomainID: uuid.NullUUID{},
			McpServerID:    uuid.NullUUID{UUID: uuid.New(), Valid: true},
			RouteBase:      mcproute.MCP,
		},
		ClientID:            "test-client",
		RedirectURI:         "http://localhost:3000/callback",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		CreatedAt:           time.Now(),
	}))

	q := url.Values{"state": {challengeID}, "code": {"idp-auth-code-mismatch"}}
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/mcp/idp_callback?"+q.Encode(), nil)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, chi.NewRouteContext()))

	err := ti.service.HandleIDPCallback(httptest.NewRecorder(), req)
	require.Error(t, err, "callback against a mismatching mcp_server ref must fail")
	require.Contains(t, err.Error(), "does not match")
}

// Drives register → authorize → consent → token → runtime through the
// production /mcp routes for a public remote backend.
func TestAttach_OAuthFullDance_PublicRemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "full-dance-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, slug, "public", issuerID)
	mux := attachedMCPMux(ti)

	const clientRedirectURI = "http://localhost:3000/callback"

	regBody := []byte(`{"client_name":"full dance","redirect_uris":["` + clientRedirectURI + `"],"token_endpoint_auth_method":"none"}`)
	regW := serveAttached(t, ctx, mux, http.MethodPost, "/mcp/"+slug+"/register", "", regBody, nil)
	require.Equal(t, http.StatusCreated, regW.Code, "register; body=%s", regW.Body.String())
	var regResp struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(regW.Body.Bytes(), &regResp))
	require.NotEmpty(t, regResp.ClientID)

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {regResp.ClientID},
		"redirect_uri":          {clientRedirectURI},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"state":                 {"client-state"},
	}
	authW := serveAttached(t, ctx, mux, http.MethodGet, "/mcp/"+slug+"/authorize?"+q.Encode(), "", nil, nil)
	require.Equal(t, http.StatusFound, authW.Code, "authorize; body=%s", authW.Body.String())
	consentLoc, err := url.Parse(authW.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/mcp/"+slug+"/connect", consentLoc.Path)
	challengeID := consentLoc.Query().Get("state")
	require.NotEmpty(t, challengeID)

	consentGetW := serveAttached(t, ctx, mux, http.MethodGet, consentLoc.RequestURI(), "", nil, nil)
	require.Equal(t, http.StatusOK, consentGetW.Code, "consent GET; body=%s", consentGetW.Body.String())
	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+challengeID)
	require.NoError(t, err)
	require.NotEmpty(t, stored.CSRFToken)

	consentForm := url.Values{"state": {challengeID}, "csrf_token": {stored.CSRFToken}, "action": {"approve"}}
	consentReq := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(consentForm.Encode()))
	consentReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	consentPostW := httptest.NewRecorder()
	mux.ServeHTTP(consentPostW, consentReq)
	require.Equal(t, http.StatusSeeOther, consentPostW.Code, "consent POST; body=%s", consentPostW.Body.String())
	redirectLoc, err := url.Parse(consentPostW.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "client-state", redirectLoc.Query().Get("state"))
	code := redirectLoc.Query().Get("code")
	require.NotEmpty(t, code, "consent POST must mint an auth code; redirect=%s", redirectLoc.String())

	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {clientRedirectURI},
		"client_id":     {regResp.ClientID},
		"code_verifier": {verifier},
	}
	tokenReq := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/"+slug+"/token", strings.NewReader(tokenForm.Encode()))
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

	rr := serveAttached(t, context.Background(), mux, http.MethodPost, "/mcp/"+slug, tokResp.AccessToken, makeInitializeBody(), nil)
	require.Equal(t, http.StatusOK, rr.Code, "runtime after full dance; body=%s", rr.Body.String())
}

func TestHandleGetAuthorizationServer_IssuerGatedRemote_CIMDAdvertisement(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mode       admission.Mode
		advertised bool
	}{
		"default":  {mode: "", advertised: true},
		"disabled": {mode: admission.ModeDisabled, advertised: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.ProjectID)

			issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
			slug := "cimd-remote-" + uuid.NewString()
			createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "public", issuerID)
			if tc.mode != "" {
				require.NoError(t, testrepo.New(ti.conn).SetUserSessionIssuerCIMDAdmissionMode(ctx, testrepo.SetUserSessionIssuerCIMDAdmissionModeParams{
					ClientIDMetadataAdmissionMode: conv.ToPGText(string(tc.mode)),
					ID:                            issuerID,
					ProjectID:                     *authCtx.ProjectID,
				}))
			}

			w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code)
			var metadata map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
			if tc.advertised {
				require.Equal(t, true, metadata["client_id_metadata_document_supported"])
			} else {
				require.NotContains(t, metadata, "client_id_metadata_document_supported")
			}
		})
	}
}
