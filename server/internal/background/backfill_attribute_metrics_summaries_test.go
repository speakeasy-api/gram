package background

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

const testBackfillProjectID = "0f9d5a3f-6a2f-4a4b-9c3e-2b1a0d8e7c6d"

// registerBackfillActivityStubs wires happy-path stubs for every backfill
// activity and returns a recorder of the invocation order plus the staged
// chunk windows.
type backfillActivityRecorder struct {
	calls         []string
	stagedWindows [][2]int64
	cleanupCount  int
}

func registerBackfillActivityStubs(env *testsuite.TestWorkflowEnvironment, rec *backfillActivityRecorder, rawWindow [2]int64, rawRowCount uint64) {
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.PrepareAttributeMetricsBackfillParams) (*activities.PrepareAttributeMetricsBackfillResult, error) {
			rec.calls = append(rec.calls, "prepare")
			return &activities.PrepareAttributeMetricsBackfillResult{
				RawRowCount:     rawRowCount,
				MinTimeUnixNano: rawWindow[0],
				MaxTimeUnixNano: rawWindow[1],
			}, nil
		},
		activity.RegisterOptions{Name: "PrepareAttributeMetricsBackfill"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.StageAttributeMetricsBackfillChunkParams) error {
			rec.calls = append(rec.calls, "stage")
			rec.stagedWindows = append(rec.stagedWindows, [2]int64{params.FromUnixNano, params.ToUnixNano})
			return nil
		},
		activity.RegisterOptions{Name: "StageAttributeMetricsBackfillChunk"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.ValidateAttributeMetricsBackfillParams) (*activities.ValidateAttributeMetricsBackfillResult, error) {
			rec.calls = append(rec.calls, "validate")
			return &activities.ValidateAttributeMetricsBackfillResult{
				Staging: activities.AttributeMetricsBackfillTableStats{
					RowCount:                 10,
					MinTimeBucketUnixSec:     rawWindow[0] / int64(time.Second),
					MaxTimeBucketUnixSec:     rawWindow[1] / int64(time.Second),
					TotalCost:                12.5,
					TotalInputTokens:         100,
					TotalOutputTokens:        50,
					TotalTokens:              150,
					CacheReadInputTokens:     0,
					CacheCreationInputTokens: 0,
					TotalToolCalls:           3,
					UniqueToolCalls:          3,
					TotalChats:               2,
				},
				Live: activities.AttributeMetricsBackfillTableStats{
					RowCount:                 8,
					MinTimeBucketUnixSec:     rawWindow[0] / int64(time.Second),
					MaxTimeBucketUnixSec:     rawWindow[1] / int64(time.Second),
					TotalCost:                12.5,
					TotalInputTokens:         100,
					TotalOutputTokens:        50,
					TotalTokens:              150,
					CacheReadInputTokens:     0,
					CacheCreationInputTokens: 0,
					TotalToolCalls:           3,
					UniqueToolCalls:          3,
					TotalChats:               2,
				},
			}, nil
		},
		activity.RegisterOptions{Name: "ValidateAttributeMetricsBackfill"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.ArchiveAttributeMetricsBackfillParams) (*activities.ArchiveAttributeMetricsBackfillResult, error) {
			rec.calls = append(rec.calls, "archive")
			return &activities.ArchiveAttributeMetricsBackfillResult{
				ArchivedRowCount:         8,
				DeleteWindowStartUnixSec: rawWindow[0] / int64(time.Second),
			}, nil
		},
		activity.RegisterOptions{Name: "ArchiveAttributeMetricsBackfill"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.CommitAttributeMetricsBackfillParams) (*activities.CommitAttributeMetricsBackfillResult, error) {
			rec.calls = append(rec.calls, "commit")
			return &activities.CommitAttributeMetricsBackfillResult{
				DeleteWindowStartUnixSec: rawWindow[0] / int64(time.Second),
				InsertedRowCount:         10,
			}, nil
		},
		activity.RegisterOptions{Name: "CommitAttributeMetricsBackfill"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.CleanupAttributeMetricsBackfillParams) error {
			rec.calls = append(rec.calls, "cleanup")
			rec.cleanupCount++
			return nil
		},
		activity.RegisterOptions{Name: "CleanupAttributeMetricsBackfill"},
	)
}

func TestBackfillAttributeMetricsSummariesWorkflow_CommitsOnApproval(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	now := env.Now().UTC()
	rawStart := now.Add(-36 * time.Hour)
	rec := &backfillActivityRecorder{calls: nil, stagedWindows: nil, cleanupCount: 0}
	registerBackfillActivityStubs(env, rec, [2]int64{rawStart.UnixNano(), now.Add(-time.Hour).UnixNano()}, 500)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalBackfillAttributeMetricsDecision, BackfillAttributeMetricsDecision{
			Approve: true,
			Reason:  "",
		})
	}, time.Minute)

	env.ExecuteWorkflow(BackfillAttributeMetricsSummariesWorkflow, BackfillAttributeMetricsSummariesParams{
		ProjectID: testBackfillProjectID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result BackfillAttributeMetricsSummariesResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.Committed)
	require.Equal(t, uint64(500), result.RawRowCount)
	require.Equal(t, uint64(8), result.ArchivedRowCount)
	require.Equal(t, uint64(10), result.InsertedRowCount)
	require.NotNil(t, result.Report)

	// Prepare first, staging chunks, validate, then the gated commit sequence.
	require.Equal(t, "prepare", rec.calls[0])
	require.Equal(t, []string{"validate", "archive", "commit", "cleanup"}, rec.calls[len(rec.calls)-4:])

	// A ~36h window staged in day chunks: 2-3 chunks depending on alignment,
	// contiguous, ending exactly at the hour-aligned boundary.
	require.NotEmpty(t, rec.stagedWindows)
	boundary := result.BoundaryUnixNano
	require.Equal(t, boundary, rec.stagedWindows[len(rec.stagedWindows)-1][1])
	require.LessOrEqual(t, rec.stagedWindows[0][0], rawStart.UnixNano())
	for i := 1; i < len(rec.stagedWindows); i++ {
		require.Equal(t, rec.stagedWindows[i-1][1], rec.stagedWindows[i][0])
	}
}

func TestBackfillAttributeMetricsSummariesWorkflow_AbortsOnRejection(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	now := env.Now().UTC()
	rec := &backfillActivityRecorder{calls: nil, stagedWindows: nil, cleanupCount: 0}
	registerBackfillActivityStubs(env, rec, [2]int64{now.Add(-6 * time.Hour).UnixNano(), now.Add(-time.Hour).UnixNano()}, 42)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalBackfillAttributeMetricsDecision, BackfillAttributeMetricsDecision{
			Approve: false,
			Reason:  "staging totals diverged",
		})
	}, time.Minute)

	env.ExecuteWorkflow(BackfillAttributeMetricsSummariesWorkflow, BackfillAttributeMetricsSummariesParams{
		ProjectID: testBackfillProjectID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result BackfillAttributeMetricsSummariesResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.False(t, result.Committed)
	require.Equal(t, "staging totals diverged", result.AbortReason)
	require.NotContains(t, rec.calls, "archive")
	require.NotContains(t, rec.calls, "commit")
	require.Equal(t, 1, rec.cleanupCount)
}

func TestBackfillAttributeMetricsSummariesWorkflow_NoRawLogs(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	rec := &backfillActivityRecorder{calls: nil, stagedWindows: nil, cleanupCount: 0}
	registerBackfillActivityStubs(env, rec, [2]int64{0, 0}, 0)

	env.ExecuteWorkflow(BackfillAttributeMetricsSummariesWorkflow, BackfillAttributeMetricsSummariesParams{
		ProjectID: testBackfillProjectID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result BackfillAttributeMetricsSummariesResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.False(t, result.Committed)
	require.Equal(t, []string{"prepare"}, rec.calls)
}

func TestBackfillAttributeMetricsSummariesWorkflow_ExposesValidationReportQuery(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	now := env.Now().UTC()
	rec := &backfillActivityRecorder{calls: nil, stagedWindows: nil, cleanupCount: 0}
	registerBackfillActivityStubs(env, rec, [2]int64{now.Add(-2 * time.Hour).UnixNano(), now.Add(-time.Hour).UnixNano()}, 42)

	env.RegisterDelayedCallback(func() {
		value, err := env.QueryWorkflow(QueryBackfillAttributeMetricsValidationReport)
		require.NoError(t, err)
		var report *activities.ValidateAttributeMetricsBackfillResult
		require.NoError(t, value.Get(&report))
		require.NotNil(t, report)
		require.Equal(t, uint64(10), report.Staging.RowCount)

		env.SignalWorkflow(SignalBackfillAttributeMetricsDecision, BackfillAttributeMetricsDecision{
			Approve: true,
			Reason:  "",
		})
	}, time.Minute)

	env.ExecuteWorkflow(BackfillAttributeMetricsSummariesWorkflow, BackfillAttributeMetricsSummariesParams{
		ProjectID: testBackfillProjectID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}
