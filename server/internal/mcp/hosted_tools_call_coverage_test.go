package mcp_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestHostedMalformedToolsCall_RecordsCoverageAtMethodBoundary(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	ctx = mcpidentity.WithIdentity(ctx, mcpidentity.AuthenticatedUser(authCtx.UserID))
	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, "hosted-coverage-toolset-"+uuid.NewString()[:8])
	endpointSlug := "hosted-coverage-endpoint-" + uuid.NewString()[:8]
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, uuid.Nil)

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  "malformed params",
	})
	require.NoError(t, err)
	_, err = servePublicHTTP(t, ctx, ti, endpointSlug, body, "", nil)
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	coveragePoints := map[attribute.Set]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != mcpmetrics.InstrumentMCPToolCallKillswitchIdentity {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				coveragePoints[point.Attributes] = point.Value
			}
		}
	}
	require.Equal(t, map[attribute.Set]int64{
		attribute.NewSet(
			attr.McpKillswitchSurface(mcpmetrics.KillswitchSurfaceHosted),
			attr.McpKillswitchIdentityClass(mcpmetrics.KillswitchIdentityActiveUser),
			attr.McpKillswitchResourceClass(mcpmetrics.KillswitchResourceCanonicalServer),
		): 1,
	}, coveragePoints)
}
