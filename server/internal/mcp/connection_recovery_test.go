package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type connectionRecoveryFixture struct {
	ti *testInstance
	//nolint:containedctx // the test's authenticated context, used by every request it makes
	ctx     context.Context
	link    mcp.ConnectionRecoveryLink
	router  chi.Router
	asBase  string
	userID  string
	otherID string
}

func newConnectionRecoveryFixture(t *testing.T) *connectionRecoveryFixture {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := &connectionRecoveryFixture{ti: ti, ctx: ctx, router: chi.NewRouter(), userID: "user_" + uuid.NewString()[:8], otherID: "user_" + uuid.NewString()[:8]}
	q := testrepo.New(ti.conn)
	for _, id := range []string{f.userID, f.otherID} {
		require.NoError(t, q.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: id, Email: id + "@example.com", DisplayName: id}))
		require.NoError(t, q.CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: authCtx.ActiveOrganizationID, UserID: pgtype.Text{String: id, Valid: true}}))
	}
	slug := "recovery-" + uuid.NewString()[:6]
	f.asBase = "https://" + slug + "-as.example.com"
	registration := createConsentRemoteClient(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, "", nil)
	f.link = mcp.ConnectionRecoveryLink{
		OrganizationID: authCtx.ActiveOrganizationID, UserID: f.userID, ProjectID: *authCtx.ProjectID,
		McpServerID: uuid.New(), AuthClientID: uuid.New(), RegistrationID: registration,
	}
	ti.service.SetBrowserSession(func(ctx context.Context, token string) (context.Context, error) {
		if token == "" || strings.HasPrefix(token, "invalid") {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{ActiveOrganizationID: "", UserID: token}), nil
	})
	ti.service.SetAuthClientPermitted(true)
	f.router.Get(mcp.ConnectionRecoveryRoute, oops.ErrHandle(ti.logger, ti.service.HandleConnectionRecovery).ServeHTTP)
	return f
}

// open requests path signed in as userID, or signed out when it is empty.
func (f *connectionRecoveryFixture) open(t *testing.T, path, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(f.ctx, http.MethodGet, path, nil)
	if userID != "" {
		req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: userID})
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
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

	started := f.open(t, path, f.userID)
	require.Equal(t, http.StatusFound, started.Code, started.Body.String())
	location, err := url.Parse(started.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, f.asBase+"/authorize", location.Scheme+"://"+location.Host+location.Path)

	again := f.open(t, path, f.userID)
	require.Equal(t, http.StatusNotFound, again.Code, "a link starts one login")

	remote, err := f.ti.service.RemoteLoginState(f.ctx, location.Query().Get("state"))
	require.NoError(t, err)
	require.NotNil(t, remote.Subject)
	require.Equal(t, f.userID, remote.Subject.ID, "the login connects the named person's own account")
	require.True(t, strings.HasSuffix(remote.FinalRedirectURI, mcp.ConnectionRecoveryDonePath))

	callback := func(userID string) *http.Request {
		req := httptest.NewRequestWithContext(f.ctx, http.MethodGet, "/mcp/remote_login_callback", nil)
		req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: userID})
		return req
	}
	recovery, err := f.ti.service.VerifyConnectionRecoveryCompletion(callback(f.otherID), remote)
	require.True(t, recovery)
	require.Error(t, err, "completing as someone else is refused")

	path, err = f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
	require.NoError(t, err)
	started = f.open(t, path, f.userID)
	location, err = url.Parse(started.Header().Get("Location"))
	require.NoError(t, err)
	remote, err = f.ti.service.RemoteLoginState(f.ctx, location.Query().Get("state"))
	require.NoError(t, err)
	recovery, err = f.ti.service.VerifyConnectionRecoveryCompletion(callback(f.userID), remote)
	require.True(t, recovery)
	require.NoError(t, err)
	_, err = f.ti.service.VerifyConnectionRecoveryCompletion(callback(f.userID), remote)
	require.Error(t, err, "a login completes once")
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
	agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: ac.ActiveOrganizationID, OwnerUserID: ac.UserID, ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, Name: "Connection subject agent " + uuid.NewString()[:8]})
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
	require.Contains(t, answer.Error(), "/mcp/connection_recovery/")
	_, ok = ti.service.ConnectionRecoveryErrorFor(personBacked, endpoint, agentSubject, uuid.New(), uuid.New(), authz.AuthClientTypeServiceAccount)
	require.False(t, ok, "a shared credential belongs to nobody, so no one connects it")
	_, ok = ti.service.ConnectionRecoveryErrorFor(ctx, endpoint, urn.NewUserSubject(ac.UserID), uuid.New(), uuid.New(), authz.AuthClientTypeRemoteSession)
	require.False(t, ok, "a person's own session reconnects through its client as before")

	require.Equal(t, urn.NewUserSubject(ac.UserID), mcp.ConnectionSubject(mint(urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), ac.UserID), agentSubject), "a turn acting for a person resolves that person's connection")
	workload := urn.NewWorkloadPrincipal(uuid.New(), "assistant-trigger:"+uuid.NewString())
	workloadSubject := urn.NewWorkloadSubject(uuid.New(), "assistant-trigger")
	require.Equal(t, workloadSubject, mcp.ConnectionSubject(mint(workload, ""), workloadSubject), "a turn acting for nobody resolves no person's connection")
}
