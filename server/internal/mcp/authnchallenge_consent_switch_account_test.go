package mcp_test

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/workos_errors"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/signins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	switchClientState   = "client-state-switch"
	switchCodeChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	switchWorkDomain    = "example.test"
	switchAgainCopy     = "That account is personal too, so Speakeasy can't sign you in to "
)

// recordingRevoker records the identity provider sessions it ends. Like
// WorkOS, ending one already ended answers not found.
type recordingRevoker struct {
	mu    sync.Mutex
	ended []string
	err   error
}

func (r *recordingRevoker) RevokeSession(_ context.Context, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	for _, ended := range r.ended {
		if ended == sessionID {
			return fmt.Errorf("revoke: %w", workos_errors.HTTPError{Code: http.StatusNotFound, Status: "404 Not Found", RequestID: "", Message: "session not found", ErrorCode: "", Errors: nil, FieldErrors: nil, ErrorDescription: ""})
		}
	}
	r.ended = append(r.ended, sessionID)
	return nil
}

func (r *recordingRevoker) sessions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ended...)
}

func (r *recordingRevoker) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

// challengeStoreFault fails the next store of a challenge other than keep, once armed.
type challengeStoreFault struct {
	cache.Cache
	mu    sync.Mutex
	armed bool
	keep  string
}

func (c *challengeStoreFault) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	c.mu.Lock()
	fail := c.armed && strings.HasPrefix(key, "authnChallenge:") && key != "authnChallenge:"+c.keep
	if fail {
		c.armed = false
	}
	c.mu.Unlock()
	if fail {
		return errors.New("cache unavailable")
	}
	if err := c.Cache.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("set: %w", err)
	}
	return nil
}

func (c *challengeStoreFault) arm(keep string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.armed, c.keep = true, keep
}

type switchFixture struct {
	consentActionFixture
	ctx      context.Context //nolint:containedctx // the fixture's test context, like the other consent fixtures
	mock     *mockIdentityResolver
	revoker  *recordingRevoker
	governor *consentGovernor
	// managed are the sign-ins the governor vouches for; any other is a personal account.
	managed map[uuid.UUID]bool
	// session is the WorkOS session behind the consent's first sign-in.
	session string
	// workosOrganization is the organization's WorkOS id.
	workosOrganization string
}

// seedSwitchConsent: a chained consent whose person signed in to Speakeasy with a personal Google account on the work domain.
func seedSwitchConsent(t *testing.T, tag string, wrap func(cache.Cache) cache.Cache) *switchFixture {
	t.Helper()

	mock := &mockIdentityResolver{hasAccessOK: true, buildAuthURLResult: &url.URL{Scheme: "https", Host: "idp.example.com", Path: "/authorize"}}
	ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), mock, mcp.TunnelPublicConfig{
		SessionTTL: 0, LiveSessionCap: 0, InitializeRate: ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0}, RequestRate: ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0}, MaxRequestLifetime: 0,
	}, wrap)
	projectID, orgID := consentTestTenant(t, ctx)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := tag + "-" + uuid.NewString()[:8]
	serverID := attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerID, slug, consentChainedUpstream)
	endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, issuerID, slug)
	endpoint.McpServerID = conv.ToNullUUID(serverID)
	endpoint.UpstreamResource = consentChainedUpstream
	fx := &switchFixture{
		consentActionFixture: consentActionFixture{ti: ti, endpoint: endpoint, stateID: stateID, projectID: projectID, orgID: orgID, shared: issuerID, subject: subject, clientA: uuid.Nil, clientB: uuid.Nil, clientC: uuid.Nil, clientD: uuid.Nil, server: ""},
		ctx:                  ctx,
		mock:                 mock,
		revoker:              nil,
		governor:             nil,
		managed:              map[uuid.UUID]bool{},
		session:              "",
		workosOrganization:   "",
	}
	registerPageClient(t, ctx, fx.consentActionFixture)
	fx.clientA = createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, tag, "", []uuid.UUID{issuerID})
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: conv.ToNullUUID(serverID), Slug: slug,
	})
	require.NoError(t, err)
	armSwitch(t, fx, clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, fx.clientA), consentChainedUpstream, func(s *mcp.AuthnChallengeState) {
		s.Endpoint.McpServerID = conv.ToNullUUID(serverID)
	})
	return fx
}

// armSwitch gives a consent fixture a personal sign-in, a governor that refuses it, and a recording session revoker.
func armSwitch(t *testing.T, fx *switchFixture, remoteIssuer uuid.UUID, upstream string, edit func(*mcp.AuthnChallengeState)) {
	t.Helper()
	ti := fx.ti
	fx.revoker = &recordingRevoker{mu: sync.Mutex{}, ended: nil, err: nil}
	fx.session = "session_" + uuid.NewString()
	fx.governor = setConsentGovernor(t, fx.consentActionFixture, remoteIssuer, upstream)
	fx.governor.acquire = func(context.Context, identitychaining.Request) identitychaining.Outcome {
		fx.governor.mu.Lock()
		checked := fx.governor.checkedSignIns
		vouched := len(checked) > 0 && fx.managed[checked[len(checked)-1]]
		fx.governor.mu.Unlock()
		if vouched {
			return identitychaining.Outcome{Stage: identitychaining.StageComplete, Reason: identitychaining.ReasonSuccess, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false, Provider: ""}
		}
		return identitychaining.Outcome{Stage: identitychaining.StageAuthorization, Reason: identitychaining.ReasonUnmanagedAccount, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false, Provider: ""}
	}
	ti.service.SetIdentityResolver(fx.mock)
	ti.service.SetIDPSessionRevoker(fx.revoker)
	signins.EnableForTests(ti.features, fx.orgID)
	setSwitchVerifiedDomains(t, fx, switchWorkDomain)
	fx.workosOrganization = workOSOrganizationID(t, fx.ctx, ti, fx.orgID)

	signInID := recordPersonalSignIn(t, fx, fx.subject.ID, fx.session)
	impersonated := false
	fx.store(t, func(state *mcp.AuthnChallengeState) {
		state.FlowID = uuid.NewString()
		state.CreatedAt = time.Now()
		state.State = switchClientState
		state.CodeChallenge = switchCodeChallenge
		state.CodeChallengeMethod = "S256"
		state.AuthorizerUserID = fx.subject.ID
		state.AuthorizerImpersonated = &impersonated
		state.SignInID = &signInID
		if edit != nil {
			edit(state)
		}
	})
}

func setSwitchVerifiedDomains(t *testing.T, fx *switchFixture, domains ...string) {
	t.Helper()
	org, err := orgrepo.New(fx.ti.conn).GetOrganizationMetadata(fx.ctx, fx.orgID)
	require.NoError(t, err)
	_, err = orgrepo.New(fx.ti.conn).UpdateOrganizationMetadataFromWorkOS(fx.ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		Name:              org.Name,
		WorkosID:          org.WorkosID,
		WorkosUpdatedAt:   pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
		WorkosLastEventID: conv.ToPGText("event_" + uuid.NewString()),
		VerifiedDomains:   domains,
		ID:                fx.orgID,
	})
	require.NoError(t, err)
}

// recordPersonalSignIn records a Google sign-in that answered with no Workspace domain.
func recordPersonalSignIn(t *testing.T, fx *switchFixture, userID, workosSessionID string) uuid.UUID {
	t.Helper()
	recorded, err := signins.Record(fx.ctx, fx.ti.conn, signins.RecordParams{
		OrganizationID:       fx.orgID,
		UserID:               userID,
		WorkOSSessionID:      workosSessionID,
		AuthenticationMethod: signins.MethodGoogleOAuth,
		Evidence:             signins.Evidence{HostedDomain: "", HostedDomainChecked: true, TenantID: "", VerifiedEmail: userID + "@" + switchWorkDomain, IDPAuthenticatedAt: time.Now()},
		AuthenticatedAt:      time.Now(),
	})
	require.NoError(t, err)
	require.True(t, recorded.ID.Valid)
	return recorded.ID.UUID
}

func (fx *switchFixture) state(t *testing.T) mcp.AuthnChallengeState {
	t.Helper()
	state, err := fx.ti.authnChallengeCache.Get(fx.ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	return state
}

func (fx *switchFixture) store(t *testing.T, edit func(*mcp.AuthnChallengeState)) {
	t.Helper()
	state := fx.state(t)
	edit(&state)
	require.NoError(t, fx.ti.authnChallengeCache.Store(fx.ctx, state))
}

func postSwitchAccount(t *testing.T, fx *switchFixture, edit func(url.Values)) (*httptest.ResponseRecorder, error) {
	t.Helper()
	form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"switch_account"}, "client_id": {fx.clientA.String()}}
	if fx.server != "" {
		form.Set("server", fx.server)
	}
	if edit != nil {
		edit(form)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	if err := fx.ti.service.ServeConsentAction(w, req, fx.endpoint); err != nil {
		return w, fmt.Errorf("serve consent action: %w", err)
	}
	return w, nil
}

// switchAccount posts the action and follows it to the identity provider, returning the new challenge.
func switchAccount(t *testing.T, fx *switchFixture) mcp.AuthnChallengeState {
	t.Helper()
	w, err := postSwitchAccount(t, fx, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)
	require.Equal(t, "https://idp.example.com/authorize", w.Header().Get("Location"), "only ever to the sign-in page")
	require.Equal(t, "login", fx.mock.buildAuthURLParams.Prompt, "the old sign-in can't be reused silently")
	restarted, err := fx.ti.authnChallengeCache.Get(fx.ctx, "authnChallenge:"+fx.mock.buildAuthURLParams.State)
	require.NoError(t, err)
	return restarted
}

// signInAs completes the identity provider callback for a restarted challenge and returns the response.
func signInAs(t *testing.T, fx *switchFixture, stateID, userID, workosSessionID, method string, evidence identity.SignInEvidence) *httptest.ResponseRecorder {
	t.Helper()
	fx.mock.upsertResult = userID
	fx.mock.exchangeResult = &identity.IDPUserInfo{
		Sub:                  "workos-" + userID,
		Email:                userID + "@" + switchWorkDomain,
		Name:                 "Test User",
		OrganizationID:       fx.workosOrganization,
		WorkOSSessionID:      workosSessionID,
		AuthenticationMethod: method,
		Evidence:             evidence,
	}
	q := url.Values{"state": {stateID}, "code": {"idp-auth-code-" + uuid.NewString()}}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+fx.endpoint.Slug+"/idp_callback?"+q.Encode(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", fx.endpoint.Slug)
	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.HandleIDPCallback(w, req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))))
	require.Equal(t, http.StatusFound, w.Code)
	return w
}

// signInTo completes the callback and moves the fixture to the consent page it lands on.
func signInTo(t *testing.T, fx *switchFixture, stateID, userID, workosSessionID, method string, evidence identity.SignInEvidence) mcp.AuthnChallengeState {
	t.Helper()
	w := signInAs(t, fx, stateID, userID, workosSessionID, method, evidence)
	consentURL, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	fx.stateID = consentURL.Query().Get("state")
	return fx.state(t)
}

func personalEvidence(userID string) identity.SignInEvidence {
	return identity.SignInEvidence{HostedDomain: "", HostedDomainChecked: true, TenantID: "", VerifiedEmail: userID + "@" + switchWorkDomain, AuthenticatedAt: time.Now()}
}

func requireSwitchRefused(t *testing.T, err error, code oops.Code, message string) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
	if message != "" {
		require.Equal(t, message, shareable.Error())
	}
}

// refusedSwitchPage posts the switch through the browser error handler and returns the page a refusal shows.
func refusedSwitchPage(t *testing.T, fx *switchFixture) string {
	t.Helper()
	form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"switch_account"}, "client_id": {fx.clientA.String()}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	oops.BrowserErrHandle(fx.ti.logger, func(w http.ResponseWriter, r *http.Request) error {
		return fx.ti.service.ServeConsentAction(w, r, fx.endpoint)
	}).ServeHTTP(w, req)
	require.Empty(t, w.Header().Get("Location"))
	return strings.Join(strings.Fields(html.UnescapeString(w.Body.String())), " ")
}

func requireRefusedSwitchPage(t *testing.T, fx *switchFixture, sentence string) {
	t.Helper()
	page := refusedSwitchPage(t, fx)
	require.Contains(t, page, "Couldn't start a new sign-in")
	require.Contains(t, page, `<p class="text-sm text-foreground" data-what-happened> `+sentence+` </p>`, "the reason is the page's one sentence")
	require.Contains(t, page, "Routability Client", "the page names the client the consent page showed")
	require.NotContains(t, page, "Your MCP client")
	require.NotContains(t, page, "close this tab")
}

func checkCard(t *testing.T, fx *switchFixture) map[string]string {
	t.Helper()
	w, err := postChainCheck(t, fx.consentActionFixture, nil)
	require.NoError(t, err)
	return chainCheckResult(t, w)
}

// The check offers the switch only for a personal account, while switches and time remain.
func TestConsentSwitchAccount_OfferedOnlyForAPersonalAccount(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-offer", nil)
	body := checkCard(t, fx)
	require.Equal(t, "rejected", body["status"])
	require.Equal(t, "switch_account", body["action"])
	offered := body["message"]
	require.True(t, strings.HasPrefix(offered, "You're signed in to Speakeasy with a personal account"), offered)

	reset := func(s *mcp.AuthnChallengeState) {
		no := false
		s.AccountSwitches, s.FirstParty, s.AuthorizerImpersonated, s.CreatedAt = 0, false, &no, time.Now()
	}
	for name, tc := range map[string]struct {
		edit    func(*mcp.AuthnChallengeState)
		refusal string
	}{
		"switches used up":  {func(s *mcp.AuthnChallengeState) { s.AccountSwitches = 3 }, "This page can't start a new sign-in. Go back and use a separate sign-in."},
		"dashboard connect": {func(s *mcp.AuthnChallengeState) { s.FirstParty = true }, ""},
		"support session":   {func(s *mcp.AuthnChallengeState) { yes := true; s.AuthorizerImpersonated = &yes }, "This page can't start a new sign-in. Go back and use a separate sign-in."},
		"too old to finish": {func(s *mcp.AuthnChallengeState) { s.CreatedAt = time.Now().Add(-9 * time.Minute) }, "This page expired; start connecting again."},
	} {
		fx.store(t, func(s *mcp.AuthnChallengeState) { reset(s); tc.edit(s) })
		body := checkCard(t, fx)
		require.Equal(t, "rejected", body["status"], name)
		require.NotContains(t, body, "action", name)
		_, err := postSwitchAccount(t, fx, nil)
		requireSwitchRefused(t, err, oops.CodeBadRequest, tc.refusal)
		if tc.refusal != "" {
			requireRefusedSwitchPage(t, fx, tc.refusal)
		}
	}
	require.Empty(t, fx.revoker.sessions())

	fx.store(t, reset)
	for _, reason := range []identitychaining.Reason{identitychaining.ReasonIneligibleIdentity, identitychaining.ReasonAccessDenied} {
		fx.governor.acquire = func(context.Context, identitychaining.Request) identitychaining.Outcome {
			return identitychaining.Outcome{Stage: identitychaining.StageExchange, Reason: reason, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false, Provider: ""}
		}
		body := checkCard(t, fx)
		require.Equal(t, "rejected", body["status"], reason)
		require.NotContains(t, body, "action", "an administrator's refusal isn't fixed by switching accounts")
	}
	fx.governor.acquire = func(context.Context, identitychaining.Request) identitychaining.Outcome {
		return identitychaining.Outcome{Stage: identitychaining.StageExchange, Reason: identitychaining.ReasonProviderUnavailable, Confidence: identitychaining.ConfidenceUnknown, Retryable: true, Cached: false, Provider: ""}
	}
	require.NotContains(t, checkCard(t, fx), "action", "unknown evidence offers nothing")
}

// The action ends only this person's own session, restarts this same request and steers the new sign-in.
func TestConsentSwitchAccount_RestartsTheSameAuthorizationRequest(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-restart", nil)
	before := fx.state(t)
	restarted := switchAccount(t, fx)

	require.Equal(t, []string{fx.session}, fx.revoker.sessions(), "exactly the consent's own sign-in session is ended")
	_, err := fx.ti.authnChallengeCache.Get(fx.ctx, "authnChallenge:"+before.ID)
	require.Error(t, err, "the consent challenge is spent")

	sent := fx.mock.buildAuthURLParams
	require.NotEmpty(t, sent.CallbackURL)
	require.Equal(t, identity.AuthorizationURLParams{
		CallbackURL:     sent.CallbackURL,
		Scope:           "",
		State:           restarted.ID,
		ScopesSupported: nil,
		LoginHint:       fx.subject.ID + "@" + switchWorkDomain,
		ScreenHint:      "",
		Prompt:          "login",
		OrganizationID:  fx.workosOrganization,
	}, sent, "a fresh login, in this organization, for the work email")

	want := before
	want.ID = restarted.ID
	want.Subject = nil
	want.AuthorizerUserID = ""
	want.AuthorizerImpersonated = nil
	want.SignInID = nil
	want.AccountSwitches = 1
	require.NotEqual(t, before.ID, restarted.ID)
	require.Equal(t, want, restarted, "client, redirect URI, PKCE, state, endpoint and flow survive unchanged")
	require.False(t, restarted.FreshLoginForced, "a revoked sign-in later still gets its one restart")

	_, err = postSwitchAccount(t, fx, nil)
	requireSwitchRefused(t, err, oops.CodeBadRequest, "")
	require.Len(t, fx.revoker.sessions(), 1, "a replay ends nothing")
	replayed := refusedSwitchPage(t, fx)
	require.Contains(t, replayed, "You started a new sign-in for this connection.", "a replay explains the switch already made")
	require.NotContains(t, replayed, "idp.example.com")
}

// A personal email off the organization's verified domains is never pre-filled.
func TestConsentSwitchAccount_HintsOnlyAWorkEmail(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-hint", nil)
	setSwitchVerifiedDomains(t, fx, "elsewhere.test")
	switchAccount(t, fx)
	require.Empty(t, fx.mock.buildAuthURLParams.LoginHint)
	require.Equal(t, fx.workosOrganization, fx.mock.buildAuthURLParams.OrganizationID)
}

// The action is bound to the attempt's anti-forgery token, a chained card of this page, and the person's own sign-in.
func TestConsentSwitchAccount_RefusesForgedAndForeignRequests(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-forged", nil)
	before := fx.state(t)

	_, err := postSwitchAccount(t, fx, func(f url.Values) { f.Set("csrf_token", "forged") })
	requireSwitchRefused(t, err, oops.CodeBadRequest, "")
	_, err = postSwitchAccount(t, fx, func(f url.Values) { f.Del("csrf_token") })
	requireSwitchRefused(t, err, oops.CodeBadRequest, "")
	_, err = postSwitchAccount(t, fx, func(f url.Values) { f.Set("state", uuid.NewString()) })
	require.Error(t, err)
	_, err = postSwitchAccount(t, fx, func(f url.Values) { f.Set("client_id", uuid.NewString()) })
	requireSwitchRefused(t, err, oops.CodeBadRequest, "This page changed. Go back, reload it, and try again.")
	_, err = postSwitchAccount(t, fx, func(f url.Values) { f.Del("client_id") })
	requireSwitchRefused(t, err, oops.CodeBadRequest, "")
	_, err = postSwitchAccount(t, fx, func(f url.Values) { f.Set("server", "not-a-gateway-server") })
	requireSwitchRefused(t, err, oops.CodeBadRequest, "This page changed. Go back, reload it, and try again.")

	// A sign-in of someone else: the check is against the consent's own person.
	other := createTestUser(t, fx.ctx, fx.ti, "aim852-other-"+uuid.NewString())
	othersSession := "session_" + uuid.NewString()
	othersSignIn := recordPersonalSignIn(t, fx, other.ID, othersSession)
	fx.store(t, func(s *mcp.AuthnChallengeState) { s.SignInID = &othersSignIn })
	_, err = postSwitchAccount(t, fx, nil)
	requireSwitchRefused(t, err, oops.CodeBadRequest, "")
	require.Empty(t, fx.revoker.sessions(), "nothing is ended for a forged or foreign request")

	fx.store(t, func(s *mcp.AuthnChallengeState) { s.SignInID = before.SignInID })
	w, err := postSwitchAccount(t, fx, func(f url.Values) {
		f.Set("redirect_uri", "https://attacker.example.com/cb")
		f.Set("workos_session_id", othersSession)
	})
	require.NoError(t, err)
	require.Equal(t, "https://idp.example.com/authorize", w.Header().Get("Location"), "posted fields can't choose where it goes")
	require.Equal(t, []string{fx.session}, fx.revoker.sessions(), "or whose session ends")
}

// If the session can't be ended, nothing changes and the person sees a plain error.
func TestConsentSwitchAccount_EndingTheSessionFailsCleanly(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-fail", nil)
	before := fx.state(t)
	fx.revoker.fail(errors.New("identity provider unavailable"))

	_, err := postSwitchAccount(t, fx, nil)
	requireSwitchRefused(t, err, oops.CodeUnavailable, "Speakeasy couldn't start a new sign-in. Try again.")
	require.Equal(t, before, fx.state(t), "the consent page keeps working")
	requireRefusedSwitchPage(t, fx, "Speakeasy couldn't start a new sign-in. Try again.")
	require.Equal(t, before, fx.state(t))
	require.Empty(t, fx.mock.buildAuthURLParams.State, "no sign-in was started")

	fx.revoker.fail(nil)
	require.Equal(t, 1, switchAccount(t, fx).AccountSwitches, "a retry works")
}

// A session WorkOS already ended is what the switch wanted.
func TestConsentSwitchAccount_AlreadyEndedSessionProceeds(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-gone", nil)
	require.NoError(t, fx.revoker.RevokeSession(fx.ctx, fx.session))
	require.Equal(t, 1, switchAccount(t, fx).AccountSwitches)
}

// Once the session has ended, a failure to save the restart leaves the page able to try again.
func TestConsentSwitchAccount_StoreFailureLeavesARetry(t *testing.T) {
	t.Parallel()

	fault := &challengeStoreFault{Cache: nil, mu: sync.Mutex{}, armed: false, keep: ""}
	fx := seedSwitchConsent(t, "aim852-store", func(inner cache.Cache) cache.Cache {
		fault.Cache = inner
		return fault
	})
	before := fx.state(t)
	fault.arm(before.ID)

	_, err := postSwitchAccount(t, fx, nil)
	requireSwitchRefused(t, err, oops.CodeUnavailable, "Speakeasy couldn't start a new sign-in. Try again.")
	require.Equal(t, []string{fx.session}, fx.revoker.sessions(), "the session did end")
	require.Equal(t, before, fx.state(t), "the page is put back")

	require.Equal(t, 1, switchAccount(t, fx).AccountSwitches, "the retry finds the session already ended and goes on")
}

// A company account connects and completes the attempt; another personal account is refused again, and the person is never stranded.
func TestConsentSwitchAccount_FreshSignInIsCheckedAgain(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-again", nil)
	rec := recordConnections(t, fx.ti)
	before := fx.state(t)
	require.Equal(t, "switch_account", checkCard(t, fx)["action"])

	// Picks another personal account.
	restarted := switchAccount(t, fx)
	second := createTestUser(t, fx.ctx, fx.ti, "aim852-second-"+uuid.NewString())
	consent := signInTo(t, fx, restarted.ID, second.ID, "session_"+uuid.NewString(), signins.MethodGoogleOAuth, personalEvidence(second.ID))
	require.Equal(t, second, *consent.Subject)
	require.NotNil(t, consent.SignInID)
	require.NotEqual(t, *before.SignInID, *consent.SignInID)
	require.Equal(t, before.FlowID, consent.FlowID)
	body := checkCard(t, fx)
	require.Equal(t, "rejected", body["status"])
	require.True(t, strings.HasPrefix(body["message"], switchAgainCopy), body["message"])
	require.Equal(t, "switch_account", body["action"], "the person can try again")
	require.Equal(t, *consent.SignInID, fx.governor.checkedSignIns[len(fx.governor.checkedSignIns)-1], "judged against the new sign-in")

	// Then the company account.
	restarted = switchAccount(t, fx)
	require.Equal(t, 2, restarted.AccountSwitches)
	company := createTestUser(t, fx.ctx, fx.ti, "aim852-company-"+uuid.NewString())
	consent = signInTo(t, fx, restarted.ID, company.ID, "session_"+uuid.NewString(), signins.MethodSSO, identity.SignInEvidence{HostedDomain: "", HostedDomainChecked: false, TenantID: "", VerifiedEmail: company.ID + "@" + switchWorkDomain, AuthenticatedAt: time.Now()})
	require.Equal(t, company, *consent.Subject)
	fx.managed[*consent.SignInID] = true
	require.Equal(t, map[string]string{"status": "connected", "message": "Connected through your identity provider"}, checkCard(t, fx))

	loc := postConsentAction(t, fx.ctx, fx.ti, fx.endpoint.Slug, fx.stateID, "approve")
	require.Equal(t, "example.com", loc.Host)
	require.Equal(t, "/cb", loc.Path, "back to the client's own redirect URI")
	require.Equal(t, switchClientState, loc.Query().Get("state"), "with the client's state")
	require.NotEmpty(t, loc.Query().Get("code"))

	require.NoError(t, fx.ti.service.DrainConnectionEndings(fx.ctx))
	rec.RequireOneAttempt()
	require.Equal(t, before.FlowID, rec.Rows()[0].AttemptID.String(), "the switch keeps the same attempt")
	refusals := 0
	for _, row := range rec.Rows() {
		if row.Reason == mcpconnectionattempts.CodeUnmanagedAccount.String() {
			refusals++
		}
	}
	require.Equal(t, 2, refusals, "one personal-account row per sign-in")
}

// A sign-in that comes back revoked after a switch still gets its one restart.
func TestConsentSwitchAccount_RevokedSignInAfterASwitchStillRestarts(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-revoked", nil)
	restarted := switchAccount(t, fx)
	next := createTestUser(t, fx.ctx, fx.ti, "aim852-revoked-"+uuid.NewString())
	revokedSession := "session_" + uuid.NewString()
	_, err := signins.RevokeSession(fx.ctx, fx.ti.conn, []string{fx.orgID}, next.ID, revokedSession)
	require.NoError(t, err)

	fx.mock.buildAuthURLParams = identity.AuthorizationURLParams{CallbackURL: "", Scope: "", State: "", ScopesSupported: nil, LoginHint: "", ScreenHint: "", Prompt: "", OrganizationID: ""}
	w := signInAs(t, fx, restarted.ID, next.ID, revokedSession, signins.MethodSSO, personalEvidence(next.ID))
	require.Equal(t, "https://idp.example.com/authorize", w.Header().Get("Location"), "back to the identity provider, not an error")
	require.Equal(t, "login", fx.mock.buildAuthURLParams.Prompt)
	again, err := fx.ti.authnChallengeCache.Get(fx.ctx, "authnChallenge:"+fx.mock.buildAuthURLParams.State)
	require.NoError(t, err)
	require.True(t, again.FreshLoginForced)
	require.Equal(t, 1, again.AccountSwitches)
}

// Switches are bounded per attempt, and the last refusal still offers a separate sign-in.
func TestConsentSwitchAccount_BoundedPerAttempt(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-bound", nil)
	for i := 1; i <= 3; i++ {
		restarted := switchAccount(t, fx)
		require.Equal(t, i, restarted.AccountSwitches)
		next := createTestUser(t, fx.ctx, fx.ti, "aim852-bound-"+uuid.NewString())
		signInTo(t, fx, restarted.ID, next.ID, "session_"+uuid.NewString(), signins.MethodGoogleOAuth, personalEvidence(next.ID))
	}
	body := checkCard(t, fx)
	require.Equal(t, "rejected", body["status"])
	require.NotContains(t, body, "action")
	_, err := postSwitchAccount(t, fx, nil)
	requireSwitchRefused(t, err, oops.CodeBadRequest, "This page can't start a new sign-in. Go back and use a separate sign-in.")
	require.Len(t, fx.revoker.sessions(), 3)

	code, page, _ := render(t, fx.consentActionFixture)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, page, "Use a separate sign-in instead", "the separate sign-in is still there")
}

// renderSpent GETs a consent page through the browser error handler, as a person sees it.
func renderSpent(t *testing.T, fx consentActionFixture) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+fx.endpoint.Slug+"/connect?state="+url.QueryEscape(fx.stateID), nil)
	w := httptest.NewRecorder()
	oops.BrowserErrHandle(fx.ti.logger, func(w http.ResponseWriter, r *http.Request) error {
		return fx.ti.service.ServeConsent(w, r, fx.endpoint)
	}).ServeHTTP(w, req)
	return w
}

// requireSwitchedPage asserts the spent consent page explains the switch and reveals nothing about the new sign-in.
func requireSwitchedPage(t *testing.T, fx *switchFixture, old consentActionFixture, secrets ...string) {
	t.Helper()
	w := renderSpent(t, old)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, w.Header().Get("Location"), "it never redirects")
	page := strings.Join(strings.Fields(html.UnescapeString(w.Body.String())), " ")
	require.Contains(t, page, "New sign-in started")
	require.Contains(t, page, "You started a new sign-in for this connection.")
	require.Contains(t, page, `Finish it in this tab, or start connecting again from <span class="text-foreground font-medium">Routability Client</span>.`, "with the client's own name")
	require.NotContains(t, page, "Couldn't connect")
	require.NotContains(t, page, "close this tab")
	for _, secret := range append([]string{"idp.example.com", fx.workosOrganization, fx.subject.ID + "@" + switchWorkDomain}, secrets...) {
		require.NotContains(t, page, secret)
	}
}

// Back to the spent consent page says what happened, without redirecting or revealing the new sign-in.
func TestConsentSwitchAccount_SpentConsentURLExplainsTheSwitch(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-back", nil)
	old := fx.consentActionFixture
	first := switchAccount(t, fx)
	requireSwitchedPage(t, fx, old, first.ID)

	// After the new sign-in lands on its consent page.
	second := createTestUser(t, fx.ctx, fx.ti, "aim852-back-"+uuid.NewString())
	consent := signInTo(t, fx, first.ID, second.ID, "session_"+uuid.NewString(), signins.MethodGoogleOAuth, personalEvidence(second.ID))
	requireSwitchedPage(t, fx, old, first.ID, consent.ID)

	// And after a second switch, from both spent pages.
	middle := fx.consentActionFixture
	again := switchAccount(t, fx)
	requireSwitchedPage(t, fx, old, again.ID)
	requireSwitchedPage(t, fx, middle, again.ID)

	other := old
	other.endpoint = &mcp.ResolvedMcpEndpoint{}
	*other.endpoint = *old.endpoint
	other.endpoint.Slug = "another-" + uuid.NewString()[:8]
	page := renderSpent(t, other).Body.String()
	require.NotContains(t, page, "You started a new sign-in", "another server's page keeps the plain expiry")
	require.Contains(t, page, "expired or was already used")
}

// On a gateway server's page, the fresh sign-in returns to that server's page.
func TestConsentSwitchAccount_KeepsTheGatewayServerPage(t *testing.T) {
	t.Parallel()

	ctx, base, metaID, clientID, issuerID := metaConsent(t, "aim852-gw")
	const upstream = "https://aim852-gw.example.com/mcp"
	createMetaMember(t, ctx, base.ti.conn, base.projectID, metaID, "aim852-gw-member", upstream, conv.ToNullUUID(issuerID), 0)
	base.server = "aim852-gw-member"
	base.clientA = clientID
	fx := &switchFixture{
		consentActionFixture: base,
		ctx:                  ctx,
		mock:                 &mockIdentityResolver{hasAccessOK: true, buildAuthURLResult: &url.URL{Scheme: "https", Host: "idp.example.com", Path: "/authorize"}},
		revoker:              nil,
		governor:             nil,
		managed:              map[uuid.UUID]bool{},
		session:              "",
		workosOrganization:   "",
	}
	armSwitch(t, fx, issuerID, upstream, nil)

	_, err := postSwitchAccount(t, fx, func(f url.Values) { f.Set("server", "aim852-not-a-member") })
	requireSwitchRefused(t, err, oops.CodeBadRequest, "This page changed. Go back, reload it, and try again.")
	require.Empty(t, fx.revoker.sessions())

	restarted := switchAccount(t, fx)
	require.Equal(t, "aim852-gw-member", restarted.FocusServer, "the consent page after sign-in is this server's")
}

// The page carries the switch button and its note hidden until the check offers them.
func TestConsentSwitchAccount_PageCarriesHiddenAction(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-page", nil)
	code, page, _ := render(t, fx.consentActionFixture)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, page, `data-switch-account-group hidden`)
	require.Contains(t, page, `value="switch_account"`)
	require.Contains(t, page, `aria-describedby="switch-note-`+fx.clientA.String()+`"`)
	require.Contains(t, page, `id="switch-note-`+fx.clientA.String()+`"`)
	require.Contains(t, page, "Sign in with your company account")
	require.Contains(t, page, "You'll be asked to sign in again.")
	require.Less(t, strings.Index(page, "data-switch-account-group"), strings.Index(page, "data-connect-fallback"), "the company account comes first")
}

// The session lookup answers only for the sign-in's own person and organization.
func TestSessionForSignIn_ScopedToPersonAndOrganization(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-scope", nil)
	signInID := *fx.state(t).SignInID
	own, err := signins.SessionForSignIn(fx.ctx, fx.ti.conn, fx.orgID, fx.subject.ID, signInID)
	require.NoError(t, err)
	require.Equal(t, signins.SignInSession{WorkOSSessionID: fx.session, WorkEmail: fx.subject.ID + "@" + switchWorkDomain, WorkOSOrganizationID: fx.workosOrganization}, own)

	none := signins.SignInSession{WorkOSSessionID: "", WorkEmail: "", WorkOSOrganizationID: ""}
	other, err := signins.SessionForSignIn(fx.ctx, fx.ti.conn, fx.orgID, "someone-else", signInID)
	require.NoError(t, err)
	require.Equal(t, none, other, "another user")
	otherOrg, _ := seedWorkOSOrganization(t, fx.ctx, fx.ti)
	elsewhere, err := signins.SessionForSignIn(fx.ctx, fx.ti.conn, otherOrg, fx.subject.ID, signInID)
	require.NoError(t, err)
	require.Equal(t, none, elsewhere, "another organization")
}
