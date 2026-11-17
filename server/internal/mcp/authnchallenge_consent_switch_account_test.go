package mcp_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/signins"
)

const (
	switchClientState   = "client-state-switch"
	switchCodeChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	switchNoteCopy      = "You'll sign in to Speakeasy again on this browser."
)

// recordingRevoker records the identity provider sessions ended, failing with err.
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
	r.ended = append(r.ended, sessionID)
	return nil
}

func (r *recordingRevoker) sessions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ended...)
}

type switchFixture struct {
	consentActionFixture
	ctx      context.Context //nolint:containedctx // the fixture's test context, like the other consent fixtures
	mock     *mockIdentityResolver
	revoker  *recordingRevoker
	governor *consentGovernor
	// managed are the sign-ins the governor vouches for; any other is a personal account.
	managed map[uuid.UUID]bool
	// session is the WorkOS session behind the consent's current sign-in.
	session string
}

// seedSwitchConsent: a chained consent whose person signed in to Speakeasy with a personal Google account.
func seedSwitchConsent(t *testing.T, tag string) *switchFixture {
	t.Helper()

	mock := &mockIdentityResolver{hasAccessOK: true, buildAuthURLResult: &url.URL{Scheme: "https", Host: "idp.example.com", Path: "/authorize"}}
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
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
		revoker:              &recordingRevoker{mu: sync.Mutex{}, ended: nil, err: nil},
		governor:             nil,
		managed:              map[uuid.UUID]bool{},
		session:              "session_" + uuid.NewString(),
	}
	registerPageClient(t, ctx, fx.consentActionFixture)
	fx.clientA = createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, tag, "", []uuid.UUID{issuerID})
	fx.governor = setConsentGovernor(t, fx.consentActionFixture, clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, fx.clientA), consentChainedUpstream)
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
	ti.service.SetSessionManager(sessions.NewManager(ti.logger, ti.tracerProvider, ti.conn, nil, cache.Suffix("switch-test"), fx.revoker, nil, nil))
	signins.EnableForTests(ti.features, orgID)
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: conv.ToNullUUID(serverID), Slug: slug,
	})
	require.NoError(t, err)

	signInID := recordPersonalSignIn(t, fx, subject.ID, fx.session)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	impersonated := false
	state.Endpoint.McpServerID = conv.ToNullUUID(serverID)
	state.FlowID = uuid.NewString()
	state.State = switchClientState
	state.CodeChallenge = switchCodeChallenge
	state.CodeChallengeMethod = "S256"
	state.AuthorizerUserID = subject.ID
	state.AuthorizerImpersonated = &impersonated
	state.SignInID = &signInID
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	return fx
}

// recordPersonalSignIn records a Google sign-in that answered with no Workspace domain.
func recordPersonalSignIn(t *testing.T, fx *switchFixture, userID, workosSessionID string) uuid.UUID {
	t.Helper()
	recorded, err := signins.Record(fx.ctx, fx.ti.conn, signins.RecordParams{
		OrganizationID:       fx.orgID,
		UserID:               userID,
		WorkOSSessionID:      workosSessionID,
		AuthenticationMethod: signins.MethodGoogleOAuth,
		Evidence:             signins.Evidence{HostedDomain: "", HostedDomainChecked: true, TenantID: "", VerifiedEmail: userID + "@example.test", IDPAuthenticatedAt: time.Now()},
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

// signInAs completes the identity provider callback for a restarted challenge and returns the consent challenge.
func signInAs(t *testing.T, fx *switchFixture, stateID, userID, workosSessionID, method string, evidence identity.SignInEvidence) mcp.AuthnChallengeState {
	t.Helper()
	fx.mock.upsertResult = userID
	fx.mock.exchangeResult = &identity.IDPUserInfo{
		Sub:                  "workos-" + userID,
		Email:                userID + "@example.test",
		Name:                 "Test User",
		OrganizationID:       workOSOrganizationID(t, fx.ctx, fx.ti, fx.orgID),
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
	consentURL, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	fx.stateID = consentURL.Query().Get("state")
	return fx.state(t)
}

func requireSwitchRefused(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

// The check offers the switch only for a personal account, while switches remain.
func TestConsentSwitchAccount_OfferedOnlyForAPersonalAccount(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-offer")
	w, err := postChainCheck(t, fx.consentActionFixture, nil)
	require.NoError(t, err)
	body := chainCheckResult(t, w)
	require.Equal(t, "rejected", body["status"])
	require.Equal(t, "switch_account", body["action"])
	offered := body["message"]
	require.True(t, strings.HasPrefix(offered, "You're signed in to Speakeasy with a personal account"), offered)

	for name, edit := range map[string]func(*mcp.AuthnChallengeState){
		"switches used up":  func(s *mcp.AuthnChallengeState) { s.AccountSwitches = 3 },
		"dashboard connect": func(s *mcp.AuthnChallengeState) { s.FirstParty = true },
		"support session":   func(s *mcp.AuthnChallengeState) { yes := true; s.AuthorizerImpersonated = &yes },
	} {
		fx.store(t, func(s *mcp.AuthnChallengeState) {
			no := false
			s.AccountSwitches, s.FirstParty, s.AuthorizerImpersonated = 0, false, &no
			edit(s)
		})
		w, err := postChainCheck(t, fx.consentActionFixture, nil)
		require.NoError(t, err, name)
		body := chainCheckResult(t, w)
		require.Equal(t, "rejected", body["status"], name)
		require.NotContains(t, body, "action", name)
		require.Equal(t, offered, body["message"], "the buttons carry the actions, not the text")
		_, err = postSwitchAccount(t, fx, nil)
		requireSwitchRefused(t, err, oops.CodeBadRequest)
	}
	require.Empty(t, fx.revoker.sessions())

	for _, reason := range []identitychaining.Reason{identitychaining.ReasonIneligibleIdentity, identitychaining.ReasonAccessDenied} {
		fx.governor.acquire = func(context.Context, identitychaining.Request) identitychaining.Outcome {
			return identitychaining.Outcome{Stage: identitychaining.StageExchange, Reason: reason, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false, Provider: ""}
		}
		fx.store(t, func(s *mcp.AuthnChallengeState) {
			no := false
			s.AccountSwitches, s.FirstParty, s.AuthorizerImpersonated = 0, false, &no
		})
		w, err := postChainCheck(t, fx.consentActionFixture, nil)
		require.NoError(t, err)
		body := chainCheckResult(t, w)
		require.Equal(t, "rejected", body["status"], reason)
		require.NotContains(t, body, "action", "an administrator's refusal isn't fixed by switching accounts")
	}
	fx.governor.acquire = func(context.Context, identitychaining.Request) identitychaining.Outcome {
		return identitychaining.Outcome{Stage: identitychaining.StageExchange, Reason: identitychaining.ReasonProviderUnavailable, Confidence: identitychaining.ConfidenceUnknown, Retryable: true, Cached: false, Provider: ""}
	}
	w, err = postChainCheck(t, fx.consentActionFixture, nil)
	require.NoError(t, err)
	require.NotContains(t, chainCheckResult(t, w), "action", "unknown evidence offers nothing")
}

// The action ends only this person's own session and restarts this same request.
func TestConsentSwitchAccount_RestartsTheSameAuthorizationRequest(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-restart")
	before := fx.state(t)
	restarted := switchAccount(t, fx)

	require.Equal(t, []string{fx.session}, fx.revoker.sessions(), "exactly the consent's own sign-in session is ended")
	_, err := fx.ti.authnChallengeCache.Get(fx.ctx, "authnChallenge:"+fx.stateID)
	require.Error(t, err, "the consent challenge is spent")

	want := before
	want.ID = restarted.ID
	want.Subject = nil
	want.AuthorizerUserID = ""
	want.AuthorizerImpersonated = nil
	want.SignInID = nil
	want.FreshLoginForced = true
	want.AccountSwitches = 1
	require.NotEqual(t, before.ID, restarted.ID)
	require.Equal(t, want, restarted, "client, redirect URI, PKCE, state, endpoint and flow survive unchanged")
	require.Equal(t, switchClientState, restarted.State)
	require.Equal(t, switchCodeChallenge, restarted.CodeChallenge)
	require.Equal(t, before.FlowID, restarted.FlowID, "the same attempt")

	_, err = postSwitchAccount(t, fx, nil)
	requireSwitchRefused(t, err, oops.CodeBadRequest)
	require.Len(t, fx.revoker.sessions(), 1, "a replay ends nothing")
}

// The action is bound to the attempt's anti-forgery token and to the person's own sign-in.
func TestConsentSwitchAccount_RefusesForgedAndForeignRequests(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-forged")
	before := fx.state(t)

	_, err := postSwitchAccount(t, fx, func(f url.Values) { f.Set("csrf_token", "forged") })
	requireSwitchRefused(t, err, oops.CodeBadRequest)
	_, err = postSwitchAccount(t, fx, func(f url.Values) { f.Del("csrf_token") })
	requireSwitchRefused(t, err, oops.CodeBadRequest)
	_, err = postSwitchAccount(t, fx, func(f url.Values) { f.Set("state", uuid.NewString()) })
	require.Error(t, err)

	// A sign-in of someone else: the check is against the consent's own person.
	other := createTestUser(t, fx.ctx, fx.ti, "aim852-other-"+uuid.NewString())
	othersSession := "session_" + uuid.NewString()
	othersSignIn := recordPersonalSignIn(t, fx, other.ID, othersSession)
	fx.store(t, func(s *mcp.AuthnChallengeState) { s.SignInID = &othersSignIn })
	_, err = postSwitchAccount(t, fx, nil)
	requireSwitchRefused(t, err, oops.CodeBadRequest)
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

	fx := seedSwitchConsent(t, "aim852-fail")
	before := fx.state(t)
	fx.revoker.err = errors.New("identity provider unavailable")

	_, err := postSwitchAccount(t, fx, nil)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.Equal(t, "Speakeasy couldn't sign you out; try again.", shareable.Error())
	require.Equal(t, before, fx.state(t), "the consent page keeps working")
	require.Empty(t, fx.mock.buildAuthURLParams.State, "no sign-in was started")

	fx.revoker.err = nil
	restarted := switchAccount(t, fx)
	require.Equal(t, 1, restarted.AccountSwitches, "a retry works")
}

// A company account connects and completes the attempt; another personal account is refused again, and the person is never stranded.
func TestConsentSwitchAccount_FreshSignInIsCheckedAgain(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-again")
	rec := recordConnections(t, fx.ti)
	before := fx.state(t)
	w, err := postChainCheck(t, fx.consentActionFixture, nil)
	require.NoError(t, err)
	require.Equal(t, "switch_account", chainCheckResult(t, w)["action"])

	// Picks another personal account.
	restarted := switchAccount(t, fx)
	second := createTestUser(t, fx.ctx, fx.ti, "aim852-second-"+uuid.NewString())
	consent := signInAs(t, fx, restarted.ID, second.ID, "session_"+uuid.NewString(), signins.MethodGoogleOAuth, identity.SignInEvidence{HostedDomain: "", HostedDomainChecked: true, TenantID: "", VerifiedEmail: second.ID + "@example.test", AuthenticatedAt: time.Now()})
	require.Equal(t, second, *consent.Subject)
	require.NotNil(t, consent.SignInID)
	require.NotEqual(t, *before.SignInID, *consent.SignInID)
	require.Equal(t, before.FlowID, consent.FlowID)
	w, err = postChainCheck(t, fx.consentActionFixture, nil)
	require.NoError(t, err)
	body := chainCheckResult(t, w)
	require.Equal(t, "rejected", body["status"])
	require.Equal(t, "switch_account", body["action"], "the person can try again")
	require.Equal(t, *consent.SignInID, fx.governor.checkedSignIns[len(fx.governor.checkedSignIns)-1], "judged against the new sign-in")

	// Then the company account.
	restarted = switchAccount(t, fx)
	require.Equal(t, 2, restarted.AccountSwitches)
	company := createTestUser(t, fx.ctx, fx.ti, "aim852-company-"+uuid.NewString())
	companySession := "session_" + uuid.NewString()
	consent = signInAs(t, fx, restarted.ID, company.ID, companySession, signins.MethodSSO, identity.SignInEvidence{HostedDomain: "", HostedDomainChecked: false, TenantID: "", VerifiedEmail: company.ID + "@example.test", AuthenticatedAt: time.Now()})
	require.Equal(t, company, *consent.Subject)
	fx.managed[*consent.SignInID] = true
	w, err = postChainCheck(t, fx.consentActionFixture, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"status": "connected", "message": "Connected through your identity provider"}, chainCheckResult(t, w))

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
	require.Equal(t, 1, refusals, "one personal-account row per attempt")
}

// Switches are bounded per attempt, and the last refusal still offers a separate sign-in.
func TestConsentSwitchAccount_BoundedPerAttempt(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-bound")
	for i := 1; i <= 3; i++ {
		restarted := switchAccount(t, fx)
		require.Equal(t, i, restarted.AccountSwitches)
		next := createTestUser(t, fx.ctx, fx.ti, "aim852-bound-"+uuid.NewString())
		signInAs(t, fx, restarted.ID, next.ID, "session_"+uuid.NewString(), signins.MethodGoogleOAuth, identity.SignInEvidence{HostedDomain: "", HostedDomainChecked: true, TenantID: "", VerifiedEmail: next.ID + "@example.test", AuthenticatedAt: time.Now()})
	}
	w, err := postChainCheck(t, fx.consentActionFixture, nil)
	require.NoError(t, err)
	body := chainCheckResult(t, w)
	require.Equal(t, "rejected", body["status"])
	require.NotContains(t, body, "action")
	_, err = postSwitchAccount(t, fx, nil)
	requireSwitchRefused(t, err, oops.CodeBadRequest)
	require.Len(t, fx.revoker.sessions(), 3)

	code, page, _ := render(t, fx.consentActionFixture)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, page, "Use a separate sign-in instead", "the separate sign-in is still there")
}

// The page carries the switch button hidden until the check offers it.
func TestConsentSwitchAccount_PageCarriesHiddenAction(t *testing.T) {
	t.Parallel()

	fx := seedSwitchConsent(t, "aim852-page")
	code, page, _ := render(t, fx.consentActionFixture)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, page, `value="switch_account"`)
	require.Contains(t, page, "data-switch-account hidden")
	require.Contains(t, page, "Sign in with your company account")
	require.Contains(t, page, "data-switch-account-note hidden >"+switchNoteCopy)
	require.Less(t, strings.Index(page, "data-switch-account"), strings.Index(page, "data-connect-fallback"), "the company account comes first")
}
