package admission

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type capturedReports struct {
	outcomes []ReportOutcome
}

func (r *capturedReports) RecordReport(_ context.Context, outcome ReportOutcome) {
	r.outcomes = append(r.outcomes, outcome)
}

func TestReportMetricsBoundsOutcomes(t *testing.T) {
	t.Parallel()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	reports := NewReportMetrics(provider, testenv.NewLogger(t))
	for _, outcome := range []ReportOutcome{ReportEmptyAudience, ReportNotRequired, ReportCovered, ReportApprovalRequired, ReportInvalidTarget, ReportUnavailable, "unrecognized-value"} {
		reports.RecordReport(t.Context(), outcome)
	}
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))
	require.Len(t, data.ScopeMetrics, 1)
	require.Len(t, data.ScopeMetrics[0].Metrics, 1)
	got := data.ScopeMetrics[0].Metrics[0]
	require.Equal(t, reportEvaluationsMetric, got.Name)
	require.Equal(t, "{evaluation}", got.Unit)
	sum, ok := got.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 6)
	for _, point := range sum.DataPoints {
		require.Equal(t, 1, point.Attributes.Len())
		value, ok := point.Attributes.Value(reportOutcomeAttribute)
		require.True(t, ok)
		if value.AsString() == string(ReportUnavailable) {
			require.EqualValues(t, 2, point.Value)
		} else {
			require.EqualValues(t, 1, point.Value)
		}
	}
}

func TestGuardReportInvalidTargetAndLegacy(t *testing.T) {
	t.Parallel()
	reports := &capturedReports{outcomes: nil}
	guard := NewGuard(nil, reports)
	err := guard.checkURL(t.Context(), nil, RolloutConfig{Mode: ModeReport}, "org", uuid.New(), "://invalid", nil)
	require.NoError(t, err)
	require.Equal(t, []ReportOutcome{ReportInvalidTarget}, reports.outcomes)
	reports.outcomes = nil
	require.NoError(t, guard.checkURL(t.Context(), nil, RolloutConfig{Mode: ModeLegacy}, "org", uuid.New(), "://invalid", nil))
	require.Empty(t, reports.outcomes)
	require.ErrorIs(t, guard.checkURL(t.Context(), nil, RolloutConfig{Mode: ModeReport, DirectRemoteDistributionDisabled: true}, "org", uuid.New(), "://invalid", nil), ErrDistributionDisabled)
	require.Empty(t, reports.outcomes)
}

func TestGuardReportRecordsAdmissionOutcomesWithoutRefusing(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	ctx := t.Context()
	target := "https://mcp.example.test/report"
	desired := []string{"role:developers"}
	reports := &capturedReports{outcomes: nil}
	guard := NewGuard(nil, reports)
	rollout := RolloutConfig{Mode: ModeReport}
	tx := testenv.BeginTx(t, ctx, fixture.conn)
	require.NoError(t, guard.checkURL(ctx, tx, rollout, fixture.orgID, fixture.projectID, target, nil))
	require.NoError(t, guard.checkURL(ctx, tx, rollout, fixture.orgID, fixture.projectID, target, desired))
	require.NoError(t, tx.Commit(ctx))
	seedBlockingPolicy(t, fixture)
	tx = testenv.BeginTx(t, ctx, fixture.conn)
	require.NoError(t, guard.checkURL(ctx, tx, rollout, fixture.orgID, fixture.projectID, target, desired))
	require.NoError(t, tx.Commit(ctx))
	seedDecision(t, fixture, target, "approved", desired)
	tx = testenv.BeginTx(t, ctx, fixture.conn)
	require.NoError(t, guard.checkURL(ctx, tx, rollout, fixture.orgID, fixture.projectID, target, desired))
	require.NoError(t, tx.Rollback(ctx))
	require.NoError(t, guard.checkURL(ctx, tx, rollout, fixture.orgID, fixture.projectID, target, desired))
	require.Equal(t, []ReportOutcome{ReportEmptyAudience, ReportNotRequired, ReportApprovalRequired, ReportCovered, ReportUnavailable}, reports.outcomes)
}
