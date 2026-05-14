// remotelogin_integration_test.go drives the /mcp/{slug}/remote_login_callback
// handler against a live dev-idp instance acting as the upstream OAuth
// authorization server. Covers the two subject shapes the callback must
// attribute remote_sessions rows to:
//
//   - authenticated: AuthnChallengeState pre-stamped with a user:<id> URN
//     (what HandleIDPCallback does on the private-toolset path).
//   - anonymous: public toolset, no subject on the parent challenge at
//     /authorize time — handleConsentGet late-binds an anonymous URN and
//     persists it back to Redis before rendering the per-remote cards.
package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// TestRemoteLoginCallback_AuthenticatedSubject covers the private-toolset
// path where HandleIDPCallback has already stamped a user:<id> URN onto the
// parent AuthnChallengeState. The callback must persist a remote_sessions
// row keyed to that user.
func TestRemoteLoginCallback_AuthenticatedSubject(t *testing.T) {
	t.Parallel()

	idp := devidptest.Launch(t, devidptest.LaunchOpts{})
	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateIssuerGatedToolset(t, ctx, ti.conn, ti.enc, authCtx, oauthtest.IssuerGatedToolsetOpts{
		Slug:                         "issuer-auth",
		IsPublic:                     false,
		UpstreamMetadata:             idp.OAuth21Metadata(t),
		RemoteSessionCallbackBaseURL: ti.serverURL.String(),
	})

	mgr, authnCache := buildChallengeManagerForTest(t, ti)

	userSubject := urn.NewUserSubject(uuid.NewString())
	parentID := uuid.NewString()
	require.NoError(t, authnCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  parentID,
		UserSessionIssuerID: result.UserSessionIssuer.ID,
		ToolsetID:           result.Toolset.ID,
		ClientID:            "test-mcp-client",
		RedirectURI:         "http://example.com/cb",
		CodeChallenge:       "",
		CodeChallengeMethod: "",
		Subject:             &userSubject,
		CreatedAt:           time.Now(),
	}))

	runRemoteLoginRoundTrip(t, ctx, ti, mgr, result, parentID, &userSubject)
}

// TestRemoteLoginCallback_AnonymousSubject covers the public-toolset
// path. The parent AuthnChallengeState is created with no subject; the
// /mcp/{slug}/connect GET handler late-binds an anonymous URN and
// persists it. The remote-login callback then attributes the
// remote_sessions row to that anonymous URN.
func TestRemoteLoginCallback_AnonymousSubject(t *testing.T) {
	t.Parallel()

	idp := devidptest.Launch(t, devidptest.LaunchOpts{})
	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateIssuerGatedToolset(t, ctx, ti.conn, ti.enc, authCtx, oauthtest.IssuerGatedToolsetOpts{
		Slug:                         "issuer-anon",
		IsPublic:                     true,
		UpstreamMetadata:             idp.OAuth21Metadata(t),
		RemoteSessionCallbackBaseURL: ti.serverURL.String(),
	})

	mgr, authnCache := buildChallengeManagerForTest(t, ti)

	parentID := uuid.NewString()
	require.NoError(t, authnCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  parentID,
		UserSessionIssuerID: result.UserSessionIssuer.ID,
		ToolsetID:           result.Toolset.ID,
		ClientID:            "test-mcp-client",
		RedirectURI:         "http://example.com/cb",
		CodeChallenge:       "",
		CodeChallengeMethod: "",
		Subject:             nil,
		CreatedAt:           time.Now(),
	}))

	insertUserSessionClient(t, ctx, ti.conn, result.UserSessionIssuer.ID, "test-mcp-client")

	// HandleConsent (GET) must stamp an anonymous URN onto the parent
	// AuthnChallengeState. Hitting it via the service exercises the real
	// path (loadToolset → requireUserSessionIssuer → late-bind branch).
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+result.Toolset.McpSlug.String+"/connect?state="+parentID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", result.Toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(w, req))
	require.Equal(t, http.StatusOK, w.Code)

	stamped, err := authnCache.Get(ctx, "authnChallenge:"+parentID)
	require.NoError(t, err)
	require.NotNil(t, stamped.Subject, "consent GET must stamp anonymous subject on public toolset")
	require.Equal(t, urn.SessionSubjectKindAnonymous, stamped.Subject.Kind)

	runRemoteLoginRoundTrip(t, ctx, ti, mgr, result, parentID, stamped.Subject)
}

func TestRemoteLoginChallenge_CustomDomainRegistersGramCallback(t *testing.T) {
	t.Parallel()

	idp := devidptest.Launch(t, devidptest.LaunchOpts{})
	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateIssuerGatedToolset(t, ctx, ti.conn, ti.enc, authCtx, oauthtest.IssuerGatedToolsetOpts{
		Slug:                         "issuer-custom-domain",
		IsPublic:                     false,
		UpstreamMetadata:             idp.OAuth21Metadata(t),
		RemoteSessionCallbackBaseURL: ti.serverURL.String(),
	})
	result.Toolset, _ = attachCustomDomainToToolset(t, ctx, ti, authCtx, result.Toolset, "remote-login-custom.example.com")

	mgr, _ := buildChallengeManagerForTest(t, ti)
	clients, err := mgr.ListClients(ctx, result.Toolset.ProjectID, result.UserSessionIssuer.ID)
	require.NoError(t, err)
	require.Len(t, clients, 1)

	userSubject := urn.NewUserSubject(uuid.NewString())
	authURL, err := mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID:                  uuid.NewString(),
		ProjectID:           result.Toolset.ProjectID,
		UserSessionIssuerID: result.UserSessionIssuer.ID,
		Subject:             &userSubject,
		McpSlug:             result.Toolset.McpSlug.String,
	}, clients[0])
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	gramCallback := ti.serverURL.String() + "/mcp/" + result.Toolset.McpSlug.String + "/remote_login_callback"
	customCallback := "https://remote-login-custom.example.com/mcp/" + result.Toolset.McpSlug.String + "/remote_login_callback"
	require.Equal(t, gramCallback, parsed.Query().Get("redirect_uri"))

	upstreamResp := httpGetNoFollow(t, authURL)
	defer func() { _ = upstreamResp.Body.Close() }()
	require.Equal(t, http.StatusFound, upstreamResp.StatusCode, "registered Gram callback should be accepted by dev-idp")

	badURL := *parsed
	badQuery := badURL.Query()
	badQuery.Set("redirect_uri", customCallback)
	badURL.RawQuery = badQuery.Encode()
	badResp := httpGetNoFollow(t, badURL.String())
	defer func() { _ = badResp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, badResp.StatusCode, "unregistered custom-domain callback should be rejected by dev-idp")
}

// runRemoteLoginRoundTrip drives BuildAuthorizationUrl → upstream
// /authorize (no-follow) → HandleRemoteLoginCallback against a freshly
// stamped parent challenge, then asserts the resulting redirect and the
// remote_sessions row's subject_urn. Shared by both subject-shape tests.
func runRemoteLoginRoundTrip(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	mgr *remotesessions.ChallengeManager,
	result oauthtest.IssuerGatedToolsetResult,
	parentChallengeID string,
	expectedSubject *urn.SessionSubject,
) {
	t.Helper()

	clients, err := mgr.ListClients(ctx, result.Toolset.ProjectID, result.UserSessionIssuer.ID)
	require.NoError(t, err)
	require.Len(t, clients, 1)

	parent := remotesessions.ParentChallenge{
		ID:                  parentChallengeID,
		ProjectID:           result.Toolset.ProjectID,
		UserSessionIssuerID: result.UserSessionIssuer.ID,
		Subject:             expectedSubject,
		McpSlug:             result.Toolset.McpSlug.String,
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
	require.NotEmpty(t, code, "upstream redirect must carry ?code")
	require.NotEmpty(t, state, "upstream redirect must carry ?state")

	cbReq := httptest.NewRequest(http.MethodGet,
		"/mcp/"+result.Toolset.McpSlug.String+"/remote_login_callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), nil)
	cbCtx := chi.NewRouteContext()
	cbCtx.URLParams.Add("mcpSlug", result.Toolset.McpSlug.String)
	cbReq = cbReq.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, cbCtx))

	cbW := httptest.NewRecorder()
	require.NoError(t, mgr.HandleRemoteLoginCallback(cbW, cbReq))
	require.Equal(t, http.StatusSeeOther, cbW.Code, "callback should redirect back to /connect")
	require.Contains(t, cbW.Header().Get("Location"), "/mcp/"+result.Toolset.McpSlug.String+"/connect")
	require.Contains(t, cbW.Header().Get("Location"), parentChallengeID)

	sessions, err := remotesessions_repo.New(ti.conn).ListRemoteSessionsByProjectID(ctx, remotesessions_repo.ListRemoteSessionsByProjectIDParams{
		ProjectID:  result.Toolset.ProjectID,
		LimitValue: 10,
	})
	require.NoError(t, err)
	require.Len(t, sessions, 1, "exactly one remote_sessions row should exist")
	require.Equal(t, expectedSubject.String(), sessions[0].SubjectUrn.String())
	require.Equal(t, result.RemoteSessionClient.ID, sessions[0].RemoteSessionClientID)
	require.True(t, sessions[0].AccessExpiresAt.Valid)
	require.True(t, sessions[0].AccessExpiresAt.Time.After(time.Now()), "access_expires_at must be in the future")
}

// insertUserSessionClient creates a user_session_clients row so
// HandleConsent's lookup by ClientID succeeds. The real flow would mint
// this in HandleRegister; tests skip that hop.
func insertUserSessionClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, issuerID uuid.UUID, clientID string) {
	t.Helper()
	_, err := usersessions_repo.New(conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:   issuerID,
		ClientID:              clientID,
		ClientSecretHash:      pgtype.Text{Valid: false},
		ClientName:            "test-mcp-client",
		RedirectUris:          []string{"http://example.com/cb"},
		ClientSecretExpiresAt: pgtype.Timestamptz{Valid: false},
	})
	require.NoError(t, err)
}

func attachCustomDomainToToolset(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	authCtx *contextvalues.AuthContext,
	toolset toolsets_repo.Toolset,
	domainName string,
) (toolsets_repo.Toolset, customdomains_repo.CustomDomain) {
	t.Helper()

	domainsRepo := customdomains_repo.New(ti.conn)
	domain, err := domainsRepo.CreateCustomDomain(ctx, customdomains_repo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domainName,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	domain, err = domainsRepo.UpdateCustomDomain(ctx, customdomains_repo.UpdateCustomDomainParams{
		ID:             domain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	toolset, err = toolsets_repo.New(ti.conn).UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            toolset.McpIsPublic,
		CustomDomainID:         uuid.NullUUID{UUID: domain.ID, Valid: true},
		McpEnabled:             toolset.McpEnabled,
		ToolSelectionMode:      toolset.ToolSelectionMode,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	return toolset, domain
}

// buildChallengeManagerForTest constructs a ChallengeManager wired to the
// same Redis + DB as the service under test, so RemoteLoginStates written
// by one are readable by the other. Also returns a TypedCacheObject for
// AuthnChallengeState keyed identically to the service's internal cache.
func buildChallengeManagerForTest(
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
