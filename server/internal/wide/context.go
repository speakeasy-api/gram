package wide

import (
	"context"
	"log/slog"
	"slices"
	"sync/atomic"
)

type ctxKey struct{}

var eventCtxKey ctxKey

type node struct {
	attrs []slog.Attr
	next  atomic.Pointer[node]
}

type ctxState struct {
	head atomic.Pointer[node]
}

func Start(ctx context.Context, attrs ...slog.Attr) context.Context {
	ev := &ctxState{head: atomic.Pointer[node]{}}
	if len(attrs) > 0 {
		n := &node{attrs: attrs, next: atomic.Pointer[node]{}}
		ev.head.Store(n)
	}

	return context.WithValue(ctx, eventCtxKey, ev)
}

func Push(ctx context.Context, attrs ...slog.Attr) {
	ev, ok := ctx.Value(eventCtxKey).(*ctxState)
	if !ok {
		return
	}

	n := &node{attrs: attrs, next: atomic.Pointer[node]{}}
	for {
		old := ev.head.Load()
		n.next.Store(old)
		if ev.head.CompareAndSwap(old, n) {
			return
		}
	}
}

func Emit(ctx context.Context) []slog.Attr {
	ev, ok := ctx.Value(eventCtxKey).(*ctxState)
	if !ok {
		return nil
	}

	var nodes []*node
	total := 0
	for n := ev.head.Load(); n != nil; n = n.next.Load() {
		nodes = append(nodes, n)
		total += len(n.attrs)
	}

	// List is in prepend-order (newest first). Reverse node pointers so the
	// forward walk yields attrs in oldest-first order while preserving the
	// attr order within each Push call.
	slices.Reverse(nodes)
	out := make([]slog.Attr, 0, total)
	for _, n := range nodes {
		out = append(out, n.attrs...)
	}

	return out
}
