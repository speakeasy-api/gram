// Package pubsub carries the server-side helpers layered over the Pub/Sub
// client in infra/pkg/gcp.
//
// Drainer observes the outcome of asynchronous publishes without putting
// broker latency on the publishing caller's path.
//
// gcp.Publisher.Publish buffers a message and returns a future; the outcome is
// only known once PublishResult.Get unblocks, after the batch flushes and the
// broker acks. That is DelayThreshold plus a round trip in the good case and
// PublishSettings.Timeout in the bad one, so a caller on a request path cannot
// wait for it. Spawning a goroutine per publish avoids the wait but makes
// goroutine and runtime-timer counts scale with request rate.
//
// A Drainer decouples the two: publishers hand results to a bounded queue and
// return immediately, and a fixed pool of goroutines observes the outcomes.
package pubsub

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	// meterAckFailures counts publishes whose broker ack reported an error.
	meterAckFailures = "pubsub.publish.ack_failures"

	// meterAckDrops counts publish results discarded without being observed,
	// because the drain queue was full or the drainer was already closed.
	meterAckDrops = "pubsub.publish.ack_drops"

	// drainWorkers is the number of goroutines observing publish results. It is
	// fixed rather than one-per-publish so goroutine and timer counts stay flat
	// as request rate climbs, and greater than one so a single stalled result —
	// bounded at the publisher by PublishSettings.Timeout — cannot stall the
	// whole drain behind it.
	drainWorkers = 8

	// queueDepth bounds the results awaiting observation. Past this point
	// results are dropped and counted: an ack outcome is telemetry about a
	// best-effort publish, so shedding it under load is preferable to a queue
	// that grows without limit.
	queueDepth = 4096

	// logInterval throttles failure logs. Publish failures are correlated — a
	// degraded topic fails every in-flight result at once — so an unthrottled
	// log emits one line per publish at request rate, turning a broker blip
	// into a log flood. The counter carries the true rate; the log carries one
	// example error plus how many lines it stood in for.
	logInterval = 10 * time.Second
)

// batch is one unit of work for the drain pool. It carries the publishing
// caller's context so failure logs keep the trace correlation the publish span
// established; the results themselves are resolved against the drainer's own
// context so shutdown can abort them.
type batch struct {
	// A queued work item carries its originating context by design: this is
	// the queue equivalent of passing ctx as the first argument, and dropping
	// it would lose the trace correlation on the failure log.
	//nolint:containedctx // see above
	ctx     context.Context
	results []gcp.PublishResult
}

// Drainer resolves publish results on a fixed pool of goroutines. The zero
// value is not usable; construct one with NewDrainer.
type Drainer struct {
	logger *slog.Logger
	logMsg string

	queue chan batch

	// mu guards queue against an Enqueue send racing Close's close(queue).
	// Enqueue takes it for reading, so concurrent publishers do not contend
	// with each other — only with a one-time Close.
	mu     sync.RWMutex
	closed bool

	// start defers spawning workers until the first Enqueue. Inert publishers
	// (the noop constructors used across tests) then cost no goroutines.
	start sync.Once

	// pending counts enqueued-but-unobserved batches. Wait exposes it as a
	// test-only barrier; it is not part of the runtime contract.
	pending sync.WaitGroup
	workers sync.WaitGroup

	// This context belongs to the pool's lifetime rather than to any one
	// caller, so there is no argument to pass it as. It exists so Close can
	// abort resolutions that outlive its deadline.
	//nolint:containedctx // see above
	ctx    context.Context
	cancel context.CancelFunc

	failures   metric.Int64Counter
	drops      metric.Int64Counter
	metricAttr metric.MeasurementOption

	lastLogUnixNano atomic.Int64
	suppressedLogs  atomic.Int64
}

// NewDrainer constructs a Drainer. The name identifies the draining publisher
// on the shared ack metrics, and logMsg is the message emitted when publishes
// fail.
func NewDrainer(logger *slog.Logger, meterProvider metric.MeterProvider, name string, logMsg string) *Drainer {
	// #nosec G118: cancel is retained on the struct and called by Close.
	ctx, cancel := context.WithCancel(context.Background())
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/pubsub")

	failures, err := meter.Int64Counter(
		meterAckFailures,
		metric.WithDescription("Pub/Sub publishes whose broker ack reported an error"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric error", attr.SlogMetricName(meterAckFailures), attr.SlogError(err))
	}

	drops, err := meter.Int64Counter(
		meterAckDrops,
		metric.WithDescription("Pub/Sub publish results discarded without their ack being observed"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric error", attr.SlogMetricName(meterAckDrops), attr.SlogError(err))
	}

	return &Drainer{
		logger:     logger.With(attr.SlogComponent(name)),
		logMsg:     logMsg,
		queue:      make(chan batch, queueDepth),
		mu:         sync.RWMutex{},
		closed:     false,
		start:      sync.Once{},
		pending:    sync.WaitGroup{},
		workers:    sync.WaitGroup{},
		ctx:        ctx,
		cancel:     cancel,
		failures:   failures,
		drops:      drops,
		metricAttr: metric.WithAttributes(attr.Component(name)),

		lastLogUnixNano: atomic.Int64{},
		suppressedLogs:  atomic.Int64{},
	}
}

// Enqueue hands publish results to the drain pool and returns immediately. It
// never blocks: when the queue is full, or the drainer is closed, the results
// are dropped and counted and their outcome simply goes unobserved.
func (d *Drainer) Enqueue(ctx context.Context, results ...gcp.PublishResult) {
	if len(results) == 0 {
		return
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		d.drops.Add(ctx, int64(len(results)), d.metricAttr)
		return
	}

	d.start.Do(func() {
		for range drainWorkers {
			d.workers.Add(1)
			go d.drain()
		}
	})

	d.pending.Add(1)
	select {
	case d.queue <- batch{ctx: ctx, results: results}:
	default:
		d.pending.Done()
		d.drops.Add(ctx, int64(len(results)), d.metricAttr)
	}
}

func (d *Drainer) drain() {
	defer d.workers.Done()

	for b := range d.queue {
		d.observe(b)
	}
}

// observe resolves one batch and reports its failures. Get blocks until the
// broker acks; the publisher's PublishSettings.Timeout bounds that, and the
// drainer's context aborts it once Close gives up waiting.
func (d *Drainer) observe(b batch) {
	defer d.pending.Done()

	var firstErr error
	failed := 0
	for _, res := range b.results {
		if _, err := res.Get(d.ctx); err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if firstErr == nil {
		return
	}

	d.failures.Add(b.ctx, int64(failed), d.metricAttr)
	d.logFailure(b.ctx, firstErr, failed, len(b.results))
}

// logFailure emits at most one error log per logInterval across the whole
// pool, reporting how many logs it stood in for.
func (d *Drainer) logFailure(ctx context.Context, err error, failed int, total int) {
	now := time.Now().UnixNano()
	last := d.lastLogUnixNano.Load()

	// Claim the interval's log slot with a compare-and-swap so concurrent
	// workers emit one line between them rather than one each. A lost race
	// means another worker is logging right now, so this failure is counted as
	// suppressed and reported by whoever claims the next slot.
	if now-last < int64(logInterval) || !d.lastLogUnixNano.CompareAndSwap(last, now) {
		d.suppressedLogs.Add(1)
		return
	}

	d.logger.ErrorContext(ctx, d.logMsg,
		attr.SlogError(err),
		attr.SlogPubSubDrainFailedCount(failed),
		attr.SlogPubSubDrainBatchSize(total),
		attr.SlogPubSubDrainSuppressedLogs(d.suppressedLogs.Swap(0)),
	)
}

// Close stops accepting results, waits for the queued ones to resolve, and
// releases the pool. Once ctx expires the in-flight resolutions are aborted so
// shutdown stays bounded by the caller's deadline. It is safe to call twice.
func (d *Drainer) Close(ctx context.Context) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	close(d.queue)
	d.mu.Unlock()

	done := make(chan struct{})
	go func() {
		d.workers.Wait()
		close(done)
	}()

	select {
	case <-done:
		d.cancel()
		return nil
	case <-ctx.Done():
		// Cancelling unblocks every in-flight Get, so the workers exit
		// promptly and none outlive this call.
		d.cancel()
		<-done
		return fmt.Errorf("drain pubsub publish acks: %w", ctx.Err())
	}
}

// Wait blocks until every batch enqueued so far has been observed. It is a
// test-only synchronization barrier: callers must have already returned from
// the Enqueue whose drain they await, so that Add happens-before this Wait.
func (d *Drainer) Wait() {
	d.pending.Wait()
}
