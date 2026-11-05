package mcp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type connectionRecoveryFixture struct {
	userAgent string
	ti        *testInstance
	//nolint:containedctx // the test's authenticated context, used by every request it makes
	ctx      context.Context
	link     mcp.ConnectionRecoveryLink
	router   chi.Router
	asBase   string
	upstream string
	userID   string
	otherID  string
	fixtures *authclientfixtures.Queries
}

// newConnectionRecoveryFixture links a personal auth client to a server; its
// registration's callback lands on the dashboard's own host.
func newConnectionRecoveryFixture(t *testing.T) *connectionRecoveryFixture {
	t.Helper()
	return newConnectionRecoveryFixtureWithCallback(t, "", "", nil)
}

// newConnectionRecoveryFixtureWithCallback records callbackBase, when set, as
// the registration's callback origin, and wraps the service's cache with wrap, when set.
func newConnectionRecoveryFixtureWithCallback(t *testing.T, callbackBase, asBase string, wrap func(cache.Cache) cache.Cache) *connectionRecoveryFixture {
	t.Helper()
	ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{}, wrap)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := &connectionRecoveryFixture{ti: ti, ctx: ctx, router: chi.NewRouter(), userID: "user_" + uuid.NewString()[:8], otherID: "user_" + uuid.NewString()[:8]}
	q := testrepo.New(ti.conn)
	for _, id := range []string{f.userID, f.otherID} {
		require.NoError(t, q.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: id, Email: id + "@example.com", DisplayName: id}))
		require.NoError(t, q.CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: authCtx.ActiveOrganizationID, UserID: pgtype.Text{String: id, Valid: true}}))
	}
	slug := "recovery-" + uuid.NewString()[:6]
	f.asBase = conv.Default(asBase, "https://"+slug+"-as.example.com")
	registration := createConsentRemoteClientWithCallback(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, f.asBase, nil, callbackBase)
	f.fixtures = authclientfixtures.New(ti.conn)
	remoteID, serverID := uuid.New(), uuid.New()
	f.upstream = "https://mcp.example.com/" + remoteID.String()[:8]
	require.NoError(t, f.fixtures.SeedRemoteMCPServerFixture(ctx, authclientfixtures.SeedRemoteMCPServerFixtureParams{ID: remoteID, ProjectID: *authCtx.ProjectID, Url: f.upstream}))
	require.NoError(t, f.fixtures.SeedMCPServerFixture(ctx, authclientfixtures.SeedMCPServerFixtureParams{ID: serverID, ProjectID: *authCtx.ProjectID, Slug: pgtype.Text{String: "s-" + serverID.String()[:8], Valid: true}, RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true}}))
	authClientID, err := f.fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, ClientType: "remote_session", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, f.fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID, McpServerID: serverID, AuthClientID: authClientID, Source: "admin"}))
	f.link = mcp.ConnectionRecoveryLink{
		OrganizationID: authCtx.ActiveOrganizationID, UserID: f.userID, ProjectID: *authCtx.ProjectID,
		McpServerID: serverID, AuthClientID: authClientID, RegistrationID: registration,
	}
	ti.service.SetBrowserSession(func(ctx context.Context, token string) (context.Context, error) {
		if token == "" || strings.HasPrefix(token, "invalid") {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{ActiveOrganizationID: "", UserID: token}), nil
	})
	ti.service.SetAuthClientPermitted(true)
	f.router.Get(mcp.ConnectionRecoveryRoute, oops.BrowserErrHandle(ti.logger, ti.service.HandleConnectionRecovery).ServeHTTP)
	f.router.Get(mcp.RemoteLoginBindPath, oops.BrowserErrHandle(ti.logger, ti.service.HandleRemoteLoginBind).ServeHTTP)
	return f
}

// open requests path signed in as userID, or signed out when it is empty.
func (f *connectionRecoveryFixture) open(t *testing.T, path, userID string) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, path, userID, nil)
}

// request requests path as a browser signed in as userID, or signed out when
// it is empty, holding cookies.
func (f *connectionRecoveryFixture) request(t *testing.T, path, userID string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(f.ctx, http.MethodGet, path, nil)
	if f.userAgent != "" {
		req.Header.Set("User-Agent", f.userAgent)
	}
	if userID != "" {
		req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: userID})
	}
	for _, cookie := range cookies {
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// start opens a link signed in as userID and follows any browser hop, as that
// browser, to the upstream authorization URL. It returns that URL and the
// cookies the browser was given on the way.
func (f *connectionRecoveryFixture) start(t *testing.T, path, userID string) (*url.URL, []*http.Cookie) {
	t.Helper()
	res := f.open(t, path, userID)
	var cookies []*http.Cookie
	for range 4 {
		require.Equal(t, http.StatusFound, res.Code, res.Body.String())
		cookies = append(cookies, res.Result().Cookies()...)
		location, err := url.Parse(res.Header().Get("Location"))
		require.NoError(t, err)
		if location.Path != mcp.RemoteLoginBindPath {
			return location, cookies
		}
		res = f.request(t, location.RequestURI(), userID, cookies)
	}
	require.FailNow(t, "the link's login took too many browser hops")
	return nil, nil
}

// callback is a remote login callback from a browser signed in as userID, or
// signed out when it is empty, holding cookies.
func (f *connectionRecoveryFixture) callback(userID string, cookies []*http.Cookie) *http.Request {
	req := httptest.NewRequestWithContext(f.ctx, http.MethodGet, "/mcp/remote_login_callback", nil)
	if userID != "" {
		req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: userID})
	}
	for _, cookie := range cookies {
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	return req
}

// started issues a link and starts its login as the link's person.
func (f *connectionRecoveryFixture) started(t *testing.T) (remotesessions.RemoteLoginState, []*http.Cookie) {
	t.Helper()
	path, err := f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
	require.NoError(t, err)
	location, cookies := f.start(t, path, f.userID)
	remote, err := f.ti.service.RemoteLoginState(f.ctx, location.Query().Get("state"))
	require.NoError(t, err)
	return remote, cookies
}

func TestConnectionRecoveryStartsTheNamedPersonsLoginOnce(t *testing.T) {
	t.Parallel()
	f := newConnectionRecoveryFixture(t)
	path, err := f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
	require.NoError(t, err)

	signedOut := f.open(t, path, "")
	require.Equal(t, http.StatusFound, signedOut.Code)
	require.Contains(t, signedOut.Header().Get("Location"), "/login?redirect="+url.QueryEscape(path))

	other := f.open(t, path, f.otherID)
	require.Equal(t, http.StatusForbidden, other.Code)
	require.Contains(t, other.Body.String(), "for someone else")
	require.NotContains(t, other.Body.String(), f.asBase)

	location, cookies := f.start(t, path, f.userID)
	require.Equal(t, f.asBase+"/authorize", location.Scheme+"://"+location.Host+location.Path)
	require.NotEmpty(t, cookies, "the starting browser is bound to the login")

	again := f.open(t, path, f.userID)
	require.Equal(t, http.StatusNotFound, again.Code, "a link starts one login")

	remote, err := f.ti.service.RemoteLoginState(f.ctx, location.Query().Get("state"))
	require.NoError(t, err)
	require.NotNil(t, remote.Subject)
	require.Equal(t, f.userID, remote.Subject.ID, "the login connects the named person's own account")
	require.True(t, strings.HasSuffix(remote.FinalRedirectURI, mcp.ConnectionRecoveryDonePath))

	recovery, err := f.ti.service.VerifyConnectionRecoveryCompletion(f.callback(f.otherID, cookies), remote)
	require.True(t, recovery)
	require.Error(t, err, "completing as someone else is refused")
	recovery, err = f.ti.service.VerifyConnectionRecoveryCompletion(f.callback("", nil), remote)
	require.True(t, recovery)
	require.Error(t, err, "a browser that did not start the login cannot complete it with its own account")
	recovery, err = f.ti.service.VerifyConnectionRecoveryCompletion(f.callback(f.userID, nil), remote)
	require.True(t, recovery)
	require.Error(t, err, "even the person's session does not stand in for the starting browser")
	recovery, err = f.ti.service.VerifyConnectionRecoveryCompletion(f.callback(f.userID, cookies), remote)
	require.True(t, recovery)
	require.NoError(t, err, "a refused callback does not spend the person's login")
	_, err = f.ti.service.VerifyConnectionRecoveryCompletion(f.callback(f.userID, cookies), remote)
	require.Error(t, err, "a login completes once")

	remote, cookies = f.started(t)
	recovery, err = f.ti.service.VerifyConnectionRecoveryCompletion(f.callback("", cookies), remote)
	require.True(t, recovery)
	require.NoError(t, err, "the starting browser completes on a host without the session, on the person's rechecked access")

	remote, cookies = f.started(t)
	f.ti.service.SetAuthClientPermitted(false)
	_, err = f.ti.service.VerifyConnectionRecoveryCompletion(f.callback(f.userID, cookies), remote)
	require.Error(t, err, "a person who lost access cannot complete")
}

// A registration whose callback lands on another host binds the starting
// browser there through the browser hop, whose confirm stop needs the
// person's own session.
func TestConnectionRecoveryBindsACallbackOnAnotherHost(t *testing.T) {
	t.Parallel()
	f := newConnectionRecoveryFixtureWithCallback(t, "https://callback.example.com", "", nil)

	remote, cookies := f.started(t)
	require.NotEmpty(t, cookies)
	recovery, err := f.ti.service.VerifyConnectionRecoveryCompletion(f.callback("", nil), remote)
	require.True(t, recovery)
	require.Error(t, err, "a browser the hop did not bind cannot complete")
	recovery, err = f.ti.service.VerifyConnectionRecoveryCompletion(f.callback("", cookies), remote)
	require.True(t, recovery)
	require.NoError(t, err, "the browser the hop bound completes on the callback host")

	// A copied bind link gives another browser the cookie, but its confirm
	// stop needs the person's session, so the authorization URL is never
	// revealed to it.
	path, err := f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
	require.NoError(t, err)
	started := f.open(t, path, f.userID)
	require.Equal(t, http.StatusFound, started.Code, started.Body.String())
	bind, err := url.Parse(started.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "callback.example.com", bind.Host)
	require.Equal(t, mcp.RemoteLoginBindPath, bind.Path)
	copied := f.request(t, bind.RequestURI(), "", nil)
	require.Equal(t, http.StatusFound, copied.Code, copied.Body.String())
	confirm, err := url.Parse(copied.Header().Get("Location"))
	require.NoError(t, err)
	refused := f.request(t, confirm.RequestURI(), f.otherID, copied.Result().Cookies())
	require.NotEqual(t, http.StatusFound, refused.Code)
	require.NotContains(t, refused.Header().Get("Location"), f.asBase)
	require.NotContains(t, refused.Body.String(), f.asBase)
}

// Where typed auth clients are enforced, a link's login stands on the offer it
// names and asks for the resource that offer decides. It completes and stores
// the connection whether or not a user session issuer links the registration,
// and it writes no link.
func TestConnectionRecoveryCompletesThroughTheOffer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		linked bool
	}{
		{name: "no sign-in link", linked: false},
		{name: "a sign-in link", linked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var posted atomic.Value
			as := newConsentExchangeAS(t, &posted, "recovered-token")
			f := newConnectionRecoveryFixtureWithCallback(t, "", as.URL, nil)
			signIn := createUserSessionIssuer(t, f.ctx, f.ti.conn, f.link.ProjectID)
			f.link.UserSessionIssuerID = signIn
			f.link.UpstreamResource = f.upstream
			q := remotesessions_repo.New(f.ti.conn)
			if tc.linked {
				require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(f.ctx, remotesessions_repo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: f.link.RegistrationID, UserSessionIssuerID: signIn}))
			}
			_, err := productfeaturesrepo.New(f.ti.conn).EnableFeature(f.ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: f.link.OrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
			require.NoError(t, err)
			seedPrincipalMCPConnectGrant(t, f.ctx, f.ti, f.link.OrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, f.userID), f.link.McpServerID)
			// A browser's request carries no prepared grants: the person's own decide.
			f.ctx = t.Context()

			path, err := f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
			require.NoError(t, err)
			location, cookies := f.start(t, path, f.userID)
			require.Equal(t, f.upstream, location.Query().Get("resource"), "the authorization request names the resource")
			remote, err := f.ti.service.RemoteLoginState(f.ctx, location.Query().Get("state"))
			require.NoError(t, err)
			subject := urn.NewUserSubject(f.userID)
			require.Equal(t, &remotesessions.TypedAuthorization{OrganizationID: f.link.OrganizationID, ProjectID: f.link.ProjectID, ServerID: f.link.McpServerID, AuthClientID: f.link.AuthClientID, RemoteSessionClientID: f.link.RegistrationID, Subject: subject}, remote.TypedAuthorization)

			req := f.callback("", cookies)
			req.URL.RawQuery = url.Values{"state": {location.Query().Get("state")}, "code": {"recovery-code"}}.Encode()
			w := httptest.NewRecorder()
			require.NoError(t, f.ti.service.HandleRemoteLoginCallback(w, req))
			require.Equal(t, http.StatusSeeOther, w.Code)
			require.True(t, strings.HasSuffix(w.Header().Get("Location"), mcp.ConnectionRecoveryDonePath))

			session, err := q.GetActiveRemoteSession(f.ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: f.link.RegistrationID})
			require.NoError(t, err, "the connection is stored")
			require.Equal(t, f.upstream, session.Resource.String)
			linked, err := q.CheckRemoteSessionClientBindingForUserSessionIssuer(f.ctx, remotesessions_repo.CheckRemoteSessionClientBindingForUserSessionIssuerParams{RemoteSessionClientID: f.link.RegistrationID, UserSessionIssuerID: signIn, ProjectID: f.link.ProjectID, OrganizationID: f.link.OrganizationID})
			require.NoError(t, err)
			require.Equal(t, tc.linked, linked, "the login writes no link of its own")
		})
	}
}

// A person the server would refuse at the callback learns it from the link,
// before signing in to the provider.
func TestConnectionRecoveryRefusesATypedLoginTheServerWouldDeny(t *testing.T) {
	t.Parallel()
	f := newConnectionRecoveryFixture(t)
	f.link.UserSessionIssuerID = createUserSessionIssuer(t, f.ctx, f.ti.conn, f.link.ProjectID)
	_, err := productfeaturesrepo.New(f.ti.conn).EnableFeature(f.ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: f.link.OrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	seedPrincipalGrant(t, f.ctx, f.ti, f.link.OrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, f.userID), authz.ScopeMCPAuth, f.link.AuthClientID.String())
	// A browser's request carries no prepared grants: the person's own decide.
	f.ctx = t.Context()

	path, err := f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
	require.NoError(t, err)
	refused := f.open(t, path, f.userID)
	require.Equal(t, http.StatusForbidden, refused.Code)
	require.Contains(t, refused.Body.String(), "No access")
	require.NotContains(t, refused.Body.String(), f.asBase)
}

// Without enforcement a link's login carries no typed authorization and no
// resource, and its callback still relies on the user session issuer link.
func TestConnectionRecoveryWithoutEnforcementStandsOnTheLink(t *testing.T) {
	t.Parallel()
	f := newConnectionRecoveryFixture(t)
	f.link.UpstreamResource = f.upstream

	path, err := f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
	require.NoError(t, err)
	location, _ := f.start(t, path, f.userID)
	require.False(t, location.Query().Has("resource"))
	remote, err := f.ti.service.RemoteLoginState(f.ctx, location.Query().Get("state"))
	require.NoError(t, err)
	require.Nil(t, remote.TypedAuthorization)
	require.Empty(t, remote.Resource)
}

// A link connects only through a client the server still offers on the same
// registration, both when its login starts and when it completes.
func TestConnectionRecoveryRefusesAClientTheServerNoLongerOffers(t *testing.T) {
	t.Parallel()
	f := newConnectionRecoveryFixture(t)

	remote, cookies := f.started(t)
	require.NoError(t, f.fixtures.SetAuthClientEnabledFixture(f.ctx, authclientfixtures.SetAuthClientEnabledFixtureParams{Enabled: false, ID: f.link.AuthClientID}))
	_, err := f.ti.service.VerifyConnectionRecoveryCompletion(f.callback(f.userID, cookies), remote)
	require.Error(t, err, "a disabled client stores no connection")
	path, err := f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
	require.NoError(t, err)
	disabled := f.open(t, path, f.userID)
	require.Equal(t, http.StatusConflict, disabled.Code)
	require.NotContains(t, disabled.Body.String(), f.asBase)

	require.NoError(t, f.fixtures.SetAuthClientEnabledFixture(f.ctx, authclientfixtures.SetAuthClientEnabledFixtureParams{Enabled: true, ID: f.link.AuthClientID}))
	remote, cookies = f.started(t)
	require.NoError(t, f.fixtures.SoftDeleteServerAuthClientLinksFixture(f.ctx, f.link.McpServerID))
	_, err = f.ti.service.VerifyConnectionRecoveryCompletion(f.callback(f.userID, cookies), remote)
	require.Error(t, err, "a client detached from the server stores no connection")
	path, err = f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
	require.NoError(t, err)
	detached := f.open(t, path, f.userID)
	require.Equal(t, http.StatusConflict, detached.Code)
	require.NotContains(t, detached.Body.String(), f.asBase)
}

func TestConnectionRecoveryShowsNoAccessWithoutAWayToConnect(t *testing.T) {
	t.Parallel()
	f := newConnectionRecoveryFixture(t)
	f.ti.service.SetAuthClientPermitted(false)
	path, err := f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
	require.NoError(t, err)

	denied := f.open(t, path, f.userID)
	require.Equal(t, http.StatusForbidden, denied.Code)
	require.Contains(t, denied.Body.String(), "No access")
	require.NotContains(t, denied.Body.String(), f.asBase)
}

type recoveryStoreFailureCache struct {
	codemode.OwnershipStore
	fail atomic.Bool
}

func (c *recoveryStoreFailureCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if strings.Contains(key, "connectionRecovery:") && c.fail.Load() {
		return errors.New("injected store failure")
	}
	return c.OwnershipStore.Set(ctx, key, value, ttl) //nolint:wrapcheck // test double passes the delegate's result through
}

// A link spent before its login could be stored reports why, not a missed branch.
func TestConnectionRecoveryStoreFailureRecordsAVerdict(t *testing.T) {
	t.Parallel()
	var failing *recoveryStoreFailureCache
	f := newConnectionRecoveryFixtureWithCallback(t, "", "", func(delegate cache.Cache) cache.Cache {
		store, ok := delegate.(codemode.OwnershipStore)
		require.True(t, ok)
		failing = &recoveryStoreFailureCache{OwnershipStore: store}
		return failing
	})
	path, err := f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
	require.NoError(t, err)
	rec := recordConnections(t, f.ti)
	failing.fail.Store(true)

	response := f.open(t, path, f.userID)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	rec.RequireEnded(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeInternalError)
	rec.RequireNoUnclassifiedExit()
}

func TestConnectionRecoveryRefusesUnknownLinks(t *testing.T) {
	t.Parallel()
	f := newConnectionRecoveryFixture(t)
	require.Equal(t, http.StatusNotFound, f.open(t, "/mcp/connection_recovery/unknown", f.userID).Code)

	var remote remotesessions.RemoteLoginState
	remote.FinalRedirectURI = "https://example.com" + mcp.ConnectionRecoveryDonePath
	remote.ParentChallengeID = uuid.NewString()
	recovery, err := f.ti.service.VerifyConnectionRecoveryCompletion(httptest.NewRequestWithContext(f.ctx, http.MethodGet, "/", nil), remote)
	require.True(t, recovery)
	require.Error(t, err, "a recovery login without its entry fails closed")
}

func TestPersonBackedTurnsUseTheirPersonsOwnConnection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	// The agent's owner is someone else, so only the authorizing user's
	// connection is the right answer.
	owner := "user_owner_" + uuid.NewString()[:8]
	require.NoError(t, testrepo.New(ti.conn).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: owner, Email: owner + "@example.com", DisplayName: owner}))
	require.NoError(t, testrepo.New(ti.conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: ac.ActiveOrganizationID, UserID: pgtype.Text{String: owner, Valid: true}}))
	agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: ac.ActiveOrganizationID, OwnerUserID: owner, ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, Name: "Connection subject agent " + uuid.NewString()[:8]})
	require.NoError(t, err)
	projectRead, err := authz.NewSelector(authz.ScopeProjectRead, ac.ProjectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), Scope: string(authz.ScopeProjectRead), Selectors: projectRead})
	require.NoError(t, err)
	agentSubject := urn.NewAgentSubject(agent.ID)
	require.Equal(t, agentSubject, mcp.ConnectionSubject(ctx, agentSubject), "a caller without a principal credential resolves its own")

	mint := func(principal urn.Principal, authorizer string) context.Context {
		t.Helper()
		token, _, err := ti.principalCredentials.Mint(principalcredential.Credential{
			OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID,
			Principal: principal, AuthorizerUserID: authorizer,
		})
		require.NoError(t, err)
		authed, err := ti.principalCredentials.Authenticate(ctx, token)
		require.NoError(t, err)
		return authed
	}
	personBacked := mint(urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), ac.UserID)
	endpoint := &mcp.ResolvedMcpEndpoint{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, Slug: "linear"}
	answer, ok := ti.service.ConnectionRecoveryErrorFor(personBacked, endpoint, agentSubject, uuid.New(), uuid.New(), authz.AuthClientTypeRemoteSession)
	require.True(t, ok)
	require.Contains(t, answer.Error(), "Connection required")
	require.Contains(t, answer.Error(), ti.siteURL.JoinPath("mcp", "connection_recovery").String()+"/", "links are issued on the canonical dashboard origin")
	_, ok = ti.service.ConnectionRecoveryErrorFor(personBacked, endpoint, agentSubject, uuid.New(), uuid.New(), authz.AuthClientTypeServiceAccount)
	require.False(t, ok, "a shared credential belongs to nobody, so no one connects it")
	_, ok = ti.service.ConnectionRecoveryErrorFor(ctx, endpoint, urn.NewUserSubject(ac.UserID), uuid.New(), uuid.New(), authz.AuthClientTypeRemoteSession)
	require.False(t, ok, "a person's own session reconnects through its client as before")

	require.Equal(t, urn.NewUserSubject(ac.UserID), mcp.ConnectionSubject(mint(urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), ac.UserID), agentSubject), "a turn acting for a person resolves that person's connection, not the agent owner's")
	workloadIssuer, workloadExternal := uuid.New(), "assistant-trigger:"+uuid.NewString()
	workload := urn.NewWorkloadPrincipal(workloadIssuer, workloadExternal)
	workloadSubject := urn.NewWorkloadSubject(workloadIssuer, workloadExternal)
	require.Equal(t, workloadSubject, mcp.ConnectionSubject(mint(workload, ""), workloadSubject), "a turn acting for nobody resolves no person's connection")

	// An agent API key is served by its exact attachment, else by its
	// validated authorizer's own connection, under the agent's own mcp:auth
	// (TestAgentKeyAuthClientUsesItsAuthorizersConnection).
	agentKey := contextvalues.WithPrincipalAPIKeyAuthorization(ctx, ac, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), contextvalues.PrincipalCredential{AuthorizerUserID: owner, DelegatedGrants: []byte("[]"), DelegatedGrantsVersion: 1, DerivesPolicyAtAdmission: false})
	require.Equal(t, agentSubject, mcp.ConnectionSubject(agentKey, agentSubject), "an agent key keeps its subject; its attachment, else its authorizer, owns the connection")
	agentSession := contextvalues.WithPrincipalCredentialAuthorization(ctx, ac, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), contextvalues.PrincipalCredential{AuthorizerUserID: owner, DelegatedGrants: []byte("[]"), DelegatedGrantsVersion: 1, DerivesPolicyAtAdmission: false})
	require.Equal(t, agentSubject, mcp.ConnectionSubject(agentSession, agentSubject), "an agent session keeps its subject; its attachment, else its authorizer, owns the connection")
}
