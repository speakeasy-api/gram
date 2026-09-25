package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

const mcpNetworkRequestEventURN = "urn:telemetry:gram_service:log:mcp_network_request"

// logNetworkRequest writes one row the way the MCP serve path records an
// inbound request: no tool URN, just the server id and network surface. The
// summary buckets by observed time, which the serve path sets to request time.
func logNetworkRequest(t *testing.T, ctx context.Context, ti *testInstance, projectID string, serverKey attr.Key, serverID, surface string, at time.Time) {
	t.Helper()

	require.NoError(t, ti.telemLogger.LogSyncForTest(ctx, telemetry.WithOTELMetadata(telemetry.LogParams{
		Timestamp: at,
		ToolInfo: telemetry.ToolInfo{
			ID:             "",
			URN:            "",
			Name:           "",
			ProjectID:      projectID,
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: ti.orgID,
		},
		UserInfo: telemetry.UserInfoByID(""),
		Attributes: telemetry.HTTPLogAttributes{
			attr.EventURNKey:       mcpNetworkRequestEventURN,
			attr.NetworkSurfaceKey: surface,
			serverKey:              serverID,
		},
	}, at, nil)))
}

func TestGetMcpNetworkTraffic_CountsPublicAndPrivatePerServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	server := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{name: "Traffic", slug: "traffic-" + uuid.NewString()[:8]})
	other := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{name: "Other", slug: "other-" + uuid.NewString()[:8]})
	serverID := server.mcpServerID.String()

	logNetworkRequest(t, ctx, ti, ti.projectID, attr.McpServerIDKey, serverID, "public", now)
	logNetworkRequest(t, ctx, ti, ti.projectID, attr.McpServerIDKey, serverID, "public", now)
	logNetworkRequest(t, ctx, ti, ti.projectID, attr.McpServerIDKey, serverID, "private", now)
	// Another server, another project, and a row older than the window: none may count.
	logNetworkRequest(t, ctx, ti, ti.projectID, attr.McpServerIDKey, other.mcpServerID.String(), "public", now)
	logNetworkRequest(t, ctx, ti, uuid.NewString(), attr.McpServerIDKey, serverID, "public", now)
	logNetworkRequest(t, ctx, ti, ti.projectID, attr.McpServerIDKey, serverID, "private", now.Add(-48*time.Hour))

	result, err := ti.service.GetMcpNetworkTraffic(ctx, &gen.GetMcpNetworkTrafficPayload{
		McpServerID: &serverID,
		Window:      "24h",
	})
	require.NoError(t, err)

	require.Len(t, result.Points, 24)
	var publicTotal, privateTotal int64
	for _, point := range result.Points {
		publicTotal += point.PublicRequests
		privateTotal += point.PrivateRequests
	}
	require.Equal(t, int64(2), publicTotal)
	require.Equal(t, int64(1), privateTotal)
	require.NotNil(t, result.LastPublicAt)
	require.NotNil(t, result.LastPrivateAt)
}

func TestGetMcpNetworkTraffic_Gateway(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	gateway := createGateway(t, ctx, ti, "Traffic Gateway")
	gatewayID := gateway.ID.String()

	logNetworkRequest(t, ctx, ti, ti.projectID, attr.MetaMcpServerIDKey, gatewayID, "private", now)

	result, err := ti.service.GetMcpNetworkTraffic(ctx, &gen.GetMcpNetworkTrafficPayload{
		MetaMcpServerID: &gatewayID,
		Window:          "7d",
	})
	require.NoError(t, err)
	require.Len(t, result.Points, 7*24)
	require.Equal(t, int64(1), result.Points[len(result.Points)-1].PrivateRequests)
	require.Nil(t, result.LastPublicAt)
}

func TestGetMcpNetworkTraffic_RejectsInvalidScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	server := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{name: "Scoped", slug: "scoped-" + uuid.NewString()[:8]})
	serverID := server.mcpServerID.String()
	unknownID := uuid.NewString()

	_, err := ti.service.GetMcpNetworkTraffic(ctx, &gen.GetMcpNetworkTrafficPayload{Window: "24h"})
	require.Error(t, err, "one server id is required")

	_, err = ti.service.GetMcpNetworkTraffic(ctx, &gen.GetMcpNetworkTrafficPayload{McpServerID: &serverID, MetaMcpServerID: &unknownID, Window: "24h"})
	require.Error(t, err, "both ids are ambiguous")

	_, err = ti.service.GetMcpNetworkTraffic(ctx, &gen.GetMcpNetworkTrafficPayload{McpServerID: &unknownID, Window: "24h"})
	require.Error(t, err, "server outside the project is not found")
}
