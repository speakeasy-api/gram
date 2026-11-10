package overview

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

func TestFetchProjectOverviewClickHouseRunsQueriesConcurrently(t *testing.T) {
	t.Parallel()

	reader := newBarrierProjectOverviewReader(3)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	result, err := FetchClickHouse(ctx, reader, Params{
		ProjectID:         "00000000-0000-0000-0000-000000000001",
		TimeStart:         200,
		TimeEnd:           300,
		HostedMCPMatchers: nil,
		MCPServerMatchers: nil,
		MetaMCPMatchers:   nil,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(11), result.ToolMetrics.TotalToolCalls)
	require.Equal(t, uint64(5), result.ActiveServersCount)
	require.Equal(t, []repo.TopServer{{ServerName: "server", ToolCallCount: 5}}, result.TopServers)
	require.Equal(t, int32(3), reader.started.Load())
	require.Equal(t, int32(1), reader.overviewCalls.Load())
	require.Equal(t, int32(1), reader.unifiedActiveServerCalls.Load())
	require.Equal(t, int32(1), reader.topServerCalls.Load())
}

type barrierProjectOverviewReader struct {
	expected                 int32
	started                  atomic.Int32
	overviewCalls            atomic.Int32
	unifiedActiveServerCalls atomic.Int32
	topServerCalls           atomic.Int32
	allStarted               chan struct{}
	closeOnce                sync.Once
}

func newBarrierProjectOverviewReader(expected int32) *barrierProjectOverviewReader {
	return &barrierProjectOverviewReader{
		expected:   expected,
		allStarted: make(chan struct{}),
	}
}

func (r *barrierProjectOverviewReader) GetOverviewSummary(ctx context.Context, _ repo.GetOverviewSummaryParams) (*repo.OverviewSummary, error) {
	r.overviewCalls.Add(1)
	if err := r.waitForQueries(ctx); err != nil {
		return nil, err
	}
	return &repo.OverviewSummary{TotalToolCalls: 11}, nil
}

func (r *barrierProjectOverviewReader) GetUnifiedActiveServerCount(ctx context.Context, _ repo.GetTopServersParams) (uint64, error) {
	r.unifiedActiveServerCalls.Add(1)
	if err := r.waitForQueries(ctx); err != nil {
		return 0, err
	}
	return 5, nil
}

func (r *barrierProjectOverviewReader) GetTopServers(ctx context.Context, _ repo.GetTopServersParams) ([]repo.TopServer, error) {
	r.topServerCalls.Add(1)
	if err := r.waitForQueries(ctx); err != nil {
		return nil, err
	}
	return []repo.TopServer{{ServerName: "server", ToolCallCount: 5}}, nil
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
