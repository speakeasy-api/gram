package metrics

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

func TestClientFamilyClassifiesMCPClientNames(t *testing.T) {
	for name, family := range map[string]string{
		"claude-code":                   "claude",
		"Claude Code":                   "claude",
		"claude-ai":                     "claude",
		"cursor-vscode":                 "cursor",
		"Cursor":                        "cursor",
		"codex-mcp-client":              "codex",
		"Codex CLI":                     "codex",
		"ChatGPT":                       "chatgpt",
		"mcp-inspector":                 "inspector",
		"Visual Studio Code":            "other",
		"Visual Studio Code - Insiders": "other",
		"windsurf-client":               "other",
		"PRIVATE_CLIENT_NAME":           "other",
		"":                              "unknown",
	} {
		require.Equal(t, family, ClientFamily(name), name)
	}
}

func TestPreferredFamilyTrustsTheFirstNamedSource(t *testing.T) {
	require.Equal(t, "cursor", PreferredFamily("cursor", "claude"))
	require.Equal(t, "claude", PreferredFamily("other", "claude"), "an unrecognized client name falls back to a recognized user agent")
	require.Equal(t, "claude", PreferredFamily("", "claude"))
	require.Equal(t, "other", PreferredFamily("other", "unknown"))
	require.Equal(t, "other", PreferredFamily("unknown", "other"))
	require.Equal(t, "unknown", PreferredFamily("", "unknown"))
	require.Equal(t, "unknown", PreferredFamily())
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
	c.drop("source")
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
	require.Empty(t, c.drops)
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
	require.Empty(t, c.drops)
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

func TestRejectionsAreCountedApartFromErrorsAndLatency(t *testing.T) {
	c := New()
	now := time.Now().UTC().Truncate(time.Minute).Add(10 * time.Second)
	c.now = func() time.Time { return now }
	c.Observe("source", "server", "tools/call", "claude", "attempt", 0)
	c.Observe("source", "server", "tools/call", "claude", "rejected", 5*time.Millisecond)
	var row Snapshot
	c.Flush(t.Context(), func(_ context.Context, s Snapshot) error {
		if s.Kind == "requests" {
			row = s
		}
		return nil
	})
	require.EqualValues(t, 1, row.Attempts)
	require.EqualValues(t, 1, row.Rejected)
	require.Zero(t, row.Errors)
	require.Zero(t, row.Incomplete)
	require.Equal(t, [12]uint64{}, row.LatencyBins, "a rejection never reached the MCP server, so it has no response time")
}

func TestUnroutedRequestsAreErrorsWithoutLatency(t *testing.T) {
	c := New()
	now := time.Now().UTC().Truncate(time.Minute).Add(10 * time.Second)
	c.now = func() time.Time { return now }
	c.Observe("source", "server", "tools/call", "claude", "attempt", 0)
	c.Observe("source", "server", "tools/call", "claude", "unrouted", 0)
	var row Snapshot
	c.Flush(t.Context(), func(_ context.Context, s Snapshot) error {
		if s.Kind == "requests" {
			row = s
		}
		return nil
	})
	require.EqualValues(t, 1, row.Errors)
	require.Equal(t, [12]uint64{}, row.LatencyBins, "a request that never reached an agent has no response time")
}

func TestDiscardedRowsRaiseOnlyTheirSourceWatermark(t *testing.T) {
	t.Parallel()
	c := New()
	now := time.Now().UTC().Truncate(time.Minute).Add(10 * time.Second)
	c.now = func() time.Time { return now }
	c.Observe("lossy", "server", "tools/call", "unknown", "attempt", 0)
	c.Observe("healthy", "server", "tools/call", "unknown", "attempt", 0)
	c.Flush(t.Context(), func(_ context.Context, s Snapshot) error {
		if s.SourceID == "lossy" {
			return errors.New("publisher unavailable")
		}
		return nil
	})
	// Past the retry limit, lossy's unpublished request and coverage rows are
	// discarded; healthy's were published and are not loss.
	now = now.Add(11 * time.Minute)
	c.Flush(t.Context(), func(context.Context, Snapshot) error { return nil })
	c.Observe("lossy", "server", "tools/call", "unknown", "attempt", 0)
	c.Observe("healthy", "server", "tools/call", "unknown", "attempt", 0)
	c.Connections("lossy", 1, 0, 0, 0)
	c.Connections("healthy", 1, 0, 0, 0)
	var mu sync.Mutex
	watermarks := map[Key]uint64{}
	c.Flush(t.Context(), func(_ context.Context, s Snapshot) error {
		if s.Kind != "requests" {
			mu.Lock()
			watermarks[Key{SourceID: s.SourceID, Kind: s.Kind}] = s.Incomplete
			mu.Unlock()
		}
		return nil
	})
	require.Equal(t, map[Key]uint64{
		{SourceID: "lossy", Kind: "coverage"}:      2,
		{SourceID: "lossy", Kind: "connections"}:   2,
		{SourceID: "healthy", Kind: "coverage"}:    0,
		{SourceID: "healthy", Kind: "connections"}: 0,
	}, watermarks)
}

func TestSeriesCapLossIsChargedToTheDroppedSource(t *testing.T) {
	t.Parallel()
	c := New()
	for i := range MaxSeries - maxSources {
		c.series[Key{SourceID: "noisy", Kind: "requests", Bucket: int64(i)}] = Snapshot{}
	}
	c.Observe("quiet", "server", "tools/call", "unknown", "attempt", 0)
	require.EqualValues(t, 1, c.watermark("quiet"))
	require.Zero(t, c.watermark("noisy"))
	c.Observe("quiet", "server", "tools/list", "unknown", "attempt", 0)
	require.EqualValues(t, 2, c.watermark("quiet"), "a source's watermark keeps growing")
}

func TestLossBeyondTrackedSourcesMarksEverySource(t *testing.T) {
	t.Parallel()
	c := New()
	for i := range maxSources {
		c.drop(fmt.Sprint(i))
	}
	c.drop("0")
	c.drop("untracked")
	require.Len(t, c.drops, maxSources)
	require.EqualValues(t, 1, c.overflowDrops)
	require.EqualValues(t, 3, c.watermark("0"))
	require.EqualValues(t, 2, c.watermark("1"))
	require.EqualValues(t, 1, c.watermark("untracked"))
	require.EqualValues(t, 1, c.watermark("never-dropped"))
}
