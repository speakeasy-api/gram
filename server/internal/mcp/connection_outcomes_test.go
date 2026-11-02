package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// recordConnections makes the service record to memory.
func recordConnections(t *testing.T, ti *testInstance) *mcpconnectionattemptstest.Recording {
	t.Helper()
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
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

func TestConnectionOutcome_ConsentAfterExpiryIsOneRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	rec := recordConnections(t, ti)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	req := httptest.NewRequest(http.MethodGet, "/mcp/"+toolset.McpSlug.String+"/connect?state="+uuid.NewString(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	require.Error(t, ti.service.HandleConsent(httptest.NewRecorder(), req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))))

	rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeExpired)
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

// A second redemption has no grant left, so it cannot end the attempt again.
func TestConnectionOutcome_ReplayedCodeIsItsOwnAttempt(t *testing.T) {
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
	replay := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeCodeNotFoundOrExpired)
	require.Equal(t, string(mcpconnectionattempts.WhoApp), replay.WhoFixes)
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
		"unknown client":    {func(q url.Values) { q.Set("client_id", "client_missing") }, mcpconnectionattempts.CodeUnknownClientID, http.StatusBadRequest},
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
		row := rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeIDPUnavailable)
		require.Equal(t, "server_error", row.SubReason)
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
