package mcp_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
)

func TestBuiltinLogWithSyntheticIdentity(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	endpoint := &mcp.ResolvedMcpEndpoint{BuiltinID: uuid.NullUUID{UUID: id, Valid: true}, ProjectID: uuid.New(), RouteBase: mcproute.Builtin}
	var output bytes.Buffer
	endpoint.LogWith(slog.New(slog.NewJSONHandler(&output, nil))).InfoContext(t.Context(), "built-in request")
	var record map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &record))
	require.Equal(t, id.String(), record[string(attr.BuiltinMcpServerIDKey)])
	require.NotContains(t, record, string(attr.McpServerIDKey), "synthetic IDs must never identify persisted MCP server rows")
}

func TestBuiltinIssuerGateRejectedTelemetry(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"", "mcp.example.com"} {
		t.Run("host="+host, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			requestCtx := contextvalues.SetRequestContext(t.Context(), &contextvalues.RequestContext{Host: host, ReqURL: "https://mcp.example.com/untrusted?nonce=unique"})
			issuer := builtinIssuerFixture(t, ctx, ti, false)
			slug := builtinSlug(t, ctx, ti)
			endpoint := &mcp.ResolvedMcpEndpoint{ProjectID: *ac.ProjectID, OrganizationID: ac.ActiveOrganizationID, RouteBase: mcproute.Builtin, Slug: slug}
			// Configured and issuerless endpoints must contribute to the same series.
			for _, issuerID := range []uuid.UUID{uuid.Nil, issuer.ID} {
				endpoint.UserSessionIssuerID = issuerID
				for _, token := range []string{"", "not-a-session-token"} {
					_, _, _, err := ti.service.ApplyIssuerGate(requestCtx, httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
					require.Error(t, err)
				}
			}
			var metrics metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(t.Context(), &metrics))
			points := map[attribute.Set]int64{}
			for _, scope := range metrics.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name != mcpmetrics.InstrumentMCPRequestRejected {
						continue
					}
					sum, ok := metric.Data.(metricdata.Sum[int64])
					require.True(t, ok)
					for _, point := range sum.DataPoints {
						points[point.Attributes] = point.Value
					}
				}
			}
			require.Len(t, points, 2)
			for _, reason := range []string{"no_credentials", "invalid_bearer_token"} {
				require.Equal(t, int64(2), points[attribute.NewSet(
					attr.McpRejectionReason(mcpmetrics.RequestRejectionReasonAuthentication),
					attr.OAuthFailureReason(reason),
					attr.McpURL(host+"/mcp/builtin/"+slug),
					attr.McpSurface(string(mcpmetrics.SurfacePlatform)),
					attr.NetworkSurface(mcpmetrics.NetworkSurfacePublic),
				)])
			}
		})
	}
}
