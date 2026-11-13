package mcpconnectionattempts

import (
	"context"
	"sync/atomic"
)

type failureMarkKey struct{}

// WithFailureMark gives ctx a mark that every failure recorded under it sets, so a request can
// tell whether anything it did failed before it records itself as served.
func WithFailureMark(ctx context.Context) context.Context {
	return context.WithValue(ctx, failureMarkKey{}, new(atomic.Bool))
}

// MarkFailed sets ctx's mark, for a failure that records no row of its own.
func MarkFailed(ctx context.Context) {
	if mark, ok := ctx.Value(failureMarkKey{}).(*atomic.Bool); ok {
		mark.Store(true)
	}
}

// Failed reports whether anything under ctx's mark failed.
func Failed(ctx context.Context) bool {
	mark, ok := ctx.Value(failureMarkKey{}).(*atomic.Bool)
	return ok && mark.Load()
}
