// cimd_test.go covers outbound OAuth Client ID Metadata Document (CIMD)
// support: the document builder + public endpoint, the createCimd management
// path, and the outbound invariant that a CIMD client sends its document URL as
// client_id and never reaches HTTP Basic auth.

package remotesessions_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const cimdServerURL = testServerURL // https://app.getgram.ai

// createCIMDIssuer seeds an issuer that advertises CIMD support and the "none"
// token endpoint auth method, with the supplied authorize/token endpoints.
func createCIMDIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug, authEP, tokenEP string) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuer, err := repo.New(ti.conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             conv.ToPGText(authEP),
		TokenEndpoint:                     conv.ToPGText(tokenEP),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		ClientIDMetadataDocumentSupported: true,
	})
	require.NoError(t, err)
	return issuer.ID
}

// createOrgLevelCIMDIssuer seeds an organization-level (project_id NULL) issuer
// that advertises CIMD support, for exercising the org-admin createCimdClient
// project-resolution path.
func createOrgLevelCIMDIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug string) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuer, err := repo.New(ti.conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{},
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		ClientIDMetadataDocumentSupported: true,
	})
	require.NoError(t, err)
	return issuer.ID
}

// createCimdClient creates a CIMD-mode client through the management API and
// returns its view.
func createCimdClient(t *testing.T, ctx context.Context, ti *testInstance, issuerID, userIssuerID string, scope []string) *types.RemoteSessionClient {
	t.Helper()
	created, err := ti.service.CreateCimd(ctx, &clientsgen.CreateCimdPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		Scope:                 scope,
	})
	require.NoError(t, err)
	return created
}

// newCIMDChallengeManager builds a ChallengeManager sharing the test db, wired
// to the supplied base URL so its served document client_id and callback match
// the URLs the management API stamped on CIMD rows.
func newCIMDChallengeManager(t *testing.T, ti *testInstance, serverURL string) *remotesessions.ChallengeManager {
	t.Helper()
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	return remotesessions.NewChallengeManager(
		testenv.NewLogger(t),
		ti.conn,
		testenv.NewEncryptionClient(t),
		policy,
		cache.NoopCache,
		mustURL(t, serverURL),
	)
}

// cimdDocumentRequest builds a GET request for the CIMD document endpoint with
// the chi {id} route param populated, optionally tagged with a custom-domain
// context so the handler's platform-host pinning can be exercised.
func cimdDocumentRequest(t *testing.T, id string, customDomain bool) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-client/"+id, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if customDomain {
		ctx = customdomains.WithContext(ctx, &customdomains.Context{
			OrganizationID: "org-cimd",
			Domain:         "mcp.customer.example.com",
			DomainID:       uuid.New(),
		})
	}
	return req.WithContext(ctx)
}

// --- Builder + URL helpers -------------------------------------------------

func TestBuildClientMetadataDocument(t *testing.T) {
	t.Parallel()

	const clientID = "https://app.getgram.ai/.well-known/oauth-client/abc"
	const redirectURI = "https://app.getgram.ai/mcp/remote_login_callback"

	doc := remotesessions.BuildClientMetadataDocument(clientID, redirectURI, []string{"read", "write"})

	body, err := json.Marshal(doc)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))

	require.Equal(t, clientID, got["client_id"], "client_id must equal the document URL")
	require.Equal(t, "Speakeasy", got["client_name"])
	require.Equal(t, []any{redirectURI}, got["redirect_uris"])
	require.Equal(t, []any{"authorization_code", "refresh_token"}, got["grant_types"])
	require.Equal(t, []any{"code"}, got["response_types"])
	require.Equal(t, "none", got["token_endpoint_auth_method"], "CIMD is public-mode only")
	require.Equal(t, "read write", got["scope"], "scope is space-delimited per RFC 7591")
}

func TestBuildClientMetadataDocument_OmitsEmptyScope(t *testing.T) {
	t.Parallel()

	doc := remotesessions.BuildClientMetadataDocument("https://app.getgram.ai/.well-known/oauth-client/abc", "https://app.getgram.ai/mcp/remote_login_callback", nil)

	body, err := json.Marshal(doc)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	_, present := got["scope"]
	require.False(t, present, "scope must be omitted when the client has no explicit scopes")
}

func TestClientMetadataDocumentURL(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	require.Equal(t, "https://app.getgram.ai/.well-known/oauth-client/"+id.String(), remotesessions.ClientMetadataDocumentURL(mustURL(t, "https://app.getgram.ai"), id))
	// Trailing slash on the base URL must not double up.
	require.Equal(t, "https://app.getgram.ai/.well-known/oauth-client/"+id.String(), remotesessions.ClientMetadataDocumentURL(mustURL(t, "https://app.getgram.ai/"), id))
}

// --- Public document endpoint ----------------------------------------------

func TestHandleClientMetadataDocument_ServesDocument(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createCIMDIssuer(t, ctx, ti, "cimd-serve", "https://idp.example.com/authorize", "https://idp.example.com/token")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "cimd-serve-usi")

	created := createCimdClient(t, ctx, ti, issuerID.String(), userIssuer.String(), []string{"read:tools"})
	require.NotNil(t, created.ClientIDMetadataURI)

	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)
	rec := httptest.NewRecorder()
	require.NoError(t, mgr.HandleClientMetadataDocument(rec, cimdDocumentRequest(t, created.ID, false)))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Equal(t, "public, max-age=3600", rec.Header().Get("Cache-Control"))
	require.NotEmpty(t, rec.Header().Get("ETag"))

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, *created.ClientIDMetadataURI, got["client_id"], "served client_id must equal the stored canonical URL")
	redirectURIs, ok := got["redirect_uris"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{cimdServerURL + "/mcp/remote_login_callback"}, redirectURIs)
	require.Equal(t, "none", got["token_endpoint_auth_method"])
	require.Equal(t, "read:tools", got["scope"])
}

func TestHandleClientMetadataDocument_NotFoundUnknownID(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)
	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)

	err := mgr.HandleClientMetadataDocument(httptest.NewRecorder(), cimdDocumentRequest(t, uuid.NewString(), false))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestHandleClientMetadataDocument_NotFoundInvalidID(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)
	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)

	err := mgr.HandleClientMetadataDocument(httptest.NewRecorder(), cimdDocumentRequest(t, "not-a-uuid", false))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestHandleClientMetadataDocument_NotFoundNonCIMDClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createRemoteIssuer(t, ctx, ti, "cimd-nonmode-issuer", "")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "cimd-nonmode-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuer.String(), "manual-cid")

	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)
	err := mgr.HandleClientMetadataDocument(httptest.NewRecorder(), cimdDocumentRequest(t, clientID, false))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestHandleClientMetadataDocument_NotFoundOnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createCIMDIssuer(t, ctx, ti, "cimd-domain", "https://idp.example.com/authorize", "https://idp.example.com/token")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "cimd-domain-usi")

	created := createCimdClient(t, ctx, ti, issuerID.String(), userIssuer.String(), nil)

	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)
	// Reached via a verified custom domain: the document is pinned to the
	// platform host, so this 404s even for a valid CIMD client id.
	err := mgr.HandleClientMetadataDocument(httptest.NewRecorder(), cimdDocumentRequest(t, created.ID, true))
	requireOopsCode(t, err, oops.CodeNotFound)
}

// --- createCimd ------------------------------------------------------------

func TestCreateCimd(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createCIMDIssuer(t, ctx, ti, "cimd-create", "https://idp.example.com/authorize", "https://idp.example.com/token")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "cimd-create-usi")

	created, err := ti.service.CreateCimd(ctx, &clientsgen.CreateCimdPayload{
		RemoteSessionIssuerID: issuerID.String(),
		UserSessionIssuerIds:  []string{userIssuer.String()},
	})
	require.NoError(t, err)

	wantURL := cimdServerURL + "/.well-known/oauth-client/" + created.ID
	require.NotNil(t, created.ClientIDMetadataURI)
	require.Equal(t, wantURL, *created.ClientIDMetadataURI)
	require.Equal(t, wantURL, created.ClientID, "client_id must equal the document URL")
	require.NotNil(t, created.TokenEndpointAuthMethod)
	require.Equal(t, "none", *created.TokenEndpointAuthMethod)

	// The stored row carries no secret (the CHECK constraint would have rejected
	// the write otherwise).
	row, err := repo.New(ti.conn).GetRemoteSessionClientForClientMetadataDocument(ctx, uuid.MustParse(created.ID))
	require.NoError(t, err)
	require.Equal(t, wantURL, row.ClientIDMetadataUri.String)
}

func TestCreateCimd_RejectedWhenIssuerUnsupported(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	// createRemoteIssuer advertises client_secret_basic and no CIMD support.
	issuerID := createRemoteIssuer(t, ctx, ti, "cimd-unsupported", "")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "cimd-unsupported-usi")

	_, err := ti.service.CreateCimd(ctx, &clientsgen.CreateCimdPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuer.String()},
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// --- Outbound flow ---------------------------------------------------------

func TestCIMD_BuildAuthorizationUrlUsesMetadataURLAsClientID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createCIMDIssuer(t, ctx, ti, "cimd-authz", "https://idp.example.com/authorize", "https://idp.example.com/token")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "cimd-authz-usi")

	created := createCimdClient(t, ctx, ti, issuerID.String(), userIssuer.String(), nil)

	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)
	clients, err := mgr.ListClients(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuer)
	require.NoError(t, err)
	require.Len(t, clients, 1)

	subject := urn.NewUserSubject("cimd-authz-subject")
	authURL, err := mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID:                  uuid.NewString(),
		ProjectID:           *authCtx.ProjectID,
		UserSessionIssuerID: userIssuer,
		Subject:             &subject,
	}, clients[0])
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.NotNil(t, created.ClientIDMetadataURI)
	require.Equal(t, *created.ClientIDMetadataURI, parsed.Query().Get("client_id"), "authorize leg must send the CIMD document URL as client_id")
}

func TestCIMD_RefreshUsesMetadataURLAsClientIDWithoutBasicAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	var spy upstreamSpy
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			spy.handlerErr = err
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			spy.handlerErr = err
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		spy.form = form
		spy.authHdr = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"refreshed-access","token_type":"Bearer","expires_in":3600,"refresh_token":"refreshed-refresh"}`))
	}))
	t.Cleanup(tokenServer.Close)

	// One encryption client shared between the manager (which decrypts the
	// seeded refresh token) and the session seeding below.
	enc := testenv.NewEncryptionClient(t)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	mgr := remotesessions.NewChallengeManager(testenv.NewLogger(t), ti.conn, enc, policy, cache.NoopCache, mustURL(t, cimdServerURL))

	issuerID := createCIMDIssuer(t, ctx, ti, "cimd-refresh", tokenServer.URL+"/authorize", tokenServer.URL+"/token")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "cimd-refresh-usi")

	created := createCimdClient(t, ctx, ti, issuerID.String(), userIssuer.String(), nil)
	require.NotNil(t, created.ClientIDMetadataURI)

	subject := urn.NewUserSubject("cimd-refresh-subject")
	seedExpiredRemoteSession(t, ctx, ti, enc, subject, userIssuer, uuid.MustParse(created.ID))

	tok, err := mgr.ResolveAccessToken(ctx, uuid.MustParse(created.ID), subject, "")
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	require.Equal(t, *created.ClientIDMetadataURI, spy.form.Get("client_id"), "token leg must send the CIMD document URL as client_id")
	require.Empty(t, spy.authHdr, "CIMD is public-mode: the token request must never use HTTP Basic auth")
	require.Empty(t, spy.form.Get("client_secret"), "CIMD clients carry no secret to send in the body")
}

// --- Org-admin createCimdClient -------------------------------------------

func TestCreateCimdClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createCIMDIssuer(t, ctx, ti, "admin-cimd-create", "https://idp.example.com/authorize", "https://idp.example.com/token")

	created, err := ti.service.CreateCimdClient(ctx, &orggen.CreateCimdClientPayload{
		RemoteSessionIssuerID: issuerID.String(),
	})
	require.NoError(t, err)

	wantURL := cimdServerURL + "/.well-known/oauth-client/" + created.ID
	require.Equal(t, authCtx.ProjectID.String(), created.ProjectID, "project-specific issuer's client inherits its project")
	require.NotNil(t, created.ClientIDMetadataURI)
	require.Equal(t, wantURL, *created.ClientIDMetadataURI)
	require.Equal(t, wantURL, created.ClientID)
	require.NotNil(t, created.TokenEndpointAuthMethod)
	require.Equal(t, "none", *created.TokenEndpointAuthMethod)
	require.Empty(t, created.UserSessionIssuerIds, "standalone client has no attachments")
}

func TestCreateCimdClient_RejectedWhenIssuerUnsupported(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	// createRemoteIssuer advertises client_secret_basic and no CIMD support.
	issuerID := createRemoteIssuer(t, ctx, ti, "admin-cimd-unsupported", "")

	_, err := ti.service.CreateCimdClient(ctx, &orggen.CreateCimdClientPayload{
		RemoteSessionIssuerID: issuerID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateCimdClient_OrganizationalIssuerDownscope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	pid := authCtx.ProjectID.String()

	issuerID := createOrgLevelCIMDIssuer(t, ctx, ti, "admin-cimd-orglevel")

	created, err := ti.service.CreateCimdClient(ctx, &orggen.CreateCimdClientPayload{
		RemoteSessionIssuerID: issuerID.String(),
		ProjectID:             &pid,
	})
	require.NoError(t, err)
	require.Equal(t, pid, created.ProjectID)
	require.NotNil(t, created.ClientIDMetadataURI)
}

func TestCreateCimdClient_OrganizationalIssuerNoProjectCreatesOrgLevel(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createOrgLevelCIMDIssuer(t, ctx, ti, "admin-cimd-orglevel-noproj")

	created, err := ti.service.CreateCimdClient(ctx, &orggen.CreateCimdClientPayload{
		RemoteSessionIssuerID: issuerID.String(),
	})
	require.NoError(t, err)
	require.Empty(t, created.ProjectID, "organization-level client has no project")
	require.Equal(t, authCtx.ActiveOrganizationID, created.OrganizationID)
	require.NotNil(t, created.ClientIDMetadataURI)
}
