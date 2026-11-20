package mcpconnectionattempts

import (
	"context"
	"sync/atomic"
)

type failureMarkKey struct{}

// failureMark is set by failures recorded under it.
type failureMark struct {
	failed atomic.Bool

	// call marks one gateway member call within a request.
	call bool
}

// WithFailureMark gives a request's ctx a mark that its failures set, so it can tell whether
// anything it did failed before it records itself as served. A failure tied to one gateway
// member, or recorded at a discovery step, does not set it.
func WithFailureMark(ctx context.Context) context.Context {
	return context.WithValue(ctx, failureMarkKey{}, &failureMark{failed: atomic.Bool{}, call: false})
}

// WithCallFailureMark gives one gateway member call its own mark. Every failure recorded under it
// is that call's, so it sets this mark and not the request's.
func WithCallFailureMark(ctx context.Context) context.Context {
	return context.WithValue(ctx, failureMarkKey{}, &failureMark{failed: atomic.Bool{}, call: true})
}

// MarkFailed sets ctx's innermost mark, for a failure that records no row of its own.
func MarkFailed(ctx context.Context) {
	if mark, ok := ctx.Value(failureMarkKey{}).(*failureMark); ok {
		mark.failed.Store(true)
	}
}

// markRecordedFailure sets ctx's innermost mark for a recorded failure, unless the mark is the
// request's and the failure is about one member.
func markRecordedFailure(ctx context.Context, memberOnly bool) {
	if mark, ok := ctx.Value(failureMarkKey{}).(*failureMark); ok && (mark.call || !memberOnly) {
		mark.failed.Store(true)
	}
}

// Failed reports whether anything under ctx's innermost mark failed.
func Failed(ctx context.Context) bool {
	mark, ok := ctx.Value(failureMarkKey{}).(*failureMark)
	return ok && mark.failed.Load()
}
