package proxy

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// drainRetryMin and drainRetryJitter set the SSE retry delay sent when a
	// listen stream closes for shutdown. Clients wait that long before
	// reconnecting. The jitter spreads the reconnects from one draining
	// replica across two seconds instead of landing them all at once.
	drainRetryMin    = time.Second
	drainRetryJitter = 2 * time.Second
)

// Drain tells relayed streams that the server is shutting down, in two phases.
//
// When it starts, streams that exist only to carry server-initiated messages
// (the standalone GET stream and the 2026-07-28 subscriptions/listen stream)
// end, so clients reconnect to a replica that is still serving, and POST
// streams end as soon as their response has been relayed.
//
// When the interrupt deadline passes, POST requests still waiting on the
// remote MCP server are answered with a JSON-RPC error, so their clients learn
// the outcome instead of waiting out their own timeout.
//
// A nil *Drain never starts, so proxies built without one relay as before.
type Drain struct {
	// started is closed when the drain starts.
	started chan struct{}

	// interrupted is closed when the interrupt deadline passes.
	interrupted chan struct{}

	// startOnce makes Start idempotent.
	startOnce sync.Once
}

// NewDrain returns a drain that has not started.
func NewDrain() *Drain {
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

// isStarted reports whether the drain has started.
func (d *Drain) isStarted() bool {
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

// afterStart runs f in its own goroutine once the drain starts, or right away
// when it already has. Calling the returned stop function, exactly once,
// releases the goroutine; f may still run if the drain starts at the same
// moment, so f must tolerate a stream that already ended.
func (d *Drain) afterStart(f func()) (stop func()) {
	if d == nil {
		return func() {}
	}
	return runOnClose(d.started, f)
}

// afterInterrupt is [Drain.afterStart] for the interrupt deadline.
func (d *Drain) afterInterrupt(f func()) (stop func()) {
	if d == nil {
		return func() {}
	}
	return runOnClose(d.interrupted, f)
}

// runOnClose runs f in its own goroutine when ch closes, unless the returned
// stop function is called first.
func runOnClose(ch <-chan struct{}, f func()) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			f()
		case <-done:
		}
	}()
	return func() { close(done) }
}

// writeSSERetry ends a stream closed for shutdown with an SSE retry field, the
// polling signal MCP 2025-11-25 asks for when a server closes a connection the
// client should reopen. Clients on earlier revisions reconnect regardless; the
// field only sets their delay. No event id is written, because ids must stay
// unique within the client's session and only the upstream assigns them.
func writeSSERetry(w http.ResponseWriter) (int64, error) {
	delay := drainRetryMin + time.Duration(rand.Int64N(int64(drainRetryJitter))) // #nosec G404 -- reconnect jitter is not security-sensitive
	return writeSSEBytes(w, []byte("retry: "+strconv.FormatInt(delay.Milliseconds(), 10)+"\n\n"))
}

// writeSSEBytes writes one complete SSE frame and flushes it to the client.
func writeSSEBytes(w http.ResponseWriter, frame []byte) (int64, error) {
	n, err := w.Write(frame)
	if err != nil {
		return int64(n), fmt.Errorf("write sse frame: %w", err)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return int64(n), nil
}
