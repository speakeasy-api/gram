// Package wide implements wide-event logging.
//
// A wide event collects structured attributes ([log/slog.Attr]) from
// multiple layers of a call stack into a single log line emitted once at
// the end. This replaces scattered per-layer log calls with one
// information-dense record that is easier to query and correlate.
//
// Usage follows a three-phase lifecycle:
//
//  1. An outer caller invokes [Start] to initialise the event on a
//     [context.Context].
//  2. Inner callees call [Push] to attach attributes as they become
//     available.
//  3. Once the work is complete, the outer caller invokes [Emit] to
//     collect every accumulated attribute and log them together.
//
// A prime use case is HTTP middleware: the logging middleware starts the
// event, handlers and auth layers push attributes throughout the request,
// and the middleware emits a single wide log line after the response is
// written.
//
// The underlying collection is a lock-free linked list, so [Push] may be
// called from multiple goroutines without synchronisation. In the common
// single-goroutine case the overhead is a single atomic store per push.
package wide
