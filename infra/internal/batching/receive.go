// Package batching adapts Google's bundler to Pub/Sub settlement and shutdown.
package batching

import (
	"context"
	"math"
	"time"

	"golang.org/x/sync/semaphore"
	"google.golang.org/api/support/bundler"
)

// Settings bounds batches and admitted messages through handler settlement.
// Values must be positive except MaxBytes (zero disables byte-triggered flushing).
type Settings struct {
	// MaxMessages triggers a batch flush.
	MaxMessages int

	// MaxBytes triggers flushing by accounted payload bytes, not a batch size cap.
	// Queued bundles can keep growing up to the count and outstanding byte limits.
	MaxBytes int

	// MaxLatency starts when the first message enters an empty bundle.
	MaxLatency time.Duration

	// OutstandingMessages bounds admitted messages until their handler returns.
	OutstandingMessages int

	// OutstandingBytes bounds the bundler's queued and processing payloads.
	// A larger single message acquires the entire budget exclusively; empty
	// payloads account for one byte to avoid unbounded zero-weight buffering.
	OutstandingBytes int
}

// Run uses a serial bundler handler and count/byte admission budgets. The receiver
// must bound its own callback memory and join callbacks before returning. handle
// must honor ctx, recover its panics and settle every member before returning.
// Cancellation rejects queued work while receive is still alive so Pub/Sub can
// observe settlement; active handlers decide which work has already committed.
func Run[T any](ctx context.Context, settings Settings,
	receive func(context.Context, func(context.Context, T)) error,
	measure func(T) int, reject func(T), handle func(context.Context, []T),
) error {
	handlerCtx, stopHandlers := context.WithCancel(ctx)
	defer stopHandlers()

	counts := semaphore.NewWeighted(int64(settings.OutstandingMessages))
	var example T
	b := bundler.NewBundler(example, func(bundle any) {
		batch := bundle.([]T)
		defer counts.Release(int64(len(batch)))

		if handlerCtx.Err() != nil {
			for _, value := range batch {
				reject(value)
			}

			return
		}

		handle(handlerCtx, batch)
	})
	b.DelayThreshold = settings.MaxLatency
	b.BundleCountThreshold = settings.MaxMessages
	b.BundleByteThreshold = math.MaxInt
	if settings.MaxBytes > 0 {
		b.BundleByteThreshold = min(settings.MaxBytes, settings.OutstandingBytes)
	}

	b.BufferedByteLimit = settings.OutstandingBytes
	b.HandlerLimit = 1

	// Receive can wait for every delivery to settle before returning. Flush on
	// cancellation concurrently with it, rather than waiting for that return.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-handlerCtx.Done()
		b.Flush()
	}()

	err := receive(handlerCtx, func(deliveryCtx context.Context, value T) {
		if handlerCtx.Err() != nil {
			reject(value)
			return
		}

		if err := counts.Acquire(deliveryCtx, 1); err != nil {
			reject(value)
			return
		}

		weight := min(max(measure(value), 1), settings.OutstandingBytes)
		if err := b.AddWait(deliveryCtx, value, weight); err != nil {
			counts.Release(1)
			reject(value)
			return
		}

		// An admitted callback may race the cancellation flush. Flush again if
		// it added a message after that snapshot, so receive cannot await a
		// stranded message until the bundler's latency timer expires.
		if handlerCtx.Err() != nil {
			b.Flush()
		}
	})

	stopHandlers()
	<-drained

	// The receiver has joined all callbacks; this final snapshot covers every
	// accepted message even when receive exited with a stream error.
	b.Flush()

	return err
}
