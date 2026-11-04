// Package shutdowndrain tells in-flight MCP requests that the server is
// shutting down, so they can end in a way their clients understand before the
// HTTP server's graceful shutdown deadline cancels them and closes their
// connections.
package shutdowndrain

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrInterrupted is the cause given to work the drain interrupted because it
// was still running when the interrupt deadline passed.
var ErrInterrupted = errors.New("interrupted by server shutdown")

// Interrupted reports whether ctx was cancelled by the drain's interrupt
// deadline, as opposed to a client disconnect or another cancellation.
func Interrupted(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), ErrInterrupted)
}

// Drain runs in two phases once the server starts shutting down.
//
// When it starts, streams that exist only to carry server-initiated messages
// can end, so their clients reconnect to a replica that is still serving.
//
// When the interrupt deadline passes, work still waiting on a remote MCP
// server, a function runner, or an HTTP tool's server is abandoned and its
// request answered with an error, leaving the rest of the server's shutdown
// window to write those answers and record them.
//
// A nil *Drain never starts, so code built without one behaves as if the
// server never shuts down.
type Drain struct {
	// started is closed when the drain starts.
	started chan struct{}

	// interrupted is closed when the interrupt deadline passes.
	interrupted chan struct{}

	// startOnce makes Start idempotent.
	startOnce sync.Once
}

// New returns a drain that has not started.
func New() *Drain {
	return &Drain{started: make(chan struct{}), interrupted: make(chan struct{}), startOnce: sync.Once{}}
}

// Start begins the drain and arms the interrupt deadline interruptAfter from
// now. Only the first call has an effect.
func (d *Drain) Start(interruptAfter time.Duration) {
	if d == nil {
		return
	}
	d.startOnce.Do(func() {
		close(d.started)
		time.AfterFunc(interruptAfter, func() { close(d.interrupted) })
	})
}

// Started reports whether the drain has started.
func (d *Drain) Started() bool {
	if d == nil {
		return false
	}
	select {
	case <-d.started:
		return true
	default:
		return false
	}
}

// AfterStart runs f in its own goroutine once the drain starts, or right away
// when it already has. Calling the returned stop function, exactly once,
// releases the goroutine; f may still run if the drain starts at the same
// moment, so f must tolerate work that already ended.
func (d *Drain) AfterStart(f func()) (stop func()) {
	if d == nil {
		return func() {}
	}
	return runOnClose(d.started, f)
}

// AfterInterrupt is [Drain.AfterStart] for the interrupt deadline.
func (d *Drain) AfterInterrupt(f func()) (stop func()) {
	if d == nil {
		return func() {}
	}
	return runOnClose(d.interrupted, f)
}

// Interruptible returns a context derived from ctx that the interrupt
// deadline cancels with [ErrInterrupted] as its cause, and a function that
// releases it and must be called exactly once. Run the work that may be
// abandoned under the returned context and check [Interrupted] on it before
// calling release, since release cancels it too.
func (d *Drain) Interruptible(ctx context.Context) (context.Context, func()) {
	if d == nil {
		return ctx, func() {}
	}

	interruptibleCtx, cancel := context.WithCancelCause(ctx)
	stop := runOnClose(d.interrupted, func() { cancel(ErrInterrupted) })

	return interruptibleCtx, func() {
		stop()
		cancel(nil)
	}
}

// runOnClose runs f in its own goroutine when ch closes, unless the returned
// stop function is called first. stop must be called exactly once.
func runOnClose(ch <-chan struct{}, f func()) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			// When ch closed and stop was called before this goroutine ran,
			// both cases are ready and select picks one at random, so a
			// stop that preceded the close must be honoured here.
			select {
			case <-done:
				return
			default:
			}
			f()
		case <-done:
		}
	}()
	return func() { close(done) }
}
