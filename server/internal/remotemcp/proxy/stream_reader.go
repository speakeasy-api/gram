package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// streamReader wraps a streaming upstream response body and enforces a
// per-event idle timeout. The clock resets on every successful Read from
// the underlying body — any byte-level activity (events, SSE keepalive
// comments) keeps the stream alive — and only inactivity longer than
// timeout terminates the stream. When the idle timer fires, cancelFunc
// is called, which closes the underlying body and causes any in-flight
// Read to return an error. Callers can distinguish idle terminations
// from clean EOF or context cancellation via IdleTimedOut.
type streamReader struct {
	// cancelFunc aborts the in-flight upstream request — invoked by the
	// idle timer when it fires, and by Close as a safety net so the
	// request context is released even if the body close fails.
	cancelFunc context.CancelFunc

	// closedOnce makes Close idempotent so callers can defer Close
	// without double-closing the underlying body or double-cancelling
	// the request context.
	closedOnce atomic.Bool

	// idleFired is set by the idle timer's AfterFunc before it invokes
	// cancelFunc, so IdleTimedOut can distinguish idle timeout from
	// other sources of Read failure.
	idleFired atomic.Bool

	// readCloser is the upstream response body being relayed.
	readCloser io.ReadCloser

	// timeout is the idle window; the timer is reset to this duration
	// on every successful Read.
	timeout time.Duration

	// timer fires cancelFunc after timeout elapses without Read
	// activity, terminating an idle stream.
	timer *time.Timer
}

func newStreamReader(inner io.ReadCloser, timeout time.Duration, cancel context.CancelFunc) *streamReader {
	r := &streamReader{
		cancelFunc: cancel,
		closedOnce: atomic.Bool{},
		idleFired:  atomic.Bool{},
		readCloser: inner,
		timeout:    timeout,
		timer:      nil, // assigned below; AfterFunc closure references r
	}
	r.timer = time.AfterFunc(timeout, func() {
		r.idleFired.Store(true)
		cancel()
	})
	return r
}

// Close stops the idle timer and closes the underlying body. cancel is
// called as a safety net so the request context is released even if the
// underlying close fails. Safe to call more than once; subsequent calls
// are no-ops.
func (r *streamReader) Close() error {
	r.timer.Stop()

	if r.closedOnce.Swap(true) {
		return nil
	}

	err := r.readCloser.Close()
	r.cancelFunc()

	if err != nil {
		return fmt.Errorf("close upstream stream body: %w", err)
	}

	return nil
}

// IdleTimedOut reports whether the idle timer has fired during this
// stream's lifetime. Used by relay callers to classify Read errors.
func (r *streamReader) IdleTimedOut() bool {
	return r.idleFired.Load()
}

// Read forwards to the underlying body verbatim — including io.EOF, which
// the SSE parser relies on as the clean end-of-stream signal. Any
// byte-level activity (n>0) resets the idle timer for another timeout
// window; on EOF the idle timer is stopped immediately so its goroutine
// does not linger until Close. Read errors caused by the timer firing
// surface as the underlying body's close error rather than as a synthetic
// timeout.
func (r *streamReader) Read(p []byte) (int, error) {
	n, err := r.readCloser.Read(p)

	if n > 0 {
		r.timer.Reset(r.timeout)
	}

	if errors.Is(err, io.EOF) {
		r.timer.Stop()
	}

	return n, err //nolint:wrapcheck // preserve io.EOF sentinel for SSE parser
}
