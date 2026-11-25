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
		settled.Add(5)

		done := make(chan error, 1)
		go func() {
			done <- Run(ctx, Settings{MaxMessages: 1, MaxLatency: time.Minute, OutstandingMessages: 4, OutstandingBytes: 100},
				func(ctx context.Context, deliver func(context.Context, string)) error {
					for range 5 {
						deliver(ctx, "12345")
						accepted.Add(1)
					}
					settled.Wait()
					return nil
				}, func(s string) int { return len(s) }, func(string) { settled.Done() },
				func(context.Context, []string) { <-blocked; handled.Add(1); settled.Done() })
		}()

		synctest.Wait()
		require.Equal(t, int32(4), accepted.Load(), "queued batches fill the count budget; the fifth callback waits for settlement")
		require.Zero(t, handled.Load())

		close(blocked)
		require.NoError(t, <-done)
		require.Equal(t, int32(5), handled.Load())
	})
}

func TestRun_CancellationDrainsPartialBundleBeforeReceiverReturns(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var rejected, handled atomic.Int32
		settled := make(chan struct{})
		started := time.Now()
		err := Run(ctx, Settings{MaxMessages: 100, MaxLatency: time.Hour, OutstandingMessages: 2, OutstandingBytes: 10},
			func(ctx context.Context, deliver func(context.Context, int)) error {
				deliver(ctx, 1)
				cancel()
				<-settled
				return nil
			}, func(int) int { return 1 }, func(int) { rejected.Add(1); close(settled) },
			func(context.Context, []int) { handled.Add(1); close(settled) })

		require.NoError(t, err)
		require.Equal(t, int32(1), rejected.Load())
		require.Zero(t, handled.Load())
		require.Equal(t, time.Duration(0), time.Since(started), "shutdown must not wait for the latency timer")
	})
}

func TestRun_AdmissionRacingCancellationFlushStillSettles(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var rejected, handled atomic.Int32
		settled := make(chan struct{})
		started := time.Now()
		err := Run(ctx, Settings{MaxMessages: 100, MaxLatency: time.Hour, OutstandingMessages: 2, OutstandingBytes: 10},
			func(ctx context.Context, deliver func(context.Context, int)) error {
				// Keep this callback alive to force admission after the shutdown
				// flush has already taken its snapshot of the empty bundler.
				deliver(context.WithoutCancel(ctx), 1)
				<-settled
				return nil
			}, func(int) int {
				cancel()
				synctest.Wait()
				return 1
			}, func(int) { rejected.Add(1); close(settled) },
			func(context.Context, []int) { handled.Add(1); close(settled) })

		require.NoError(t, err)
		require.Equal(t, int32(1), rejected.Load())
		require.Zero(t, handled.Load())
		require.Equal(t, time.Duration(0), time.Since(started))
	})
}

func TestRun_StreamFailureCancelsActiveAndRejectsQueued(t *testing.T) {
	t.Parallel()

	active := make(chan struct{})
	failure := errors.New("stream disconnected")
	var handled, rejected atomic.Int32
	err := Run(t.Context(), Settings{MaxMessages: 1, MaxLatency: time.Hour, OutstandingMessages: 2, OutstandingBytes: 10},
		func(ctx context.Context, deliver func(context.Context, int)) error {
			deliver(ctx, 1)
			<-active
			deliver(ctx, 2)
			return failure
		}, func(int) int { return 1 }, func(int) { rejected.Add(1) },
		func(ctx context.Context, batch []int) {
			handled.Add(int32(len(batch)))
			close(active)
			<-ctx.Done()
		})

	require.ErrorIs(t, err, failure)
	require.Equal(t, int32(1), handled.Load())
	require.Equal(t, int32(1), rejected.Load())
}

func TestRun_StreamFailureWaitsForHandlerSettlement(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		active, release := make(chan struct{}), make(chan struct{})
		var cancelled, settled atomic.Bool
		failure := errors.New("stream disconnected")
		done := make(chan error, 1)
		go func() {
			done <- Run(t.Context(), Settings{MaxMessages: 1, MaxLatency: time.Hour, OutstandingMessages: 2, OutstandingBytes: 10},
				func(ctx context.Context, deliver func(context.Context, int)) error {
					deliver(ctx, 1)
					<-active
					return failure
				}, func(int) int { return 1 }, func(int) {},
				func(ctx context.Context, _ []int) {
					close(active)
					<-ctx.Done()
					cancelled.Store(true)
					<-release
					settled.Store(true)
				})
		}()

		synctest.Wait()
		require.True(t, cancelled.Load())
		require.False(t, settled.Load())
		select {
		case <-done:
			t.Fatal("Run returned before the active handler settled")
		default:
		}

		close(release)
		require.ErrorIs(t, <-done, failure)
		require.True(t, settled.Load())
	})
}

func TestRun_ByteThresholdFlushesBeforeLatency(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var batches [][]string
		settled := make(chan struct{})
		started := time.Now()
		err := Run(t.Context(), Settings{MaxMessages: 100, MaxBytes: 3, MaxLatency: time.Hour, OutstandingMessages: 10, OutstandingBytes: 100},
			func(ctx context.Context, deliver func(context.Context, string)) error {
				deliver(ctx, "ab")
				deliver(ctx, "c")
				<-settled
				return nil
			}, func(s string) int { return len(s) }, func(string) {},
			func(_ context.Context, batch []string) { batches = append(batches, batch); close(settled) })

		require.NoError(t, err)
		require.Equal(t, [][]string{{"ab", "c"}}, batches)
		require.Equal(t, time.Duration(0), time.Since(started), "byte threshold must flush without waiting for the timer")
	})
}

func TestRun_ZeroByteThresholdUsesCountAndLatency(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var batches [][]string
		var settled sync.WaitGroup
		settled.Add(3)

		err := Run(t.Context(), Settings{MaxMessages: 2, MaxLatency: time.Second, OutstandingMessages: 3, OutstandingBytes: 100},
			func(ctx context.Context, deliver func(context.Context, string)) error {
				for _, value := range []string{"a", "b", "c"} {
					deliver(ctx, value)
				}
				settled.Wait()
				return nil
			}, func(s string) int { return len(s) }, func(string) { settled.Done() },
			func(_ context.Context, batch []string) {
				batches = append(batches, batch)
				for range batch {
					settled.Done()
				}
			})

		require.NoError(t, err)
		require.Equal(t, [][]string{{"a", "b"}, {"c"}}, batches)
	})
}

func TestRun_ByteBudgetHeldUntilHandlerSettles(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		blocked := make(chan struct{})
		var accepted atomic.Int32
		var settled sync.WaitGroup
		settled.Add(3)

		done := make(chan error, 1)
		go func() {
			done <- Run(t.Context(), Settings{MaxMessages: 1, MaxLatency: time.Minute, OutstandingMessages: 10, OutstandingBytes: 10},
				func(ctx context.Context, deliver func(context.Context, string)) error {
					for range 3 {
						deliver(ctx, "12345")
						accepted.Add(1)
					}
					settled.Wait()
					return nil
				}, func(s string) int { return len(s) }, func(string) { settled.Done() },
				func(context.Context, []string) { <-blocked; settled.Done() })
		}()

		synctest.Wait()
		require.Equal(t, int32(2), accepted.Load(), "byte budget applies even with spare count permits")

		close(blocked)
		require.NoError(t, <-done)
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
				}, func(int) int { return 1 }, func(int) { rejected.Add(1); settled.Done() },
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
		func(int) int { return 1 }, func(int) { rejected.Add(1) }, func(context.Context, []int) { handled.Add(1) })
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
			func(s string) int { return len(s) }, func(string) { settled.Done() },
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
