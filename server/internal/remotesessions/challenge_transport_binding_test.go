package remotesessions_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRemoteLoginCallbackTunnelSnapshotFailsClosedAfterBindingCleared(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "", "transport-tunnel-to-direct", &spy)

	tunnelID := seedTunneledMcpServer(t, ctx, fx.ti)
	_, err := fx.ti.service.UpdateRemoteSessionIssuer(withAdmin(t, ctx), &gen.UpdateRemoteSessionIssuerPayload{
		ID:                  fx.issuerID.String(),
		TunneledMcpServerID: conv.PtrEmpty(tunnelID.String()),
	})
	require.NoError(t, err)

	clients, err := fx.mgr.ListClients(ctx, fx.parent.ProjectID, fx.parent.OrganizationID, fx.userSessionIssuerID)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	require.True(t, clients[0].TunneledMcpServerID.Valid)
	require.Equal(t, tunnelID, clients[0].TunneledMcpServerID.UUID)
	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, fx.parent, clients[0])
	require.NoError(t, err)

	empty := ""
	_, err = fx.ti.service.UpdateRemoteSessionIssuer(withAdmin(t, ctx), &gen.UpdateRemoteSessionIssuerPayload{
		ID:                  fx.issuerID.String(),
		TunneledMcpServerID: &empty,
	})
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.NotEmpty(t, parsed.Query().Get("state"))
	req := httptest.NewRequest(http.MethodGet,
		"/mcp/remote_login_callback?code=fake-code&state="+url.QueryEscape(parsed.Query().Get("state")), nil)
	req = req.WithContext(ctx)
	err = fx.mgr.HandleRemoteLoginCallback(httptest.NewRecorder(), req)
	requireOopsCode(t, err, oops.CodeUnauthorized)
	require.Nil(t, spy.form, "the callback must not fall back to direct egress")
}

func TestRemoteLoginCallbackDirectSnapshotIgnoresTunnelAddedMidFlow(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "", "transport-direct-to-tunnel", &spy)
	require.False(t, fx.clients[0].TunneledMcpServerID.Valid)
	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, fx.parent, fx.clients[0])
	require.NoError(t, err)

	tunnelID := seedTunneledMcpServer(t, ctx, fx.ti)
	_, err = fx.ti.service.UpdateRemoteSessionIssuer(withAdmin(t, ctx), &gen.UpdateRemoteSessionIssuerPayload{
		ID:                  fx.issuerID.String(),
		TunneledMcpServerID: conv.PtrEmpty(tunnelID.String()),
	})
	require.NoError(t, err)

	runCallback(t, ctx, fx, authURL)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "authorization_code", spy.form.Get("grant_type"), "the callback must keep using direct egress")
}
