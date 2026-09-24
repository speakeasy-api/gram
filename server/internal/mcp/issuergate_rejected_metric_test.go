package mcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
)

// The issuer gate counts every request it turns away on mcp.request.rejected,
// keyed by the endpoint's route and slug rather than the raw request URL, and
// tells the unauthenticated handshake probe apart from a presented-but-bad
// bearer token. Both populations 401 identically on the wire, so the counter
// is the only place they are distinguishable.
func TestServePublic_McpEndpoint_IssuerGated_RecordsRejectedRequests(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	endpointSlug := "endpoint-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", endpointSlug, "public", issuerID)

	_, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), "", nil)
	require.Error(t, err, "issuer-gated endpoint must reject the unauthenticated probe")

	_, err = servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), "not-a-session-token", nil)
	require.Error(t, err, "issuer-gated endpoint must reject a malformed bearer token")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	points := map[attribute.Set]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != mcpmetrics.InstrumentMCPRequestRejected {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "rejected instrument must be an int64 counter")
			for _, dp := range sum.DataPoints {
				points[dp.Attributes] = dp.Value
			}
		}
	}
	require.Len(t, points, 2, "the probe and the bad token must land in distinct series")

	// The test request carries no request context, so the host segment is
	// empty and the value is the route and slug alone.
	mcpURL := "/mcp/" + endpointSlug
	require.Equal(t, int64(1), points[attribute.NewSet(
		attr.OAuthFailureReason("no_credentials"),
		attr.McpURL(mcpURL),
		attr.McpSurface(string(mcpmetrics.SurfaceHosting)),
		attr.NetworkSurface(mcpmetrics.NetworkSurfacePublic),
	)])
	require.Equal(t, int64(1), points[attribute.NewSet(
		attr.OAuthFailureReason("invalid_bearer_token"),
		attr.McpURL(mcpURL),
		attr.McpSurface(string(mcpmetrics.SurfaceHosting)),
		attr.NetworkSurface(mcpmetrics.NetworkSurfacePublic),
	)])
}
