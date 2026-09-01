package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestHandleToolsCall_RecordsCoverageWithoutRequestContext(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, "internal-coverage-"+uuid.NewString()[:8])
	_, err := ti.service.HandleToolsCall(context.Background(), &mcp.McpInputs{
		ProjectID: *authCtx.ProjectID,
		Toolset:   toolset.Slug,
		Mode:      mcp.ToolModeStatic,
	}, "missing_tool", json.RawMessage(`{}`))
	require.Error(t, err)

	coveragePoints := collectKillswitchCoverage(t, reader)
	require.Equal(t, int64(1), coveragePoints[attribute.NewSet(
		attr.McpKillswitchSurface(mcpmetrics.KillswitchSurfaceHosted),
		attr.McpKillswitchIdentityClass(mcpmetrics.KillswitchIdentityUnattributed),
		attr.McpKillswitchResourceClass(mcpmetrics.KillswitchResourceLegacyNoServer),
	)])
}

func TestServePublic_RecordsCanonicalServerCoverage(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.features.SetFlag(feature.FlagMCPKillswitchShadow, authCtx.ActiveOrganizationID, true)

	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, "routed-coverage-"+uuid.NewString()[:8])
	endpointSlug := "routed-coverage-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	mcpServer := createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, issuerID)
	userToken := mintIssuerBearerForEndpointSubject(t, ctx, ti, endpointSlug, mcpServer, authCtx.ActiveOrganizationID, urn.NewUserSubject(authCtx.UserID))

	response, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeToolsCallBody("missing_tool"), userToken, nil)
	require.NoError(t, err)
	require.Equal(t, 200, response.Code)

	coveragePoints := collectKillswitchCoverage(t, reader)
	require.Equal(t, int64(1), coveragePoints[attribute.NewSet(
		attr.McpKillswitchSurface(mcpmetrics.KillswitchSurfaceHosted),
		attr.McpKillswitchIdentityClass(mcpmetrics.KillswitchIdentityActiveUser),
		attr.McpKillswitchResourceClass(mcpmetrics.KillswitchResourceCanonicalServer),
	)])
	require.Zero(t, coveragePoints[attribute.NewSet(
		attr.McpKillswitchSurface(mcpmetrics.KillswitchSurfaceHosted),
		attr.McpKillswitchIdentityClass(mcpmetrics.KillswitchIdentityActiveUser),
		attr.McpKillswitchResourceClass(mcpmetrics.KillswitchResourceLegacyNoServer),
	)])
}

func collectKillswitchCoverage(t *testing.T, reader *sdkmetric.ManualReader) map[attribute.Set]int64 {
	t.Helper()

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
	return coveragePoints
}
