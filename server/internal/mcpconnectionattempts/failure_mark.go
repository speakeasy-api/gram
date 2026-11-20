package mcpconnectionattempts

import (
	"context"
	"sync/atomic"
)

type failureMarkKey struct{}

// failureMark is set by every failure recorded under it, and passes the failure to the mark it nests in.
type failureMark struct {
	failed atomic.Bool
	parent *failureMark
}

// WithFailureMark gives ctx a mark that every failure recorded under it sets, so a request, or one
// call within it, can tell whether anything it did failed before it records itself as served.
// A mark nested in another sets both.
func WithFailureMark(ctx context.Context) context.Context {
	parent, _ := ctx.Value(failureMarkKey{}).(*failureMark)
	return context.WithValue(ctx, failureMarkKey{}, &failureMark{failed: atomic.Bool{}, parent: parent})
}

// MarkFailed sets ctx's marks, for a failure that records no row of its own.
func MarkFailed(ctx context.Context) {
	mark, _ := ctx.Value(failureMarkKey{}).(*failureMark)
	for ; mark != nil; mark = mark.parent {
		mark.failed.Store(true)
	}
}

// Failed reports whether anything under ctx's innermost mark failed.
func Failed(ctx context.Context) bool {
	mark, ok := ctx.Value(failureMarkKey{}).(*failureMark)
	return ok && mark.failed.Load()
}
