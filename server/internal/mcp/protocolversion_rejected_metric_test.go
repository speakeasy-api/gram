package mcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServePublic_UnsupportedVersionCountsRejectionWithoutDispatch(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, "unsupported-version-metric")
	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, toolsListBody(), "", map[string]string{
		mcpversions.HTTPHeader: mcpversions.Version20260728,
	})
	require.NoError(t, err)
	requireUnsupportedProtocolVersionResponse(t, w, mcpversions.Version20260728, mcpversions.SupportedHostedToolset())

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	var rejected, dispatched int64
	for _, scope := range rm.ScopeMetrics {
		for _, observed := range scope.Metrics {
			sum, ok := observed.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				switch observed.Name {
				case mcpmetrics.InstrumentMCPProtocolVersionRejected:
					rejected += point.Value
				case mcpmetrics.InstrumentMCPRequest:
					dispatched += point.Value
				}
			}
		}
	}
	require.Equal(t, int64(1), rejected)
	require.Zero(t, dispatched, "a version-rejected request must not enter the dispatch census")
}
