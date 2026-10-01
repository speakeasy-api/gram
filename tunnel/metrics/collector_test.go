package metrics

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCumulativeSnapshotsHaveStableIdentityAndIncreasingRevision(t *testing.T) {
	c := New()
	now := time.Now().UTC().Truncate(time.Minute).Add(10 * time.Second)
	c.now = func() time.Time { return now }
	c.Observe("source", "server", "tools/call", "claude", "attempt", 0)
	var first, second Snapshot
	c.Flush(t.Context(), func(_ context.Context, s Snapshot) error {
		if s.Kind == "requests" {
			first = s
		}
		return nil
	})
	c.Observe("source", "server", "tools/call", "claude", "success", 80*time.Millisecond)
	c.Flush(t.Context(), func(_ context.Context, s Snapshot) error {
		if s.Kind == "requests" {
			second = s
		}
		return nil
	})
	require.Equal(t, first.Key, second.Key)
	require.Equal(t, first.ProducerID, second.ProducerID)
	require.Greater(t, second.Revision, first.Revision)
	require.EqualValues(t, 1, second.Attempts)
	require.EqualValues(t, 1, second.Successes)
	require.EqualValues(t, 1, second.LatencyBins[2])
}

func TestDimensionsDiscardUnknownNames(t *testing.T) {
	require.Equal(t, "other", Method("private/method/secret"))
	require.Equal(t, "other", ClientFamily("PrivateClient/private-user"))
	require.Equal(t, "claude", ClientFamily("Claude/1.0 private-user"))
}

func TestFullFleetFlushIsBoundedAndRecordingContinues(t *testing.T) {
	c := New()
	for i := range 10000 {
		c.Observe(fmt.Sprint(i), "server", "tools/call", fmt.Sprintf("client-%d", i), "attempt", 0)
	}
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	done := make(chan struct{})
	var unbounded atomic.Bool
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		c.Flush(ctx, func(ctx context.Context, s Snapshot) error {
			select {
			case entered <- struct{}{}:
			default:
			}
			if s.Kind == "requests" && s.ClientFamily != "other" {
				unbounded.Store(true)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return nil
			}
		})
		close(done)
	}()
	for range 16 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("publisher concurrency not reached")
		}
	}
	start := time.Now()
	c.Observe("another-source", "server", "tools/list", "codex", "attempt", 0)
	require.Less(t, time.Since(start), 100*time.Millisecond, "publisher must not lock recording")
	cancel()
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("flush did not stop")
	}
	require.False(t, unbounded.Load())
	require.LessOrEqual(t, len(c.series), MaxSeries)
}

func TestCoverageReportsIdleAndLossWithoutRequestPayloads(t *testing.T) {
	c := New()
	c.Observe("source", "server", "tools/list", "unknown", "attempt", 0)
	c.dropped.Add(1)
	var coverage Snapshot
	c.Flush(t.Context(), func(_ context.Context, s Snapshot) error {
		if s.Kind == "coverage" {
			coverage = s
		}
		return nil
	})
	require.Equal(t, "source", coverage.SourceID)
	require.Empty(t, coverage.ServerID)
	require.EqualValues(t, 1, coverage.Incomplete)
	require.Zero(t, coverage.Attempts)
}

func TestGatewayCoverageKeepsDisconnectedZeroForBoundedLease(t *testing.T) {
	c := New()
	now := time.Now()
	c.now = func() time.Time { return now }
	c.Connections("source", 1, 0, 0, 1)
	c.Connections("source", 0, 0, 0, 0)
	require.Equal(t, []string{"source"}, c.GaugeSources())
	require.Empty(t, c.sources, "gateway observation cannot prove MCP request coverage")
	now = now.Add(6 * time.Minute)
	require.Empty(t, c.GaugeSources())
}

func TestCoverageSourcesExpireAndPublishedRowsDoNotCountAsLoss(t *testing.T) {
	c := New()
	now := time.Now().UTC().Truncate(time.Minute)
	c.now = func() time.Time { return now }
	c.Observe("expired", "server", "tools/list", "unknown", "attempt", 0)
	publish := func(context.Context, Snapshot) error { return nil }
	c.Flush(t.Context(), publish)
	now = now.Add(2 * time.Minute)
	c.Flush(t.Context(), publish)
	now = now.Add(10 * time.Minute)
	c.Flush(t.Context(), publish)
	require.Empty(t, c.sources)
	require.Zero(t, c.dropped.Load())
	c.Observe("replacement", "server", "tools/list", "unknown", "attempt", 0)
	require.Contains(t, c.sources, "replacement")
}

func TestLatencyUsesExactCompletionBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		duration time.Duration
		bin      int
	}{
		{"at boundary", 10 * time.Millisecond, 0},
		{"above boundary", 10*time.Millisecond + time.Nanosecond, 1},
		{"above last boundary", 60*time.Second + time.Nanosecond, 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := New()
			c.Observe("source", "server", "tools/call", "unknown", "success", tc.duration)
			for _, row := range c.series {
				var expected [12]uint64
				expected[tc.bin] = 1
				require.Equal(t, expected, row.LatencyBins)
			}
		})
	}
}

func TestIncompleteObservationsHaveNoCompletionLatency(t *testing.T) {
	t.Parallel()
	c := New()
	c.Observe("source", "server", "tools/call", "unknown", "incomplete", time.Second)
	for _, row := range c.series {
		require.EqualValues(t, 1, row.Incomplete)
		require.Equal(t, [12]uint64{}, row.LatencyBins)
	}
}

func TestFlushSkipsConfirmedRevisionsAndRetainsNewObservations(t *testing.T) {
	t.Parallel()
	c := New()
	now := time.Now().UTC().Truncate(time.Minute).Add(10 * time.Second)
	c.now = func() time.Time { return now }
	c.Observe("source", "server", "tools/call", "unknown", "attempt", 0)
	var count atomic.Uint64
	publish := func(_ context.Context, s Snapshot) error {
		if s.Kind == "requests" {
			count.Add(1)
		}
		return nil
	}
	c.Flush(t.Context(), publish)
	c.Flush(t.Context(), publish)
	require.EqualValues(t, 1, count.Load())
	c.Observe("source", "server", "tools/call", "unknown", "success", time.Second)
	c.Flush(t.Context(), publish)
	require.EqualValues(t, 2, count.Load())
	now = now.Add(2 * time.Minute)
	c.Flush(t.Context(), publish)
	require.EqualValues(t, 2, count.Load())
	for key := range c.series {
		require.NotEqual(t, "requests", key.Kind)
	}
	require.Zero(t, c.dropped.Load())
}

func TestFlushRetriesUnconfirmedRevisions(t *testing.T) {
	t.Parallel()
	c := New()
	c.Observe("source", "server", "tools/list", "unknown", "attempt", 0)
	c.Flush(t.Context(), func(context.Context, Snapshot) error { return errors.New("publisher unavailable") })
	var attempts atomic.Uint64
	c.Flush(t.Context(), func(_ context.Context, s Snapshot) error {
		if s.Kind == "requests" {
			attempts.Add(s.Attempts)
		}
		return nil
	})
	require.EqualValues(t, 1, attempts.Load())
}
