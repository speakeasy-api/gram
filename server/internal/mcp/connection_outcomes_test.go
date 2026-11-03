package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// recordConnections makes the service record to memory.
func recordConnections(t *testing.T, ti *testInstance) *mcpconnectionattemptstest.Recording {
	t.Helper()
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	t.Cleanup(rec.RequireNoUnclassifiedExit)
	t.Cleanup(func() { _ = ti.service.DrainConnectionEndings(context.WithoutCancel(t.Context())) })
	return rec
}

func serveConsentRoute(t *testing.T, ctx context.Context, ti *testInstance, slug string, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(w, req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))))
	return w
}

func seedAttemptChallenge(t *testing.T, ctx context.Context, ti *testInstance, toolset hostedToolset, client usersessions_repo.UserSessionClient, verifier string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(verifier))
	subject := urn.NewUserSubject("attempt-user-" + uuid.NewString())
	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		FlowID:              uuid.NewString(),
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint:            mcp.EndpointRef{McpSlug: toolset.McpSlug.String, CustomDomainID: toolset.CustomDomainID},
		ClientID:            client.ClientID,
		RedirectURI:         client.RedirectUris[0],
		State:               "client-state",
		CodeChallenge:       base64.RawURLEncoding.EncodeToString(sum[:]),
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		Subject:             &subject,
		CreatedAt:           time.Now(),
	}))
	return stateID
}

func postConsentAction(t *testing.T, ctx context.Context, ti *testInstance, slug, stateID, action string) *url.URL {
	t.Helper()
	form := url.Values{"state": {stateID}, "csrf_token": {"csrf-token"}, "action": {action}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := serveConsentRoute(t, ctx, ti, slug, req)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	return loc
}

// Approving keeps the attempt open, and /token is its Connected ending.
func TestConnectionOutcome_ConsentThenTokenEndsConnected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	verifier := "verifier-" + uuid.NewString()
	stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, verifier)

	loc := postConsentAction(t, ctx, ti, toolset.McpSlug.String, stateID, "approve")
	rec.RequireStep(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultAccepted)
	rec.RequireNotEnded()

	w := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, loc.Query().Get("code"), verifier))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued)
	require.Equal(t, string(mcpconnectionattempts.OutcomeConnected), row.Outcome)
	require.Equal(t, oauthwire.GrantTypeAuthorizationCode, row.GrantType)
	require.Equal(t, client.ID.String(), row.UserSessionClientID)
	require.NotEmpty(t, row.SubjectURN)
	rec.RequireOneAttempt()
}

func TestConnectionOutcome_ConsentDeclinedEndsCanceled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, "v")

	loc := postConsentAction(t, ctx, ti, toolset.McpSlug.String, stateID, "deny")
	require.Equal(t, "access_denied", loc.Query().Get("error"))

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeCanceled)
	require.Equal(t, string(mcpconnectionattempts.OutcomeDeclined), row.Outcome)
}

// A code misused at /token is burned, so it ends the attempt it belongs to.
func TestConnectionOutcome_BurnedCodeEndsItsAttempt(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mutate func(url.Values)
		what   mcpconnectionattempts.Code
	}{
		"pkce mismatch":     {func(f url.Values) { f.Set("code_verifier", "wrong") }, mcpconnectionattempts.CodePKCEMismatch},
		"redirect mismatch": {func(f url.Values) { f.Set("redirect_uri", "http://other.example/cb") }, mcpconnectionattempts.CodeRedirectURIMismatch},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			rec := recordConnections(t, ti)
			toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			verifier := "verifier-" + uuid.NewString()
			stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, verifier)
			loc := postConsentAction(t, ctx, ti, toolset.McpSlug.String, stateID, "approve")

			form := codeGrantForm(client, loc.Query().Get("code"), verifier)
			tc.mutate(form)
			require.Equal(t, http.StatusBadRequest, postForm(t, ti, toolset.McpSlug.String, "token", form).Code)

			rec.RequireEnded(mcpconnectionattempts.StepInboundToken, tc.what)
			rec.RequireOneAttempt()
		})
	}
}

// A wrong endpoint leaves the code usable, so the real redemption still ends the attempt once.
func TestConnectionOutcome_WrongEndpointDoesNotEndTheAttempt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	correct, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, toolset.McpSlug.String, "mcp")
	require.NoError(t, err)
	ref, err := correct.EndpointRef(ctx, ti.conn, ti.serverURL.String())
	require.NoError(t, err)
	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	code := "code-" + uuid.NewString()
	attempt := mcpconnectionattempts.Begin(mcpconnectionattempts.ToolsetScope("org-test", toolset.ProjectID, toolset.ID, mcpconnectionattempts.AppDCR))
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
		Code: code, Attempt: attempt, UserSessionIssuerID: issuer.ID, UserSessionClientID: client.ID,
		ClientID: client.ClientID, RedirectURI: client.RedirectUris[0],
		CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]), CodeChallengeMethod: "S256",
		Subject: urn.NewUserSubject("endpoint-user-" + uuid.NewString()), Endpoint: &ref, CreatedAt: time.Now(),
	}))

	sibling := seedSiblingToolset(t, ctx, ti, toolset, issuer.ID)
	require.Equal(t, http.StatusBadRequest, postForm(t, ti, sibling, "token", codeGrantForm(client, code, verifier)).Code)
	refused := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeResourceMismatch)
	require.NotEqual(t, attempt.ID(), refused.AttemptID)

	require.Equal(t, http.StatusOK, postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, verifier)).Code)
	issued := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued)
	require.Equal(t, attempt.ID(), issued.AttemptID)
}

func TestConnectionOutcome_AuthorizeRefusals(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mutate func(url.Values)
		what   mcpconnectionattempts.Code
		status int
	}{
		"redirect mismatch": {func(q url.Values) { q.Set("redirect_uri", "http://other.example/cb") }, mcpconnectionattempts.CodeRedirectURIMismatch, http.StatusBadRequest},
		"resource mismatch": {func(q url.Values) { q.Set("resource", "https://other.example/mcp") }, mcpconnectionattempts.CodeResourceMismatch, http.StatusBadRequest},
		"missing pkce":      {func(q url.Values) { q.Del("code_challenge") }, mcpconnectionattempts.CodeInvalidRequest, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			rec := recordConnections(t, ti)
			toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			q := url.Values{"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {client.RedirectUris[0]}, "state": {"s"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
			tc.mutate(q)
			req := httptest.NewRequest(http.MethodGet, "/mcp/"+toolset.McpSlug.String+"/authorize?"+q.Encode(), nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
			w := httptest.NewRecorder()
			require.NoError(t, ti.service.HandleAuthorize(w, req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))))
			require.NotEqual(t, http.StatusInternalServerError, w.Code)

			rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, tc.what)
			require.Len(t, rec.Rows(), 1)
		})
	}
}

// A client the issuer's admission policy refuses is Blocked, and the admin fixes it.
func TestConnectionOutcome_AuthorizeAdmissionDenied(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	rec := recordConnections(t, ti)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModeDisabled)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	redirect, ok := ds.doc["redirect_uris"].([]any)
	require.True(t, ok)
	first, ok := redirect[0].(string)
	require.True(t, ok)
	q := url.Values{"response_type": {"code"}, "client_id": {ds.clientID}, "redirect_uri": {first}, "state": {"s"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(ctx, http.MethodGet, "/mcp/"+toolset.McpSlug.String+"/authorize?"+q.Encode(), nil))

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeCIMDAdmissionDenied)
	require.Equal(t, string(mcpconnectionattempts.OutcomeBlocked), row.Outcome)
	require.Equal(t, mcpconnectionattempts.AppUnverified.String(), row.AppKey)
}

func TestConnectionOutcome_SignIn(t *testing.T) {
	t.Parallel()

	serve := func(t *testing.T, mock *mockIdentityResolver, query url.Values) *mcpconnectionattemptstest.Recording {
		t.Helper()
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
		rec := recordConnections(t, ti)
		challengeID := seedIDPCallbackChallenge(t, ctx, ti)
		state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+challengeID)
		require.NoError(t, err)
		state.FlowID = uuid.NewString()
		require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
		query.Set("state", challengeID)
		req := httptest.NewRequest(http.MethodGet, "/mcp/idp_callback?"+query.Encode(), nil).WithContext(ctx)
		_ = ti.service.HandleIDPCallback(httptest.NewRecorder(), req)
		return rec
	}

	t.Run("success continues to consent", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, memberMock(), url.Values{"code": {"c"}})
		row := rec.RequireStep(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.ResultSignedIn)
		require.NotEmpty(t, row.SubjectURN)
		rec.RequireNotEnded()
	})
	t.Run("not a member", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.hasAccessOK = false
		rec := serve(t, mock, url.Values{"code": {"c"}})
		row := rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeNotOrgMember)
		require.NotEmpty(t, row.SubjectURN)
	})
	t.Run("canceled at the sign-in page", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, memberMock(), url.Values{"error": {"access_denied"}})
		rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeCanceled)
	})
	t.Run("sign-in service error", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, memberMock(), url.Values{"error": {"server_error"}})
		row := rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeSignInProviderError)
		require.Equal(t, "server_error", row.SubReason)
	})
	t.Run("an error the query made up is neither stored nor an outage", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, memberMock(), url.Values{"error": {"made_up_by_caller"}})
		row := rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeSignInProviderError)
		require.Equal(t, "unregistered", row.SubReason)
	})
	t.Run("a rejected code is not an outage", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.exchangeErr = identity.ErrCodeRejected
		rec := serve(t, mock, url.Values{"code": {"garbage"}})
		rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeSignInProviderError)
	})
	t.Run("a real exchange outage", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.exchangeErr = context.DeadlineExceeded
		rec := serve(t, mock, url.Values{"code": {"c"}})
		rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeIDPUnavailable)
	})
	t.Run("membership check down", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.hasAccessErr = context.DeadlineExceeded
		rec := serve(t, mock, url.Values{"code": {"c"}})
		rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeInternalError)
	})
}

// A first-party connect has no /token: it opens an attempt, and finishing ends it once however often the page renders.
func TestConnectionOutcome_FirstPartyEndsOnceWhenNothingIsLeft(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{buildAuthURLResult: &url.URL{Scheme: "https", Host: "idp.example.com"}})
	rec := recordConnections(t, ti)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := toolset.McpSlug.String

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleFirstPartyConnect(w, httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect/first-party", nil).WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))))
	require.Equal(t, http.StatusFound, w.Code)
	start := rec.RequireStep(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted)
	require.Equal(t, mcpconnectionattempts.AppFirstParty.String(), start.AppKey)

	subject := urn.NewUserSubject("first-party-" + uuid.NewString())
	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID: stateID, FlowID: start.AttemptID.String(), UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint:  mcp.EndpointRef{McpSlug: slug, CustomDomainID: toolset.CustomDomainID},
		CSRFToken: "csrf-token", Subject: &subject, CreatedAt: start.AttemptStartedAt, FirstParty: true,
	}))
	for range 2 {
		page := serveConsentRoute(t, ctx, ti, slug, httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect?state="+stateID, nil))
		require.Equal(t, http.StatusOK, page.Code, page.Body.String())
	}

	require.NoError(t, ti.service.DrainConnectionEndings(ctx))
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete)
	require.Equal(t, string(mcpconnectionattempts.OutcomeConnected), row.Outcome)
	rec.RequireOneAttempt()
}

// The check of a new provider sign-in records its verdict and leaves the attempt open.
func TestConnectionOutcome_PostSignInCheck(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		forbid bool
		what   mcpconnectionattempts.Happened
	}{
		"accepted": {false, mcpconnectionattempts.ResultAllowed},
		"rejected": {true, mcpconnectionattempts.CodeRejectedByMember},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, fx := seedStandaloneValidationFixture(t, "attempt-probe-"+uuid.NewString()[:6])
			rec := recordConnections(t, fx.ti)
			state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
			require.NoError(t, err)
			state.FlowID = uuid.NewString()
			state.CreatedAt = time.Now()
			require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))
			if tc.forbid {
				fx.member.set(memberForbids)
			}

			fx.ti.service.VerifyRemoteGrantOn(ctx, fx.endpoint, remotesessions.RemoteGrant{
				ParentChallengeID:     fx.stateID,
				UserSessionIssuerID:   fx.endpoint.UserSessionIssuerID,
				RemoteSessionClientID: fx.clientID,
				Subject:               fx.subject,
			})

			row := rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, tc.what)
			require.Equal(t, state.FlowID, row.AttemptID.String())
			require.Equal(t, "connect", row.SubReason)
			require.Equal(t, fx.clientID.String(), row.RemoteSessionClientID)
			require.NotEmpty(t, row.RemoteSessionID)
			rec.RequireNotEnded()
		})
	}
}

func seedSiblingToolset(t *testing.T, ctx context.Context, ti *testInstance, toolset hostedToolset, issuerID uuid.UUID) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	other := createPublicMCPToolset(t, ctx, ti, authCtx, "sibling-"+uuid.NewString()[:8])
	other = setToolsetIssuer(t, ctx, ti.conn, toolset.ProjectID, other, issuerID)
	return other.McpSlug.String
}

// A recovery link has one sign-in and no consent page: reaching the provider opens the attempt, and its callback ends it.
func TestConnectionOutcome_RecoveryLinkEndsAtTheProvider(t *testing.T) {
	t.Parallel()

	f := newConnectionRecoveryFixture(t)
	rec := mcpconnectionattemptstest.New(t)
	f.ti.service.SetConnectionRecorder(rec.Recorder)
	manager := f.ti.service.RemoteChallengeManager()
	remotesessions.WithConnectionRecorder(rec.Recorder)(manager)

	path, err := f.ti.service.IssueConnectionRecovery(f.ctx, f.link)
	require.NoError(t, err)
	location, _ := f.start(t, path, f.userID)

	start := rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.ResultAccepted)
	require.Equal(t, mcpconnectionattempts.AppRecoveryLink.String(), start.AppKey)
	rec.RequireNotEnded()

	callback := httptest.NewRequestWithContext(f.ctx, http.MethodGet, "/mcp/remote_login_callback?error=access_denied&state="+url.QueryEscape(location.Query().Get("state"))+"&iss="+url.QueryEscape(location.Query().Get("iss")), nil)
	_, err = manager.CompleteRemoteLogin(callback)
	require.Error(t, err)

	rec.RequireEnded(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderDenied)
	rec.RequireOneAttempt()
}

func TestConnectionOutcome_AuthorizeSignInServiceDownEndsTheAttempt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{buildAuthURLErr: context.DeadlineExceeded})
	rec := recordConnections(t, ti)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	q := url.Values{"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {client.RedirectUris[0]}, "state": {"s"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+toolset.McpSlug.String+"/authorize?"+q.Encode(), nil)
	require.Error(t, ti.service.HandleAuthorize(httptest.NewRecorder(), req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))))

	rec.RequireStep(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted)
	rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeIDPUnavailable)
	rec.RequireOneAttempt()
}

// Nothing proves a flow behind these, so anyone could otherwise write final rows into an organization.
func TestConnectionOutcome_AnonymousRequestsWriteNothing(t *testing.T) {
	t.Parallel()

	serve := func(t *testing.T, build func(slug, clientID, redirect string) *http.Request) {
		t.Helper()
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
		rec := recordConnections(t, ti)
		toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		router := goahttp.NewMuxer()
		mcp.Attach(router, ti.service.Service, nil)
		router.ServeHTTP(httptest.NewRecorder(), build(toolset.McpSlug.String, client.ClientID, client.RedirectUris[0]).WithContext(ctx))
		rec.RequireNothing()
	}
	get := func(target string) *http.Request { return httptest.NewRequest(http.MethodGet, target, nil) }
	form := func(target string, values url.Values) *http.Request {
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req
	}
	for name, build := range map[string]func(slug, clientID, redirect string) *http.Request{
		"consent state that never existed": func(slug, _, _ string) *http.Request {
			return get("/mcp/" + slug + "/connect?state=" + uuid.NewString())
		},
		"unknown client at authorize": func(slug, _, redirect string) *http.Request {
			return get("/mcp/" + slug + "/authorize?" + url.Values{"response_type": {"code"}, "client_id": {"client_missing"}, "redirect_uri": {redirect}, "code_challenge": {"c"}, "code_challenge_method": {"S256"}}.Encode())
		},
		"code mode ticket that never existed": func(slug, _, _ string) *http.Request {
			return get("/mcp/" + slug + "/connect/first-party?code_reconnect=" + uuid.NewString())
		},
		"code mode ticket that is not an id": func(slug, _, _ string) *http.Request {
			return get("/mcp/" + slug + "/connect/first-party?code_reconnect=garbage")
		},
		"registration body that does not parse": func(slug, _, _ string) *http.Request {
			req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/register", strings.NewReader("not json"))
			req.Header.Set("Content-Type", "application/json")
			return req
		},
		"unknown client at token": func(slug, _, _ string) *http.Request {
			return form("/mcp/"+slug+"/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {"client_missing"}, "code": {"c"}, "code_verifier": {"v"}, "redirect_uri": {"http://x/cb"}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			serve(t, build)
		})
	}
}

func TestConnectionOutcome_RegistrationMetadataRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/register", strings.NewReader(`{"client_name":"x","redirect_uris":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeRegistrationInvalid)
	require.Equal(t, string(mcpconnectionattempts.WhoApp), row.WhoFixes)
}

// A live ticket past its time is proof of a real flow, on the server it names.
func TestConnectionOutcome_ExpiredCodeModeTicket(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	slug := "expired-ticket-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	ref, err := endpoint.EndpointRef(ctx, ti.conn, ti.serverURL.String())
	require.NoError(t, err)
	ticket := mcp.AuthnChallengeState{ID: uuid.NewString(), FirstParty: true, CreatedAt: time.Now(), Endpoint: ref, UserSessionIssuerID: issuer, CodeReconnect: &mcp.CodeReconnectBinding{UserID: auth.UserID, Server: "tracker", ServerID: uuid.New(), Ticket: true, ExpiresAt: time.Now().Add(-time.Minute)}}
	require.NoError(t, ti.authnChallengeCache.Store(ctx, ticket))

	req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect/first-party?code_reconnect="+ticket.ID, nil).WithContext(ctx)
	require.Error(t, ti.service.ServeFirstPartyConnect(httptest.NewRecorder(), req, endpoint))

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeExpired)
	require.Equal(t, mcpconnectionattempts.AppCodeMode.String(), row.AppKey)
}

// The endpoint comes from the route, so a state from elsewhere must not write into it.
func TestConnectionOutcome_ForeignStateWritesNothingOnThisServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	mine, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	victim, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	stateID := seedAttemptChallenge(t, ctx, ti, mine, client, "v")
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	state.Browser = &mcp.ChallengeBrowserBinding{CookieID: "cookie", OriginHash: "hash", CallbackHash: "hash"}
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	slug := victim.McpSlug.String
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)

	form := url.Values{"state": {stateID}, "csrf_token": {"csrf-token"}, "action": {"approve"}}
	post := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode())).WithContext(ctx)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req
	}
	require.Error(t, ti.service.ServeConsent(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect?state="+stateID, nil).WithContext(ctx), endpoint))
	require.Error(t, ti.service.ServeConsent(httptest.NewRecorder(), post(), endpoint))
	require.Error(t, ti.service.ServeConsentAction(httptest.NewRecorder(), post(), endpoint))
	rec.RequireNothing()

	// Without the browser binding the state reaches the endpoint check, and still writes nothing here.
	state.Browser = nil
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	require.Error(t, ti.service.ServeConsent(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect?state="+stateID, nil).WithContext(ctx), endpoint))
	rec.RequireNothing()
}

func TestConnectionOutcome_WrongBrowserOnItsOwnServerIsAStep(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, "v")
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	state.Browser = &mcp.ChallengeBrowserBinding{CookieID: "cookie", OriginHash: "hash", CallbackHash: "hash"}
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, toolset.McpSlug.String, "mcp")
	require.NoError(t, err)

	require.Error(t, ti.service.ServeConsent(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/mcp/x/connect?state="+stateID, nil).WithContext(ctx), endpoint))

	rec.RequireStep(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeSessionChanged)
	rec.RequireNotEnded()
}

// Back, reload or a second tab after approving is not a failure.
func TestConnectionOutcome_ReloadAfterApproveRecordsNothingMore(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, "v")
	postConsentAction(t, ctx, ti, toolset.McpSlug.String, stateID, "approve")
	before := len(rec.Rows())

	slug := toolset.McpSlug.String
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect?state="+stateID, nil)
	require.Error(t, ti.service.HandleConsent(httptest.NewRecorder(), req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))))

	require.Len(t, rec.Rows(), before)
	rec.RequireNotEnded()
}

// A second redemption has no grant left: the first connected, and nothing failed.
func TestConnectionOutcome_ReplayedCodeWritesNoSecondEnding(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	verifier := "verifier-" + uuid.NewString()
	stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, verifier)
	loc := postConsentAction(t, ctx, ti, toolset.McpSlug.String, stateID, "approve")

	form := codeGrantForm(client, loc.Query().Get("code"), verifier)
	require.Equal(t, http.StatusOK, postForm(t, ti, toolset.McpSlug.String, "token", form).Code)
	require.Equal(t, http.StatusBadRequest, postForm(t, ti, toolset.McpSlug.String, "token", form).Code)

	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued)
	rec.RequireOneAttempt()
}

func TestConnectionOutcome_ConcurrentRequestsEndOnce(t *testing.T) {
	t.Parallel()

	race := func(n int, call func()) {
		var group sync.WaitGroup
		for range n {
			group.Go(call)
		}
		group.Wait()
	}

	t.Run("double submit of a decline", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
		rec := recordConnections(t, ti)
		toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, "v")
		slug := toolset.McpSlug.String
		race(4, func() {
			form := url.Values{"state": {stateID}, "csrf_token": {"csrf-token"}, "action": {"deny"}}
			req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("mcpSlug", slug)
			_ = ti.service.HandleConsent(httptest.NewRecorder(), req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx)))
		})
		rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeCanceled)
		rec.RequireOneAttempt()
	})
	t.Run("double submit of an approval", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
		rec := recordConnections(t, ti)
		toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, "v")
		slug := toolset.McpSlug.String
		race(4, func() {
			form := url.Values{"state": {stateID}, "csrf_token": {"csrf-token"}, "action": {"approve"}}
			req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("mcpSlug", slug)
			_ = ti.service.HandleConsent(httptest.NewRecorder(), req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx)))
		})
		rec.RequireStep(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultAccepted)
		rec.RequireNotEnded()
	})
	t.Run("double token", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
		rec := recordConnections(t, ti)
		toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		verifier := "verifier-" + uuid.NewString()
		stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, verifier)
		loc := postConsentAction(t, ctx, ti, toolset.McpSlug.String, stateID, "approve")
		form := codeGrantForm(client, loc.Query().Get("code"), verifier)
		race(4, func() { postForm(t, ti, toolset.McpSlug.String, "token", form) })
		rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued)
		rec.RequireOneAttempt()
	})
}

// A chained first-party connect has no provider callback: its check is the ending.
func TestConnectionOutcome_ChainedFirstPartyEnds(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "attempt-chain-"+uuid.NewString()[:6])
	g := setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	g.acquire = func(context.Context, identitychaining.Request) identitychaining.Outcome {
		return identitychaining.Outcome{Stage: identitychaining.StageComplete, Reason: identitychaining.ReasonSuccess, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false, Provider: ""}
	}
	rec := recordConnections(t, fx.ti)
	state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	state.FlowID = uuid.NewString()
	state.CreatedAt = time.Now()
	state.FirstParty = true
	require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))

	for range 2 {
		w, err := postChainCheck(t, fx, nil)
		require.NoError(t, err)
		require.Equal(t, "connected", chainCheckResult(t, w)["status"])
	}

	require.NoError(t, fx.ti.service.DrainConnectionEndings(ctx))
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete)
	require.Equal(t, state.FlowID, row.AttemptID.String())
}

// A provider callback whose sign-in has gone ends that attempt once, however many callbacks follow.
func TestConnectionOutcome_ParentGoneDuringProviderSignIn(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (context.Context, consentActionFixture, *mcpconnectionattemptstest.Recording, mcp.AuthnChallengeState, string) {
		t.Helper()
		ctx, fx, _ := seedChainedConsentEndpoint(t, "attempt-gone-"+uuid.NewString()[:6])
		rec := recordConnections(t, fx.ti)
		state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
		require.NoError(t, err)
		state.FlowID = uuid.NewString()
		state.CreatedAt = time.Now()
		require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))
		return ctx, fx, rec, state, postConnectAction(t, fx, fx.clientA).Query().Get("state")
	}
	callback := func(ctx context.Context, fx consentActionFixture, login string) error {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/mcp/remote_login_callback?code=c&state="+url.QueryEscape(login), nil)
		return fx.ti.service.HandleRemoteLoginCallback(httptest.NewRecorder(), req)
	}

	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		ctx, fx, rec, _, login := setup(t)
		_, err := fx.ti.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+fx.stateID)
		require.NoError(t, err)
		require.Error(t, callback(ctx, fx, login))
		require.Error(t, callback(ctx, fx, login))
		require.NoError(t, fx.ti.service.DrainConnectionEndings(ctx))
		row := rec.RequireEnded(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeExpired)
		require.NotEmpty(t, row.ProviderHost)
		rec.RequireOneAttempt()
	})
	t.Run("decided in another tab is not an expiry", func(t *testing.T) {
		t.Parallel()
		ctx, fx, rec, _, login := setup(t)
		form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"deny"}}
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect", strings.NewReader(form.Encode())).WithContext(ctx)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		require.NoError(t, fx.ti.service.ServeConsent(httptest.NewRecorder(), req, fx.endpoint))
		require.Error(t, callback(ctx, fx, login))
		require.NoError(t, fx.ti.service.DrainConnectionEndings(ctx))
		rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeCanceled)
	})
	// Consent consumes the state and reserves the ending in one call, so a racing
	// callback that finds the parent gone can never also call it expired.
	t.Run("a callback racing the decision is never an expiry", func(t *testing.T) {
		t.Parallel()
		ctx, fx, rec, state, _ := setup(t)
		for range 10 {
			state.FlowID = uuid.NewString()
			state.CreatedAt = time.Now()
			require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))
			login := postConnectAction(t, fx, fx.clientA).Query().Get("state")
			var group sync.WaitGroup
			group.Go(func() {
				form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"deny"}}
				req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect", strings.NewReader(form.Encode())).WithContext(ctx)
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				_ = fx.ti.service.ServeConsent(httptest.NewRecorder(), req, fx.endpoint)
			})
			group.Go(func() {
				req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/mcp/remote_login_callback?error=access_denied&state="+url.QueryEscape(login), nil)
				_ = fx.ti.service.HandleRemoteLoginCallback(httptest.NewRecorder(), req)
			})
			group.Wait()
		}
		require.NoError(t, fx.ti.service.DrainConnectionEndings(ctx))
		rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeCanceled)
		for _, row := range rec.Rows() {
			require.NotEqual(t, mcpconnectionattempts.CodeExpired.String(), row.Reason)
		}
	})
}

// Direct tools/list writes only real failures, after the inventory is built.
func TestConnectionOutcome_DirectListingWritesOnlyFailures(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := recordConnections(t, ti)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha")
	downSlug := "down-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Down", downSlug, 2, newListingUpstream(t, nil, func() bool { return false }))
	down, err := mcpserversrepo.New(ti.conn).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{Slug: conv.ToPGText(downSlug), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")

	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	row := rec.RequireEnded(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeMemberToolsMissing)
	require.Equal(t, down.ID.String(), row.MemberID)
	require.Len(t, rec.Rows(), 1, "no Connected listing and no row for a healthy member")
}

// A member with invalid definitions is left out of the listing, and recorded.
func TestConnectionOutcome_MalformedMemberIsRecorded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := recordConnections(t, ti)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newListingUpstream(t, []any{map[string]any{"description": "missing name", "inputSchema": map[string]any{"type": "object"}}}, nil)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Catalog member", "catalog-member", 1, upstream)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	_, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	rec.RequireEnded(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeMemberToolsMissing)
}

// A consent capture records each member on the sign-in's attempt, with no listing rows.
func TestConnectionOutcome_ConsentCaptureRecordsMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "gateway-consent-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer.ID)
	docsSlug := "docs-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Docs", docsSlug, 1, newRecordingUpstream(t, "search_docs").url)
	docs, err := mcpserversrepo.New(ti.conn).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{Slug: conv.ToPGText(docsSlug), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	state.FlowID = uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))

	consentGetPageWithContext(t, ctx, ti, slug, stateID)

	row := rec.RequireStep(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.ResultAllowed)
	require.Equal(t, docs.ID.String(), row.MemberID)
	require.Equal(t, state.FlowID, row.AttemptID.String())
	rec.RequireNotEnded()
	for _, got := range rec.Rows() {
		require.NotEqual(t, mcpconnectionattempts.StepDiscoveryListing.String(), got.Step)
	}
}

// A real app with broken metadata is a connection problem for an admin to see. Only the host is stored.
func TestConnectionOutcome_AuthorizeClientMetadataFailures(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mutate func(ds *cimdDocServer)
		what   mcpconnectionattempts.Code
	}{
		"metadata invalid": {func(ds *cimdDocServer) { ds.doc["client_id"] = ds.srv.URL + "/oauth/other.json" }, mcpconnectionattempts.CodeCIMDMetadataInvalid},
		"fetch failed":     {func(ds *cimdDocServer) { ds.status = http.StatusInternalServerError }, mcpconnectionattempts.CodeCIMDFetchFailed},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, ti, ds, toolset := newTestCIMDService(t)
			rec := recordConnections(t, ti)
			ds.set(t, tc.mutate)
			router := goahttp.NewMuxer()
			mcp.Attach(router, ti.service.Service, nil)
			q := url.Values{"response_type": {"code"}, "client_id": {ds.clientID}, "redirect_uri": {ds.srv.URL + "/callback"}, "state": {"s"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodGet, "/mcp/"+toolset.McpSlug.String+"/authorize?"+q.Encode(), nil))
			require.Contains(t, []int{http.StatusBadRequest, http.StatusServiceUnavailable}, w.Code, w.Body.String())

			row := rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, tc.what)
			require.Equal(t, string(mcpconnectionattempts.WhoApp), row.WhoFixes)
			require.Equal(t, mcpconnectionattempts.AppOf(ds.clientID).String(), row.AppKey)
			require.NotContains(t, row.AppKey, "/", "host only")
			require.Len(t, rec.Rows(), 1)
		})
	}
}

// An expired code reconnect is expired, however wrong the browser looks.
func TestConnectionOutcome_ExpiredReviewIsNotAWrongBrowser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	stateID := seedAttemptChallenge(t, ctx, ti, toolset, client, "v")
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	state.CodeReconnect = &mcp.CodeReconnectBinding{UserID: state.Subject.ID, Server: "tracker", ServerID: uuid.New(), ExpiresAt: time.Now().Add(-time.Minute)}
	state.FirstParty = true
	state.FocusServer = "tracker"
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, toolset.McpSlug.String, "mcp")
	require.NoError(t, err)

	req := func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "/mcp/x/connect?state="+stateID, nil).WithContext(ctx)
	}
	require.Error(t, ti.service.ServeConsent(httptest.NewRecorder(), req(), endpoint))
	require.Error(t, ti.service.ServeConsent(httptest.NewRecorder(), req(), endpoint))

	require.NoError(t, ti.service.DrainConnectionEndings(ctx))
	rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeExpired)
}
