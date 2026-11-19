package telemetry_test

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// The single grouped overview query must return what the five separate
// queries it replaced return, for both the current and the previous period.
func TestGetObservabilityOverview_SingleQueryMatchesSeparateQueries(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC().Truncate(time.Minute)
	timeStart := now.Add(-1 * time.Hour)
	timeEnd := now.Add(1 * time.Hour)

	// Current period: three tools with distinct call counts and failure
	// rates, so both top-tools orders are unambiguous, plus chats.
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-50*time.Minute), "tools:http:a:one", 200, 0.2)
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-40*time.Minute), "tools:http:a:one", 200, 0.4)
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-30*time.Minute), "tools:http:a:one", 500, 0.6)
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-20*time.Minute), "tools:http:a:two", 404, 1.0)
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), "tools:http:a:two", 200, 2.0)
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), "tools:http:a:three", 200, 0.1)
	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-45*time.Minute), uuid.New().String(), "success", 90)
	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-15*time.Minute), uuid.New().String(), "failure", 20)

	// Previous period only.
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-2*time.Hour), "tools:http:a:one", 500, 3.0)
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-90*time.Minute), "tools:http:a:four", 200, 0.3)
	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-100*time.Minute), uuid.New().String(), "success", 70)

	// A filter that excludes nothing but forces the raw telemetry_logs path,
	// as every MCP server overview does.
	scope := repo.GetOverviewSummaryParams{
		ExcludedHookSources: []string{"not-a-real-hook-source"},
		GramProjectIDs:      []string{projectID},
		TimeStart:           timeStart.UnixNano(),
		TimeEnd:             timeEnd.UnixNano(),
		User:                repo.UserIdentity{UserIDs: nil, Emails: nil},
		CanonicalUser:       repo.CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""},
		ExternalUserID:      "",
		APIKeyID:            "",
		ToolsetSlug:         "",
		ToolsetSlugServerID: "",
		RemoteMCPServerID:   "",
		MCPServerID:         "",
		MetaMCPServerID:     "",
		EventSource:         "",
		HookSource:          "",
		AccountType:         "",
		ExternalOrgID:       "",
	}
	const intervalSeconds = 900

	got, err := ti.chClient.GetObservabilityOverview(ctx, repo.GetObservabilityOverviewParams{
		Scope:             scope,
		IntervalSeconds:   intervalSeconds,
		IncludeTimeSeries: true,
		ToolLimit:         10,
	})
	require.NoError(t, err)

	wantSummary, err := ti.chClient.GetOverviewSummary(ctx, scope)
	require.NoError(t, err)
	comparisonScope := scope
	comparisonScope.TimeStart = scope.TimeStart - (scope.TimeEnd - scope.TimeStart)
	comparisonScope.TimeEnd = scope.TimeStart
	wantComparison, err := ti.chClient.GetOverviewSummary(ctx, comparisonScope)
	require.NoError(t, err)
	wantSeries, err := ti.chClient.GetTimeSeriesMetrics(ctx, repo.GetTimeSeriesMetricsParams{
		ExcludedHookSources: scope.ExcludedHookSources,
		GramProjectIDs:      scope.GramProjectIDs,
		TimeStart:           scope.TimeStart,
		TimeEnd:             scope.TimeEnd,
		IntervalSeconds:     intervalSeconds,
		User:                scope.User,
		CanonicalUser:       scope.CanonicalUser,
		ExternalUserID:      "",
		APIKeyID:            "",
		ToolsetSlug:         "",
		ToolsetSlugServerID: "",
		RemoteMCPServerID:   "",
		MCPServerID:         "",
		MetaMCPServerID:     "",
		EventSource:         "",
		HookSource:          "",
		AccountType:         "",
		ExternalOrgID:       "",
	})
	require.NoError(t, err)
	breakdown := func(sortBy string) []repo.ToolMetric {
		tools, err := ti.chClient.GetToolMetricsBreakdown(ctx, repo.GetToolMetricsBreakdownParams{
			ExcludedHookSources: scope.ExcludedHookSources,
			GramProjectIDs:      scope.GramProjectIDs,
			TimeStart:           scope.TimeStart,
			TimeEnd:             scope.TimeEnd,
			User:                scope.User,
			CanonicalUser:       scope.CanonicalUser,
			ExternalUserID:      "",
			APIKeyID:            "",
			ToolsetSlug:         "",
			ToolsetSlugServerID: "",
			RemoteMCPServerID:   "",
			MCPServerID:         "",
			MetaMCPServerID:     "",
			EventSource:         "",
			HookSource:          "",
			AccountType:         "",
			ExternalOrgID:       "",
			Limit:               10,
			SortBy:              sortBy,
		})
		require.NoError(t, err)
		return tools
	}

	require.Equal(t, wantSummary, got.Summary)
	require.Equal(t, wantComparison, got.Comparison)
	require.Equal(t, wantSeries, got.TimeSeries)
	requireSameTools(t, breakdown("count"), got.TopToolsByCount)
	requireSameTools(t, breakdown("failure_rate"), got.TopToolsByFailureRate)

	// Guard against a vacuous pass.
	require.Equal(t, uint64(6), got.Summary.TotalToolCalls)
	require.Equal(t, uint64(2), got.Comparison.TotalToolCalls)
	require.Len(t, got.TopToolsByCount, 3)
}

// requireSameTools compares tool lists exactly except for average latency,
// which differs in the last float bit with summation order.
func requireSameTools(t *testing.T, want, got []repo.ToolMetric) {
	t.Helper()
	round := func(tools []repo.ToolMetric) []repo.ToolMetric {
		out := make([]repo.ToolMetric, len(tools))
		for i, tool := range tools {
			tool.AvgLatencyMs = math.Round(tool.AvgLatencyMs*1e6) / 1e6
			out[i] = tool
		}
		return out
	}
	require.Equal(t, round(want), round(got))
}
