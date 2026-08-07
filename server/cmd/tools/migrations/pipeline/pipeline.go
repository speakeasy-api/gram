// Package pipeline is a small, generic Source -> Transform -> Sink harness for
// moving records between arbitrary stores. Each stage is an interface so callers
// can plug in any origin, mapping, or destination; the concrete risk-findings
// backfill lives in the sibling riskfindings package.
//
// The three stages run concurrently and are wired by Run:
//
//	Source[A] --srcCh--> Transformer[A,B] --sink.Input()--> Sink[B]
//
// The Sink owns and exposes the buffered channel it consumes from; the transform
// stage is the sole producer to that channel and closes it once the source is
// exhausted, so the Sink's Run drains cleanly and flushes any partial batch.
package pipeline

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// Criteria is a free-form bag of query bounds (time range, cursor, tenant, page
// size, ...). Source implementations type-assert the keys they understand and
// ignore the rest, so new sources can define their own criteria without changing
// this package.
type Criteria map[string]any

// Source scans an origin, honoring criteria, and publishes each record to out.
// It returns once the origin is exhausted or ctx is cancelled. The implementation
// owns checkpoint bookkeeping (e.g. logging the last processed cursor).
//
// Read must NOT close out: the pipeline owns out and closes it after Read
// returns. Closing it from Read would double-close and panic.
type Source[T any] interface {
	Read(ctx context.Context, criteria Criteria, out chan<- T) error
}

// Transformer maps one source record to zero or more sink records. Returning an
// empty slice drops the record (e.g. filtered or invalid); returning several
// fans one record out to many.
type Transformer[A, B any] interface {
	Transform(ctx context.Context, in A) ([]B, error)
}

// Sink consumes records from the buffered channel it exposes via Input and
// writes them to a destination in batches. Run drains Input until it is closed,
// flushes the final partial batch, and returns.
//
// The pipeline closes Input only after the upstream stages finish successfully,
// so a closed channel means "all records delivered" — Run may safely flush its
// final batch on close. On an upstream failure the channel is NOT closed;
// instead ctx is cancelled, and Run must return on ctx.Done without flushing, so
// a failing run never performs a partial write. Run must therefore always select
// on ctx.Done alongside its input channel.
type Sink[T any] interface {
	Input() chan<- T
	Run(ctx context.Context) error
}

// Run wires the three stages together and blocks until every stage finishes or
// one of them errors. The first error cancels the shared context so the other
// stages unwind promptly. srcBuf sizes the buffer between the source and the
// transform stage; the sink owns the buffer on its own input side.
func Run[A, B any](ctx context.Context, src Source[A], tf Transformer[A, B], sink Sink[B], criteria Criteria, srcBuf int) error {
	if srcBuf < 0 {
		srcBuf = 0
	}

	g, ctx := errgroup.WithContext(ctx)
	srcCh := make(chan A, srcBuf)
	sinkCh := sink.Input()

	// Sink consumer: drains its own input channel and flushes batches.
	g.Go(func() error {
		if err := sink.Run(ctx); err != nil {
			return fmt.Errorf("sink: %w", err)
		}
		return nil
	})

	// Transform stage: the sole producer to the sink channel. It closes that
	// channel ONLY after the source channel is closed cleanly (source success),
	// so a closed sink channel unambiguously means "producer finished
	// successfully". The receive is cancellation-aware: because the source does
	// not close srcCh on error, a plain `range` would block forever, so the
	// transform selects on ctx.Done and unwinds without closing sinkCh, letting
	// the sink unwind via ctx.Done rather than mistaking the failure for EOF and
	// flushing a partial batch.
	g.Go(func() error {
		for {
			var (
				a  A
				ok bool
			)
			select {
			case <-ctx.Done():
				return fmt.Errorf("transform cancelled: %w", ctx.Err())
			case a, ok = <-srcCh:
			}
			if !ok {
				// srcCh is closed only on source success ⇒ clean end of stream.
				close(sinkCh)
				return nil
			}

			bs, err := tf.Transform(ctx, a)
			if err != nil {
				return fmt.Errorf("transform: %w", err)
			}
			for _, b := range bs {
				select {
				case sinkCh <- b:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	})

	// Source: publishes records, then closes srcCh ONLY on success. On error it
	// returns without closing, so the closed-channel signal never reaches the
	// transform on a failed read; the shared context cancels and every stage
	// unwinds via ctx.Done instead.
	g.Go(func() error {
		if err := src.Read(ctx, criteria, srcCh); err != nil {
			return fmt.Errorf("source: %w", err)
		}
		close(srcCh)
		return nil
	})

	if err := g.Wait(); err != nil {
		return fmt.Errorf("run pipeline: %w", err)
	}
	return nil
}
