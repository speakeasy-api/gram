package metamcp_test

import (
	"encoding/json"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/stretchr/testify/require"
)

func TestGatewayDiscoverySettingsProductFeatureAndAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	gateway, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{Name: "Discovery gateway"})
	require.NoError(t, err)
	require.Equal(t, "progressive", gateway.DiscoveryMode)
	update := &gen.UpdateMetaMcpServerPayload{ID: gateway.ID, Name: gateway.Name, DiscoveryMode: conv.PtrEmpty("direct")}
	_, err = ti.service.UpdateMetaMcpServer(ctx, update)
	require.ErrorContains(t, err, "not available")
	require.NoError(t, ti.productFeatures.SetFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureGatewayDiscoveryModes, true))
	gateway, err = ti.service.UpdateMetaMcpServer(ctx, update)
	require.NoError(t, err)
	require.Equal(t, "direct", gateway.DiscoveryMode)
	event, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMetaMcpServerUpdate)
	require.NoError(t, err)
	var after map[string]any
	require.NoError(t, json.Unmarshal(event.AfterSnapshot, &after))
	require.Equal(t, "direct", after["DiscoveryMode"])
	require.NoError(t, ti.productFeatures.SetFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureGatewayDiscoveryModes, false))
	gateway, err = ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{ID: gateway.ID, Name: "Renamed gateway"})
	require.NoError(t, err)
	require.Equal(t, "direct", gateway.DiscoveryMode, "unrelated writes preserve mode while the product feature is disabled")
}

func TestGatewayCapabilitiesDoNotRequireOrganizationRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	gateway, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{Name: "Project editor gateway"})
	require.NoError(t, err)
	require.NoError(t, ti.productFeatures.SetFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureGatewayDiscoveryModes, true))
	ctx = withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, authCtx.ProjectID.String()), authz.NewGrant(authz.ScopeMCPWrite, authCtx.ProjectID.String()))
	got, err := ti.service.GetMetaMcpServer(ctx, &gen.GetMetaMcpServerPayload{ID: gateway.ID})
	require.NoError(t, err)
	require.NotNil(t, got.DiscoveryModesEnabled)
	require.True(t, *got.DiscoveryModesEnabled)
	listed, err := ti.service.ListMetaMcpServers(ctx, &gen.ListMetaMcpServersPayload{})
	require.NoError(t, err)
	require.Len(t, listed.MetaMcpServers, 1)
	require.Equal(t, got.DiscoveryModesEnabled, listed.MetaMcpServers[0].DiscoveryModesEnabled)
	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{ID: gateway.ID, Name: gateway.Name, DiscoveryMode: conv.PtrEmpty("direct")})
	require.NoError(t, err)
	require.NotNil(t, updated.DiscoveryModesEnabled)
	require.True(t, *updated.DiscoveryModesEnabled)
	require.Equal(t, "direct", updated.DiscoveryMode)
}
