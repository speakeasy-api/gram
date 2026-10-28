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
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/stretchr/testify/require"
)

func TestPostLoginDelegationRejectsChangedGrantAndReplay(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	state.FirstParty = true
	state.PendingDelegation = &remotesessions.RemoteGrant{Subject: *state.Subject, UserSessionIssuerID: state.UserSessionIssuerID, RemoteSessionID: uuid.New(), GrantGeneration: 7}
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	var response *httptest.ResponseRecorder
	post := func(action, session, generation, csrf string) error {
		form := url.Values{"state": {fx.stateID}, "csrf_token": {csrf}, "action": {action}, "remote_session_id": {session}, "grant_generation": {generation}, "browser_redirect": {"1"}}
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.toolset.McpSlug.String+"/connect/remote-session", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		route := chi.NewRouteContext()
		route.URLParams.Add("mcpSlug", fx.toolset.McpSlug.String)
		req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
		response = httptest.NewRecorder()
		return ti.service.HandleConsentAction(response, req)
	}
	require.ErrorContains(t, post("delegate_grant", state.PendingDelegation.RemoteSessionID.String(), "7", "incorrect"), "csrf")
	require.ErrorContains(t, post("delegate_grant", uuid.NewString(), "7", fx.csrf), "connection changed")
	require.ErrorContains(t, post("delegate_grant", state.PendingDelegation.RemoteSessionID.String(), "8", fx.csrf), "connection changed")
	require.NoError(t, post("finish_delegation", state.PendingDelegation.RemoteSessionID.String(), "7", fx.csrf))
	require.Equal(t, http.StatusSeeOther, response.Code)
	require.Contains(t, response.Header().Get("Location"), "?state="+fx.stateID)
	finished, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	require.Nil(t, finished.PendingDelegation)
	require.Error(t, post("delegate_grant", state.PendingDelegation.RemoteSessionID.String(), "7", fx.csrf))
	require.Error(t, post("finish_delegation", state.PendingDelegation.RemoteSessionID.String(), "7", fx.csrf))
}
