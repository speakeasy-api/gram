package batching

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRun_BudgetHeldUntilHandlerSettles(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		blocked := make(chan struct{})
		var accepted, handled atomic.Int32
		var settled sync.WaitGroup
		settled.Add(3)

		done := make(chan error, 1)
		go func() {
			done <- Run(ctx, Settings{MaxMessages: 1, MaxLatency: time.Minute, OutstandingMessages: 2, OutstandingBytes: 10},
				func(ctx context.Context, deliver func(context.Context, string)) error {
					for range 3 {
						deliver(ctx, "12345")
						accepted.Add(1)
					}
					settled.Wait()
					return nil
				}, func(s string) (int, string) { return len(s), "" }, func(string) { settled.Done() },
				func(context.Context, []string) { <-blocked; handled.Add(1); settled.Done() })
		}()

		synctest.Wait()
		require.Equal(t, int32(2), accepted.Load(), "one active and one pending batch; third callback waits")
		require.Zero(t, handled.Load())

		close(blocked)
		require.NoError(t, <-done)
		require.Equal(t, int32(3), handled.Load())
	})
}

func TestRun_GroupOverflowStartsAnotherBatch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var batches [][]string
		var settled sync.WaitGroup
		settled.Add(4)

		err := Run(t.Context(), Settings{MaxMessages: 100, MaxLatency: time.Second, MaxGroups: 2, OutstandingMessages: 5, OutstandingBytes: 100},
			func(ctx context.Context, deliver func(context.Context, string)) error {
				for _, route := range []string{"a", "b", "a", "c"} {
					deliver(ctx, route)
				}
				settled.Wait()
				return nil
			}, func(s string) (int, string) { return len(s), s }, func(string) { settled.Done() },
			func(_ context.Context, batch []string) {
				batches = append(batches, batch)
				for range batch {
					settled.Done()
				}
			})
		require.NoError(t, err)
		require.Equal(t, [][]string{{"a", "b", "a"}, {"c"}}, batches)
	})
}

func TestRun_CancellationSettlesBeforeReceiverReturns(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var settled sync.WaitGroup
		settled.Add(3)
		done := make(chan error, 1)
		var rejected atomic.Int32

		go func() {
			done <- Run(ctx, Settings{MaxMessages: 1, MaxLatency: time.Hour, OutstandingMessages: 2, OutstandingBytes: 2},
				func(ctx context.Context, deliver func(context.Context, int)) error {
					for i := range 3 {
						deliver(ctx, i)
					}
					settled.Wait()
					return nil
				}, func(int) (int, string) { return 1, "" }, func(int) { rejected.Add(1); settled.Done() },
				func(ctx context.Context, batch []int) {
					<-ctx.Done()
					for range batch {
						settled.Done()
					}
				})
		}()

		synctest.Wait()
		cancel()
		require.NoError(t, <-done)
		require.Positive(t, rejected.Load(), "budget-waiting delivery is rejected on cancellation")
	})
}

func TestRun_StreamFailureDoesNotFlushPending(t *testing.T) {
	t.Parallel()

	var rejected, handled atomic.Int32
	err := Run(t.Context(), Settings{MaxMessages: 100, MaxLatency: time.Hour, OutstandingMessages: 2, OutstandingBytes: 10},
		func(ctx context.Context, deliver func(context.Context, int)) error {
			deliver(ctx, 1)
			return errors.New("stream disconnected")
		},
		func(int) (int, string) { return 1, "" }, func(int) { rejected.Add(1) }, func(context.Context, []int) { handled.Add(1) })
	require.ErrorContains(t, err, "stream disconnected")
	require.Equal(t, int32(1), rejected.Load())
	require.Zero(t, handled.Load())
}

func TestRun_OversizedMessageAndLatencyFlush(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var batches [][]string
		var settled sync.WaitGroup
		settled.Add(2)

		err := Run(t.Context(), Settings{MaxMessages: 100, MaxBytes: 5, MaxLatency: time.Second, OutstandingMessages: 2, OutstandingBytes: 5},
			func(ctx context.Context, deliver func(context.Context, string)) error {
				deliver(ctx, "123456789")
				deliver(ctx, "a")
				settled.Wait()
				return nil
			},
			func(s string) (int, string) { return len(s), "" }, func(string) { settled.Done() },
			func(_ context.Context, batch []string) {
				batches = append(batches, batch)
				for range batch {
					settled.Done()
				}
			})
		require.NoError(t, err)
		require.Equal(t, [][]string{{"123456789"}, {"a"}}, batches)
	})
}
