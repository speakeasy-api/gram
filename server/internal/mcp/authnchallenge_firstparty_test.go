package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestFirstPartyConnect_PreservesDeviceAgentLoopbackRedirect(t *testing.T) {
	t.Parallel()

	idpURL, err := url.Parse("https://idp.example.com/authorize")
	require.NoError(t, err)
	identityResolver := &mockIdentityResolver{buildAuthURLResult: idpURL}
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, identityResolver)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	const callbackURI = "http://localhost:49152/callback"
	req := httptest.NewRequest(
		http.MethodGet,
		"/mcp/"+toolset.McpSlug.String+"/connect/first-party?remote_redirect_uri="+url.QueryEscape(callbackURI),
		nil,
	)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()

	require.NoError(t, ti.service.HandleFirstPartyConnect(w, req))
	require.Equal(t, http.StatusFound, w.Code)
	require.NotNil(t, identityResolver.buildAuthURLParams)
	require.NotEmpty(t, identityResolver.buildAuthURLParams.State)

	challenge, err := ti.authnChallengeCache.Get(
		ctx,
		"authnChallenge:"+identityResolver.buildAuthURLParams.State,
	)
	require.NoError(t, err)
	require.True(t, challenge.FirstParty)
	require.Equal(t, callbackURI, challenge.RemoteOAuthRedirectURI)
}

func TestFirstPartyConnect_RejectsNonLoopbackRedirect(t *testing.T) {
	t.Parallel()

	idpURL, err := url.Parse("https://idp.example.com/authorize")
	require.NoError(t, err)
	identityResolver := &mockIdentityResolver{buildAuthURLResult: idpURL}
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, identityResolver)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	req := httptest.NewRequest(
		http.MethodGet,
		"/mcp/"+toolset.McpSlug.String+"/connect/first-party?remote_redirect_uri="+
			url.QueryEscape("http://attacker.example.com:49152/callback"),
		nil,
	)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()

	err = ti.service.HandleFirstPartyConnect(w, req)
	require.ErrorContains(t, err, "invalid remote_redirect_uri")
	require.Nil(t, identityResolver.buildAuthURLParams)
}

func TestFirstPartyConnect_HostedRedirectRemainsDefault(t *testing.T) {
	t.Parallel()

	idpURL, err := url.Parse("https://idp.example.com/authorize")
	require.NoError(t, err)
	identityResolver := &mockIdentityResolver{buildAuthURLResult: idpURL}
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, identityResolver)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	req := httptest.NewRequest(
		http.MethodGet,
		"/mcp/"+toolset.McpSlug.String+"/connect/first-party",
		nil,
	)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()

	require.NoError(t, ti.service.HandleFirstPartyConnect(w, req))
	require.NotNil(t, identityResolver.buildAuthURLParams)
	challenge, err := ti.authnChallengeCache.Get(
		ctx,
		"authnChallenge:"+identityResolver.buildAuthURLParams.State,
	)
	require.NoError(t, err)
	require.Empty(t, challenge.RemoteOAuthRedirectURI)
	require.True(t, challenge.FirstParty)
}
