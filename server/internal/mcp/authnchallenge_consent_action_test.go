package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type consentActionFixture struct {
	ti        *testInstance
	endpoint  *mcp.ResolvedMcpEndpoint
	stateID   string
	projectID uuid.UUID
	orgID     string
	shared    uuid.UUID
	subject   urn.SessionSubject
	// clientA/clientB derive distinct resources; clientC derives none;
	// clientD sees both upstreams so derivation is ambiguous.
	clientA uuid.UUID
	clientB uuid.UUID
	clientC uuid.UUID
	clientD uuid.UUID
}

const (
	consentUpstreamA = "https://upstream-a.example.com"
	consentUpstreamB = "https://upstream-b.example.com"
)

// createConsentRemoteClient mints a remote_session_issuer plus a client
// attached to every given user_session_issuer. asBase "" uses fake AS
// endpoints (BuildAuthorizationUrl never contacts them); pass an httptest
// base URL to run a real code exchange against it.
func createConsentRemoteClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID, slug, asBase string, userSessionIssuerIDs []uuid.UUID) uuid.UUID {
	t.Helper()
	if asBase == "" {
		asBase = "https://" + slug + "-as.example.com"
	}
	q := remotesessions_repo.New(conn)
	rsi, err := q.CreateRemoteSessionIssuer(ctx, remotesessions_repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: projectID, Valid: true},
		Slug:                              slug + "-rsi",
		Issuer:                            asBase,
		AuthorizationEndpoint:             conv.ToPGText(asBase + "/authorize"),
		TokenEndpoint:                     conv.ToPGText(asBase + "/token"),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	})
	require.NoError(t, err)

	rsc, err := q.CreateRemoteSessionClient(ctx, remotesessions_repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGTextEmpty(organizationID),
		RemoteSessionIssuerID: rsi.ID,
		ClientID:              slug + "-external-client",
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	for _, usi := range userSessionIssuerIDs {
		require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessions_repo.AttachRemoteSessionClientToUserSessionIssuerParams{
			RemoteSessionClientID: rsc.ID,
			UserSessionIssuerID:   usi,
		}))
	}
	return rsc.ID
}

// attachConsentRemoteMcpServer binds a remote-backed mcp_server to issuerID
// so clients on that issuer derive serverURL as their resource.
func attachConsentRemoteMcpServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, issuerID uuid.UUID, slug, serverURL string) {
	t.Helper()
	remoteServer, err := remotemcp_repo.New(conn).CreateServer(ctx, remotemcp_repo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		TransportType: "sse",
		Url:           serverURL,
	})
	require.NoError(t, err)
	_, err = mcpservers_repo.New(conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		RemoteMcpServerID:   conv.ToNullUUID(remoteServer.ID),
		Visibility:          "private",
		UserSessionIssuerID: conv.ToNullUUID(issuerID),
	})
	require.NoError(t, err)
}

// mintConsentEndpointState builds the resolved endpoint (with an endpoint-
// level UpstreamResource poison no client matches) and stores a consent
// challenge state for a fresh subject.
func mintConsentEndpointState(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, orgID string, shared uuid.UUID, slug string) (*mcp.ResolvedMcpEndpoint, string, urn.SessionSubject) {
	t.Helper()

	endpoint := &mcp.ResolvedMcpEndpoint{
		AudienceURN:          urn.NewUserSessionIssuer(shared).String(),
		CIMDAdmissionModeRaw: pgtype.Text{String: "", Valid: false},
		CustomDomainID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		IsPublic:             false,
		McpServerID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:       orgID,
		ProjectID:            projectID,
		RouteBase:            "mcp",
		Slug:                 slug,
		ToolsetID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		// Deliberately matches no client: the action must never thread it in.
		UpstreamResource:    "https://endpoint-level.example.com",
		UserSessionIssuerID: shared,
	}

	subject := urn.NewUserSubject(uuid.NewString())
	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: shared,
		Endpoint: mcp.EndpointRef{
			McpSlug:        endpoint.Slug,
			RouteBase:      endpoint.RouteBase,
			CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		},
		ClientID:            "age3328-mcp-client",
		RedirectURI:         "http://example.com/cb",
		CodeChallenge:       "",
		CodeChallengeMethod: "",
		CSRFToken:           "csrf-token",
		Subject:             &subject,
		CreatedAt:           time.Now(),
	}))
	return endpoint, stateID, subject
}

// seedMultiClientConsentEndpoint: one shared user_session_issuer with four
// bound clients, two pinned to distinct upstreams via their own issuer link.
func seedMultiClientConsentEndpoint(t *testing.T) (context.Context, consentActionFixture) {
	t.Helper()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	issuerA := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	issuerB := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerA, "age3328-srv-a", consentUpstreamA+"/")
	attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerB, "age3328-srv-b", consentUpstreamB)

	clientA := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "age3328-a", "", []uuid.UUID{shared, issuerA})
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "age3328-b", "", []uuid.UUID{shared, issuerB})
	clientC := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "age3328-c", "", []uuid.UUID{shared})
	clientD := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "age3328-d", "", []uuid.UUID{shared, issuerA, issuerB})

	endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, shared, "age3328-consent")

	return ctx, consentActionFixture{
		ti:        ti,
		endpoint:  endpoint,
		stateID:   stateID,
		projectID: projectID,
		orgID:     orgID,
		shared:    shared,
		subject:   subject,
		clientA:   clientA,
		clientB:   clientB,
		clientC:   clientC,
		clientD:   clientD,
	}
}

// postConnectAction drives one client's connect action and returns the
// upstream authorize redirect URL.
func postConnectAction(t *testing.T, fx consentActionFixture, clientID uuid.UUID) *url.URL {
	t.Helper()
	return postConnectActionAs(t, nil, fx, clientID)
}

// postConnectActionAs is postConnectAction with the request context under test
// control, so a consent can be driven with a specific grant set. A nil context
// leaves the request on its own background context, which is what an
// unauthenticated browser POST carries.
func postConnectActionAs(t *testing.T, ctx context.Context, fx consentActionFixture, clientID uuid.UUID) *url.URL {
	t.Helper()

	form := url.Values{}
	form.Set("state", fx.stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", "connect")
	form.Set("client_id", clientID.String())

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsentAction(w, req, fx.endpoint))
	require.Equal(t, http.StatusSeeOther, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	return loc
}

// mintedRemoteLoginState reads back the stored RemoteLoginState — its
// Resource is what the code exchange records onto the grant.
func mintedRemoteLoginState(t *testing.T, ctx context.Context, fx consentActionFixture, upstreamState string) remotesessions.RemoteLoginState {
	t.Helper()
	loginCache := cache.NewTypedObjectCache[remotesessions.RemoteLoginState](fx.ti.logger, fx.ti.cacheAdapter, cache.SuffixNone)
	state, err := loginCache.Get(ctx, "remoteLogin:"+upstreamState)
	require.NoError(t, err)
	return state
}

func TestServeConsentAction_ConnectSendsPerClientResource(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMultiClientConsentEndpoint(t)

	locA := postConnectAction(t, fx, fx.clientA)
	require.Equal(t, "age3328-a-as.example.com", locA.Host)
	require.Equal(t, consentUpstreamA, locA.Query().Get("resource"))
	stateA := mintedRemoteLoginState(t, ctx, fx, locA.Query().Get("state"))
	require.Equal(t, consentUpstreamA, stateA.Resource)
	require.Equal(t, fx.clientA, stateA.RemoteSessionClientID)

	locB := postConnectAction(t, fx, fx.clientB)
	require.Equal(t, "age3328-b-as.example.com", locB.Host)
	require.Equal(t, consentUpstreamB, locB.Query().Get("resource"))
	stateB := mintedRemoteLoginState(t, ctx, fx, locB.Query().Get("state"))
	require.Equal(t, consentUpstreamB, stateB.Resource)
	require.Equal(t, fx.clientB, stateB.RemoteSessionClientID)
}

func TestServeConsentAction_ConnectWithoutDerivableResourceSendsNone(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMultiClientConsentEndpoint(t)

	loc := postConnectAction(t, fx, fx.clientC)
	require.Equal(t, "age3328-c-as.example.com", loc.Host)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a client with no derivable resource must send none — not the endpoint-level one")
	state := mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state"))
	require.Empty(t, state.Resource)
}

// A client whose attached upstreams disagree on URL derives "" — no resource
// is sent upstream and none is recorded on the login state.
func TestServeConsentAction_ConnectAmbiguousUpstreamsSendsNoResource(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMultiClientConsentEndpoint(t)

	loc := postConnectAction(t, fx, fx.clientD)
	require.Equal(t, "age3328-d-as.example.com", loc.Host)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "distinct upstream URLs make derivation ambiguous — send no resource")
	state := mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state"))
	require.Empty(t, state.Resource)
}

// A derivation failure must fail the connect closed: error out before any
// upstream redirect or login state exists.
func TestServeConsentAction_ConnectDerivationErrorFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMultiClientConsentEndpoint(t)

	// Break only the derivation query's table; nothing earlier in the connect
	// arm touches mcp_servers (per-test cloned DB, safe to mutate).
	_, err := fx.ti.conn.Exec(ctx, "ALTER TABLE mcp_servers RENAME TO mcp_servers_unavailable") //nolint:glint // notestingrawsql: deliberate DDL breakage to force a derivation DB error; not expressible as an SQLc query
	require.NoError(t, err)

	form := url.Values{}
	form.Set("state", fx.stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", "connect")
	form.Set("client_id", fx.clientA.String())
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	err = fx.ti.service.ServeConsentAction(w, req, fx.endpoint)
	require.Error(t, err)
	require.ErrorContains(t, err, "derive client upstream resource")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.Empty(t, w.Header().Get("Location"), "fail closed: no upstream redirect may be minted")
}

// consentExchangeCapture records the resource param of a token POST.
type consentExchangeCapture struct {
	HasResource bool
	Resource    string
}

// newConsentExchangeAS is a live fake authorization server: its /token
// endpoint captures the RFC 8707 resource and returns a token pair.
func newConsentExchangeAS(t *testing.T, captured *atomic.Value, accessToken string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/token" || r.ParseForm() != nil || r.PostForm.Get("grant_type") != "authorization_code" {
			http.NotFound(w, r)
			return
		}
		_, has := r.PostForm["resource"]
		captured.Store(consentExchangeCapture{HasResource: has, Resource: r.PostForm.Get("resource")})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + accessToken + `","token_type":"Bearer","expires_in":3600,"refresh_token":"rt-` + accessToken + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newConsentCallbackManager builds a ChallengeManager over the mcp test
// instance's own db/enc/cache so it can finish flows the service started.
func newConsentCallbackManager(t *testing.T, ti *testInstance) *remotesessions.ChallengeManager {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	return remotesessions.NewChallengeManager(ti.logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, ti.enc, policy, ti.cacheAdapter, ti.serverURL)
}

// completeRemoteLogin drives the code-exchange callback for one authorize
// redirect, as the upstream AS would after the user approves.
func completeRemoteLogin(t *testing.T, mgr *remotesessions.ChallengeManager, loc *url.URL) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?state="+url.QueryEscape(loc.Query().Get("state"))+"&code=fake-code", nil)
	w := httptest.NewRecorder()
	require.NoError(t, mgr.HandleRemoteLoginCallback(w, req))
	require.Equal(t, http.StatusSeeOther, w.Code)
}

// The AGE-3328 routing-poison regression: two clients with distinct upstreams
// connected through ONE endpoint must each exchange with, persist, and serve
// their own resource — never a shared or endpoint-level one.
func TestServeConsentAction_MultiBindingExchangePersistsPerClientResource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	issuerA := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	issuerB := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerA, "age3328-x-srv-a", consentUpstreamA)
	attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerB, "age3328-x-srv-b", consentUpstreamB)

	var postedA, postedB atomic.Value
	asA := newConsentExchangeAS(t, &postedA, "exchanged-a")
	asB := newConsentExchangeAS(t, &postedB, "exchanged-b")
	clientA := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "age3328-xa", asA.URL, []uuid.UUID{shared, issuerA})
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "age3328-xb", asB.URL, []uuid.UUID{shared, issuerB})

	endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, shared, "age3328-exchange")
	fx := consentActionFixture{
		ti:        ti,
		endpoint:  endpoint,
		stateID:   stateID,
		projectID: projectID,
		orgID:     orgID,
		shared:    shared,
		subject:   subject,
		clientA:   clientA,
		clientB:   clientB,
		clientC:   uuid.Nil,
		clientD:   uuid.Nil,
	}

	mgr := newConsentCallbackManager(t, ti)
	completeRemoteLogin(t, mgr, postConnectAction(t, fx, clientA))
	completeRemoteLogin(t, mgr, postConnectAction(t, fx, clientB))

	// Each code exchange carried its own client's resource.
	require.Equal(t, consentExchangeCapture{HasResource: true, Resource: consentUpstreamA}, postedA.Load())
	require.Equal(t, consentExchangeCapture{HasResource: true, Resource: consentUpstreamB}, postedB.Load())

	// Each credential row persisted its own resource.
	q := remotesessions_repo.New(ti.conn)
	sessA, err := q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: clientA})
	require.NoError(t, err)
	require.Equal(t, consentUpstreamA, sessA.Resource.String)
	sessB, err := q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: clientB})
	require.NoError(t, err)
	require.Equal(t, consentUpstreamB, sessB.Resource.String)

	// The routing layer serves both, each token qualified by its own resource.
	tokens, err := mgr.ResolveAccessTokens(ctx, projectID, orgID, shared, subject)
	require.NoError(t, err)
	require.Len(t, tokens, 2)
	byClient := map[uuid.UUID]remotesessions.UpstreamToken{}
	for _, tok := range tokens {
		byClient[tok.RemoteSessionClientID] = tok
	}
	require.Equal(t, "exchanged-a", byClient[clientA].Token)
	require.Equal(t, consentUpstreamA, byClient[clientA].Resource)
	require.Equal(t, "exchanged-b", byClient[clientB].Token)
	require.Equal(t, consentUpstreamB, byClient[clientB].Resource)
}
