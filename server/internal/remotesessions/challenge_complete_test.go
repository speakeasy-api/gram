package remotesessions_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// CompleteRemoteLogin returns the committed grant addressed the way the consent page is, alongside the redirect.
func TestCompleteRemoteLogin_ReturnsTheCommittedGrant(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "set", &spy)

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, fx.parent, fx.clients[0])
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?code=fake-code&state="+url.QueryEscape(parsed.Query().Get("state")), nil)
	result, err := fx.mgr.CompleteRemoteLogin(req.WithContext(ctx))
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)

	require.Contains(t, result.RedirectURL, "/mcp/"+fx.parent.McpSlug+"/connect?state="+url.QueryEscape(fx.parent.ID))
	require.NotNil(t, result.Grant)
	require.Equal(t, fx.parent.ID, result.Grant.ParentChallengeID)
	require.Equal(t, fx.parent.UserSessionIssuerID, result.Grant.UserSessionIssuerID)
	require.Equal(t, fx.clients[0].ID, result.Grant.RemoteSessionClientID)
	require.Equal(t, *fx.parent.Subject, result.Grant.Subject)
}

// A login started from a gateway server's connect page returns to that page.
func TestCompleteRemoteLogin_ReturnsToTheGatewayServerPage(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "set", &spy)
	parent := fx.parent
	parent.ConsentServer = "eng.github"

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?code=fake-code&state="+url.QueryEscape(parsed.Query().Get("state")), nil)
	result, err := fx.mgr.CompleteRemoteLogin(req.WithContext(ctx))
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)

	require.True(t, strings.HasSuffix(result.RedirectURL, "/mcp/"+parent.McpSlug+"/connect?state="+url.QueryEscape(parent.ID)+"&server=eng.github"), result.RedirectURL)
}

// An authorization server with no authorization endpoint can't be signed in to.
func TestBuildAuthorizationUrl_NoAuthorizationEndpoint(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "noauthz", &spy)
	client := fx.clients[0]
	client.AuthorizationEndpoint = ""

	_, err := fx.mgr.BuildAuthorizationUrl(ctx, fx.parent, client)
	require.ErrorIs(t, err, remotesessions.ErrIssuerNoAuthorizationEndpoint)
}
