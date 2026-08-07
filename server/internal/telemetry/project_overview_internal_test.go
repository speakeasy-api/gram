package telemetry

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

func TestFetchProjectOverviewClickHouseRunsSessionQueriesConcurrently(t *testing.T) {
	t.Parallel()

	reader := newBarrierProjectOverviewReader(4)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	result, err := fetchProjectOverviewClickHouse(ctx, reader, projectOverviewClickHouseParams{
		projectID:       "00000000-0000-0000-0000-000000000001",
		timeStart:       200,
		timeEnd:         300,
		comparisonStart: 100,
		comparisonEnd:   200,
		sessionMode:     true,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(11), result.toolMetrics.TotalToolCalls)
	require.Equal(t, uint64(7), result.toolMetricsComparison.TotalToolCalls)
	require.Equal(t, uint64(3), result.activeCounts.ActiveServersCount)
	require.Equal(t, uint64(4), result.activeCounts.ActiveUsersCount)
	require.Equal(t, []repo.TopServer{{ServerName: "server", ToolCallCount: 5}}, result.topServers)
	require.Empty(t, result.topUsers)
	require.Empty(t, result.llmClients)
	require.Equal(t, int32(4), reader.started.Load())
	require.Equal(t, int32(2), reader.overviewCalls.Load())
	require.Equal(t, int32(1), reader.activeCountCalls.Load())
	require.Equal(t, int32(1), reader.topServerCalls.Load())
	require.Zero(t, reader.topUserCalls.Load())
	require.Zero(t, reader.llmClientCalls.Load())
}

func TestFetchProjectOverviewClickHouseRunsToolCallQueriesConcurrently(t *testing.T) {
	t.Parallel()

	reader := newBarrierProjectOverviewReader(6)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	result, err := fetchProjectOverviewClickHouse(ctx, reader, projectOverviewClickHouseParams{
		projectID:       "00000000-0000-0000-0000-000000000001",
		timeStart:       200,
		timeEnd:         300,
		comparisonStart: 100,
		comparisonEnd:   200,
		sessionMode:     false,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(11), result.toolMetrics.TotalToolCalls)
	require.Equal(t, uint64(7), result.toolMetricsComparison.TotalToolCalls)
	require.Equal(t, uint64(3), result.activeCounts.ActiveServersCount)
	require.Equal(t, uint64(4), result.activeCounts.ActiveUsersCount)
	require.Equal(t, []repo.TopServer{{ServerName: "server", ToolCallCount: 5}}, result.topServers)
	require.Equal(t, []repo.TopUser{{UserID: "user", UserType: "external", ActivityCount: 6}}, result.topUsers)
	require.Equal(t, []repo.LLMClientUsage{{ClientName: "client", ActivityCount: 8}}, result.llmClients)
	require.Equal(t, int32(6), reader.started.Load())
	require.Equal(t, int32(2), reader.overviewCalls.Load())
	require.Equal(t, int32(1), reader.activeCountCalls.Load())
	require.Equal(t, int32(1), reader.topServerCalls.Load())
	require.Equal(t, int32(1), reader.topUserCalls.Load())
	require.Equal(t, int32(1), reader.llmClientCalls.Load())
}

type barrierProjectOverviewReader struct {
	expected         int32
	started          atomic.Int32
	overviewCalls    atomic.Int32
	activeCountCalls atomic.Int32
	topServerCalls   atomic.Int32
	topUserCalls     atomic.Int32
	llmClientCalls   atomic.Int32
	allStarted       chan struct{}
	closeOnce        sync.Once
}

func newBarrierProjectOverviewReader(expected int32) *barrierProjectOverviewReader {
	return &barrierProjectOverviewReader{
		expected:   expected,
		allStarted: make(chan struct{}),
	}
}

func (r *barrierProjectOverviewReader) GetOverviewSummary(ctx context.Context, params repo.GetOverviewSummaryParams) (*repo.OverviewSummary, error) {
	r.overviewCalls.Add(1)
	if err := r.waitForQueries(ctx); err != nil {
		return nil, err
	}

	summary := &repo.OverviewSummary{}
	if params.TimeStart == 200 {
		summary.TotalToolCalls = 11
	} else {
		summary.TotalToolCalls = 7
	}
	return summary, nil
}

func (r *barrierProjectOverviewReader) GetActiveCounts(ctx context.Context, _ repo.GetActiveCountsParams) (*repo.ActiveCounts, error) {
	r.activeCountCalls.Add(1)
	if err := r.waitForQueries(ctx); err != nil {
		return nil, err
	}
	return &repo.ActiveCounts{ActiveServersCount: 3, ActiveUsersCount: 4}, nil
}

func (r *barrierProjectOverviewReader) GetTopServers(ctx context.Context, _ repo.GetTopServersParams) ([]repo.TopServer, error) {
	r.topServerCalls.Add(1)
	if err := r.waitForQueries(ctx); err != nil {
		return nil, err
	}
	return []repo.TopServer{{ServerName: "server", ToolCallCount: 5}}, nil
}

func (r *barrierProjectOverviewReader) GetTopUsers(ctx context.Context, _ repo.GetTopUsersParams) ([]repo.TopUser, error) {
	r.topUserCalls.Add(1)
	if err := r.waitForQueries(ctx); err != nil {
		return nil, err
	}
	return []repo.TopUser{{UserID: "user", UserType: "external", ActivityCount: 6}}, nil
}

func (r *barrierProjectOverviewReader) GetLLMClientBreakdown(ctx context.Context, _ repo.GetLLMClientBreakdownParams) ([]repo.LLMClientUsage, error) {
	r.llmClientCalls.Add(1)
	if err := r.waitForQueries(ctx); err != nil {
		return nil, err
	}
	return []repo.LLMClientUsage{{ClientName: "client", ActivityCount: 8}}, nil
}

func (r *barrierProjectOverviewReader) waitForQueries(ctx context.Context) error {
	if r.started.Add(1) == r.expected {
		r.closeOnce.Do(func() {
			close(r.allStarted)
		})
	}

	select {
	case <-r.allStarted:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for concurrent project overview queries: %w", ctx.Err())
	}
}
