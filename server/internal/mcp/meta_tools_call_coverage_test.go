package mcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
)

func TestMetaHostedMemberToolsCall_DoesNotReportHostedCoverage(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "meta-coverage-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")
	callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{
		"name":      member.slug + "--alpha_tool",
		"arguments": map[string]any{},
	})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			require.NotEqual(t, mcpmetrics.InstrumentMCPToolCallKillswitchIdentity, metric.Name,
				"meta member dispatch must not be represented as hosted mcp_tool_execution coverage")
		}
	}
}
