package remotesessions_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// A consent flow minted on an extra platform host (GRAM_PLATFORM_HOSTS)
// returns there after the upstream login instead of to the server URL, where
// consent would reject the state for its origin.
func TestCompleteRemoteLogin_ReturnsToMintPlatformHost(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "platform-host", &spy)

	parent := fx.parent
	parent.Authority = networkingress.Authority{
		Surface:          requestorigin.SurfacePlatform,
		BaseURL:          "https://ai.example.com",
		OrganizationID:   fx.parent.OrganizationID,
		NetworkIngressID: uuid.Nil,
		NamespaceKind:    networkingress.NamespacePlatform,
		CustomDomainID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?code=fake-code&state="+url.QueryEscape(parsed.Query().Get("state")), nil)
	result, err := fx.mgr.CompleteRemoteLogin(req.WithContext(ctx))
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)

	require.Equal(t, "https://ai.example.com/mcp/"+parent.McpSlug+"/connect?state="+url.QueryEscape(parent.ID), result.RedirectURL)
}
