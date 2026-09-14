package remotesessions_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
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
