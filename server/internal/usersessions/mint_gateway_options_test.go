package usersessions_test

import (
	"testing"

	sessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func TestMintGatewayDiscoveryOverride(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	gateway, issuerID := createIssuerGatedMintMetaServer(t, ctx, ti, "mint-mode")
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPConnect, gateway.ID.String()))
	payload := &sessionsgen.MintUserSessionPayload{MetaMcpServerID: conv.PtrEmpty(gateway.ID.String()), DiscoveryMode: conv.PtrEmpty("direct")}
	_, err := ti.service.MintUserSession(ctx, payload)
	require.ErrorContains(t, err, "not available")
	ti.features.SetFlag(feature.FlagGatewayDiscoveryModes, authCtx.ActiveOrganizationID, true)
	got, err := ti.service.MintUserSession(ctx, payload)
	require.NoError(t, err)
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(got.AccessToken, urn.NewUserSessionIssuer(issuerID).String())
	require.NoError(t, err)
	row, err := repo.New(ti.conn).GetUserSessionByJTI(ctx, repo.GetUserSessionByJTIParams{UserSessionIssuerID: issuerID, Jti: claims.ID})
	require.NoError(t, err)
	policy, err := toolfilter.ParseSessionPolicy(row.ToolSelection)
	require.NoError(t, err)
	require.Equal(t, "meta_mcp_server:"+gateway.ID.String(), policy.Resource)
	require.Equal(t, metamcp.DiscoveryModeDirect, *policy.Gateway.DiscoveryMode)
	require.Nil(t, policy.Selection)

	payload.DiscoveryMode = nil
	ti.features.SetFlag(feature.FlagGatewayDiscoveryModes, authCtx.ActiveOrganizationID, false)
	got, err = ti.service.MintUserSession(ctx, payload)
	require.NoError(t, err)
	claims, err = usersessions.NewSigner("test-jwt-secret").Validate(got.AccessToken, urn.NewUserSessionIssuer(issuerID).String())
	require.NoError(t, err)
	row, err = repo.New(ti.conn).GetUserSessionByJTI(ctx, repo.GetUserSessionByJTIParams{UserSessionIssuerID: issuerID, Jti: claims.ID})
	require.NoError(t, err)
	require.Empty(t, row.ToolSelection, "the gateway default must remain live, not copied into a session")
}
