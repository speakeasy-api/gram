package metamcp_test

import (
	"encoding/json"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/stretchr/testify/require"
)

func TestGatewayDiscoverySettingsRolloutAndAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	gateway, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{Name: "Discovery gateway"})
	require.NoError(t, err)
	require.Equal(t, "progressive", gateway.DiscoveryMode)
	update := &gen.UpdateMetaMcpServerPayload{ID: gateway.ID, Name: gateway.Name, DiscoveryMode: conv.PtrEmpty("direct")}
	_, err = ti.service.UpdateMetaMcpServer(ctx, update)
	require.ErrorContains(t, err, "not available")
	ti.features.SetFlag(feature.FlagGatewayDiscoveryModes, authCtx.ActiveOrganizationID, true)
	gateway, err = ti.service.UpdateMetaMcpServer(ctx, update)
	require.NoError(t, err)
	require.Equal(t, "direct", gateway.DiscoveryMode)
	event, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMetaMcpServerUpdate)
	require.NoError(t, err)
	var after map[string]any
	require.NoError(t, json.Unmarshal(event.AfterSnapshot, &after))
	require.Equal(t, "direct", after["DiscoveryMode"])
	ti.features.SetFlag(feature.FlagGatewayDiscoveryModes, authCtx.ActiveOrganizationID, false)
	gateway, err = ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{ID: gateway.ID, Name: "Renamed gateway"})
	require.NoError(t, err)
	require.Equal(t, "direct", gateway.DiscoveryMode, "unrelated writes preserve mode while rollout is disabled")
}
