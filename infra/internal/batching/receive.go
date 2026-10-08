// Package batching bounds unsettled work independently of Pub/Sub's callback
// flow control, which is released before a buffered message is acknowledged.
package batching

import (
	"context"
	"time"

	"golang.org/x/sync/semaphore"
)

// Settings bounds batches and all messages retained until handler settlement.
// Values must be positive except MaxBytes and MaxGroups (zero disables them).
type Settings struct {
	// MaxMessages triggers a batch flush.
	MaxMessages int

	// MaxBytes is a raw-input flush threshold; a single larger message is allowed.
	MaxBytes int

	// MaxLatency starts when the first message enters an empty batch.
	MaxLatency time.Duration

	// MaxGroups bounds distinct routing keys per batch.
	MaxGroups int

	// OutstandingMessages counts admitted work through settlement.
	OutstandingMessages int

	// OutstandingBytes budgets raw payloads through settlement. A larger single
	// message acquires the entire budget exclusively to avoid a permanent stall.
	OutstandingBytes int
}

type item[T any] struct {
	value  T
	weight int64
	bytes  int
	group  string
}

// Run owns one processing batch and one accumulating/pending batch, with at
// most one carry message when a new group crosses MaxGroups. Callbacks block
// at the input budget; permits are released only after handle has settled the
// batch. reject settles messages that cannot enter or outlive the receiver.
// receive must join its callbacks before returning. handle must honor ctx and
// settle every member before returning, including on cancellation and panic.
// receive must also bound callback concurrency and bytes: callbacks waiting for
// admission already own payloads, outside the admitted-input budget.
func Run[T any](ctx context.Context, settings Settings,
	receive func(context.Context, func(context.Context, T)) error,
	measure func(T) (int, string), reject func(T), handle func(context.Context, []T),
) error {
	handlerCtx, stopHandlers := context.WithCancel(ctx)
	defer stopHandlers()
	counts := semaphore.NewWeighted(int64(settings.OutstandingMessages))
	weights := semaphore.NewWeighted(int64(settings.OutstandingBytes))
	in := make(chan item[T])
	work := make(chan []item[T])
	stopped := make(chan struct{})
	collectorDone, workerDone := make(chan struct{}), make(chan struct{})
	release := func(m item[T]) { weights.Release(m.weight); counts.Release(1) }
	go func() {
		defer close(workerDone)
		for batch := range work {
			func() {
				defer func() {
					for _, m := range batch {
						release(m)
					}
				}()
				values := make([]T, len(batch))
				for i, m := range batch {
					values[i] = m.value
				}
				select {
				case <-stopped:
					for _, value := range values {
						reject(value)
					}
				default:
					handle(handlerCtx, values)
				}
			}()
		}
	}()
	go func() {
		defer close(collectorDone)
		defer close(work)
		var pending []item[T]
		var carry *item[T]
		var timer *time.Timer
		var tick <-chan time.Time
		var groups map[string]bool
		bytes, ready := 0, false
		cancelled := ctx.Done()
		stopTimer := func() {
			if timer != nil {
				timer.Stop()
			}
			tick = nil
		}
		defer stopTimer()
		appendItem := func(m item[T]) {
			if len(pending) == 0 {
				groups = map[string]bool{}
				timer = time.NewTimer(settings.MaxLatency)
				tick = timer.C
			}
			pending = append(pending, m)
			bytes += m.bytes
			groups[m.group] = true
			ready = len(pending) >= settings.MaxMessages || (settings.MaxBytes > 0 && bytes >= settings.MaxBytes) || ctx.Err() != nil
		}
		for {
			input := in
			var output chan []item[T]
			if ready {
				input = nil
				output = work
				stopTimer()
			}
			select {
			case m := <-input:
				if len(pending) > 0 && settings.MaxGroups > 0 && !groups[m.group] && len(groups) >= settings.MaxGroups {
					carry, ready = &m, true
				} else {
					appendItem(m)
				}
			case output <- pending:
				pending, groups, bytes, ready = nil, nil, 0, false
				if carry != nil {
					appendItem(*carry)
					carry = nil
				}
			case <-tick:
				ready = len(pending) > 0
			case <-cancelled:
				cancelled = nil
				ready = len(pending) > 0
			case <-stopped:
				// Cancellation flushes while receive is still alive; any remainder
				// after a fatal stream return must not start a new storage write.
				for _, m := range pending {
					reject(m.value)
					release(m)
				}
				if carry != nil {
					reject(carry.value)
					release(*carry)
				}
				return
			}
		}
	}()
	err := receive(ctx, func(deliveryCtx context.Context, value T) {
		size, group := measure(value)
		weight := int64(min(max(size, 1), settings.OutstandingBytes))
		if err := counts.Acquire(deliveryCtx, 1); err != nil {
			reject(value)
			return
		}
		if err := weights.Acquire(deliveryCtx, weight); err != nil {
			counts.Release(1)
			reject(value)
			return
		}
		m := item[T]{value: value, weight: weight, bytes: size, group: group}
		select {
		case in <- m:
		case <-deliveryCtx.Done():
			reject(value)
			release(m)
		}
	})
	close(stopped)
	stopHandlers()
	<-collectorDone
	<-workerDone
	return err
}
