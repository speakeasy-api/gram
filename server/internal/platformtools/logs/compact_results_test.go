package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// recordingTelemetry returns full results, time series included, and records
// the overview payload so tests can see what the tool asked for.
type recordingTelemetry struct {
	TelemetryService
	overviewPayload *telemetry.GetObservabilityOverviewPayload
}

func (r *recordingTelemetry) GetObservabilityOverview(_ context.Context, payload *telemetry.GetObservabilityOverviewPayload) (*telemetry.GetObservabilityOverviewResult, error) {
	r.overviewPayload = payload
	return &telemetry.GetObservabilityOverviewResult{}, nil
}

func (r *recordingTelemetry) GetToolUsageSummary(context.Context, *telemetry.GetToolUsageSummaryPayload) (*telemetry.GetToolUsageSummaryResult, error) {
	return &telemetry.GetToolUsageSummaryResult{
		Totals:              &telemetry.ToolUsageTotals{},
		Targets:             []*telemetry.ToolUsageTargetSummary{{}},
		TargetToolBreakdown: []*telemetry.ToolUsageTargetToolBreakdownRow{{}},
		TargetTimeSeries:    []*telemetry.ToolUsageTargetTimeSeriesPoint{{}},
		UserTimeSeries:      []*telemetry.ToolUsageUserTimeSeriesPoint{{}},
		UsersByTarget:       []*telemetry.ToolUsageUsersByTargetRow{{}},
		ClientToolBreakdown: []*telemetry.ToolUsageClientToolBreakdownRow{{}},
	}, nil
}

func TestObservabilityOverviewOmitsTimeSeriesUnlessAsked(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]bool{
		`{}`:                           false,
		`{"include_time_series":true}`: true,
	} {
		svc := &recordingTelemetry{}
		var out bytes.Buffer
		require.NoError(t, NewGetObservabilityOverviewTool(svc).Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(input), &out))
		require.Equal(t, want, svc.overviewPayload.IncludeTimeSeries, input)
	}
}

func TestToolUsageSummaryIsCompactUnlessAsked(t *testing.T) {
	t.Parallel()

	call := func(input string) map[string]json.RawMessage {
		var out bytes.Buffer
		require.NoError(t, NewGetToolUsageSummaryTool(&recordingTelemetry{}).Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(input), &out))
		var body map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(out.Bytes(), &body))
		return body
	}

	compact := call(`{}`)
	for _, kept := range []string{"Totals", "Targets", "TargetToolBreakdown"} {
		require.NotEqual(t, "null", string(compact[kept]), kept)
	}
	for _, dropped := range []string{"TargetTimeSeries", "UserTimeSeries", "UsersByTarget", "ClientToolBreakdown"} {
		require.Equal(t, "null", string(compact[dropped]), dropped)
	}

	full := call(`{"include_time_series":true,"include_breakdowns":true}`)
	for _, field := range []string{"TargetTimeSeries", "UserTimeSeries", "UsersByTarget", "ClientToolBreakdown"} {
		require.NotEqual(t, "null", string(full[field]), field)
	}
}

func TestMostFailingToolRowsKeepsTheWorstTools(t *testing.T) {
	t.Parallel()

	rows := []*telemetry.ToolUsageTargetToolBreakdownRow{
		{ToolName: "quiet", EventCount: 500, FailureCount: 0},
		{ToolName: "busy_failing", EventCount: 200, FailureCount: 9},
		{ToolName: "rare_failing", EventCount: 20, FailureCount: 9},
		{ToolName: "flaky", EventCount: 50, FailureCount: 3},
	}
	names := func(rows []*telemetry.ToolUsageTargetToolBreakdownRow) []string {
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, row.ToolName)
		}
		return out
	}

	require.Equal(t, []string{"busy_failing", "rare_failing", "flaky"}, names(mostFailingToolRows(rows, 3)))
	require.Equal(t, names(rows), names(mostFailingToolRows(rows, 10)), "short lists are returned as they are")
}
