package wide_test

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/wide"
)

// ---------------------------------------------------------------------------
// Baseline: no synchronisation (unsafe under concurrency)
// ---------------------------------------------------------------------------

type baselineCtxKey string

const baselineKey baselineCtxKey = "baseline"

type baselineState struct {
	attrs []slog.Attr
}

func baselineStart(ctx context.Context, attrs ...slog.Attr) context.Context {
	ev := &baselineState{attrs: make([]slog.Attr, 0, len(attrs))}
	ev.attrs = append(ev.attrs, attrs...)
	return context.WithValue(ctx, baselineKey, ev)
}

func baselinePush(ctx context.Context, attrs ...slog.Attr) {
	ev, ok := ctx.Value(baselineKey).(*baselineState)
	if !ok {
		return
	}
	ev.attrs = append(ev.attrs, attrs...)
}

func baselineEmit(ctx context.Context) []slog.Attr {
	ev, ok := ctx.Value(baselineKey).(*baselineState)
	if !ok {
		return nil
	}
	return ev.attrs
}

// ---------------------------------------------------------------------------
// Mutex variant
// ---------------------------------------------------------------------------

type mutexCtxKey string

const mutexKey mutexCtxKey = "mutex"

type mutexState struct {
	mu    sync.Mutex
	attrs []slog.Attr
}

func mutexStart(ctx context.Context, attrs ...slog.Attr) context.Context {
	ev := &mutexState{attrs: make([]slog.Attr, 0, len(attrs))}
	ev.attrs = append(ev.attrs, attrs...)
	return context.WithValue(ctx, mutexKey, ev)
}

func mutexPush(ctx context.Context, attrs ...slog.Attr) {
	ev, ok := ctx.Value(mutexKey).(*mutexState)
	if !ok {
		return
	}
	ev.mu.Lock()
	ev.attrs = append(ev.attrs, attrs...)
	ev.mu.Unlock()
}

func mutexEmit(ctx context.Context) []slog.Attr {
	ev, ok := ctx.Value(mutexKey).(*mutexState)
	if !ok {
		return nil
	}
	ev.mu.Lock()
	out := make([]slog.Attr, len(ev.attrs))
	copy(out, ev.attrs)
	ev.mu.Unlock()
	return out
}

// ---------------------------------------------------------------------------
// Lock-free linked list: the real wide package implementation
// ---------------------------------------------------------------------------

// Defined as a variant so it sits alongside the others in benchmark output.
func lockfreeStart(ctx context.Context, attrs ...slog.Attr) context.Context {
	return wide.Start(ctx, attrs...)
}

func lockfreePush(ctx context.Context, attrs ...slog.Attr) {
	wide.Push(ctx, attrs...)
}

func lockfreeEmit(ctx context.Context) []slog.Attr {
	return wide.Emit(ctx)
}

// ---------------------------------------------------------------------------
// Benchmark helpers
// ---------------------------------------------------------------------------

var sinkAttrs []slog.Attr

type variant struct {
	name  string
	start func(context.Context, ...slog.Attr) context.Context
	push  func(context.Context, ...slog.Attr)
	emit  func(context.Context) []slog.Attr
}

var variants = []variant{
	{"Baseline", baselineStart, baselinePush, baselineEmit},
	{"Mutex", mutexStart, mutexPush, mutexEmit},
	{"LockFree", lockfreeStart, lockfreePush, lockfreeEmit},
}

func testAttrs(i int) []slog.Attr {
	return []slog.Attr{
		slog.String(testBenchmarkKey, "value"),
		slog.Int(testIndexKey, i),
	}
}

// ---------------------------------------------------------------------------
// Serial: Push N attrs then Emit (single goroutine, the common case)
// ---------------------------------------------------------------------------

func BenchmarkSerialPushEmit(b *testing.B) {
	const pushes = 10

	for _, v := range variants {
		b.Run(v.name, func(b *testing.B) {
			for b.Loop() {
				ctx := v.start(context.Background())
				for i := range pushes {
					v.push(ctx, testAttrs(i)...)
				}
				sinkAttrs = v.emit(ctx)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Serial: create an accumulator and Push a fixed batch (no Emit)
// ---------------------------------------------------------------------------

func BenchmarkSerialPush(b *testing.B) {
	const pushes = 10

	for _, v := range variants {
		b.Run(v.name, func(b *testing.B) {
			for b.Loop() {
				ctx := v.start(context.Background())
				for i := range pushes {
					v.push(ctx, testAttrs(i)...)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Concurrent: N goroutines each push a fixed batch
// ---------------------------------------------------------------------------

func BenchmarkConcurrentPush(b *testing.B) {
	goroutines := []int{2, 4, 8}
	const pushesPerGoroutine = 10

	for _, v := range variants {
		// Skip baseline under concurrency — it's unsafe and only here for
		// serial comparison.
		if v.name == "Baseline" {
			continue
		}
		for _, g := range goroutines {
			b.Run(v.name+"/goroutines="+itoa(g), func(b *testing.B) {
				for b.Loop() {
					ctx := v.start(context.Background())
					var wg sync.WaitGroup
					wg.Add(g)
					for worker := range g {
						go func() {
							defer wg.Done()
							for i := range pushesPerGoroutine {
								v.push(ctx, testAttrs(worker*pushesPerGoroutine+i)...)
							}
						}()
					}
					wg.Wait()
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Concurrent: interleaved Push + Emit
// ---------------------------------------------------------------------------

func BenchmarkConcurrentPushWithEmit(b *testing.B) {
	goroutines := []int{2, 4, 8}
	const pushesPerIter = 10 // typical attrs per request cycle

	for _, v := range variants {
		if v.name == "Baseline" {
			continue
		}
		for _, g := range goroutines {
			b.Run(v.name+"/goroutines="+itoa(g), func(b *testing.B) {
				for b.Loop() {
					// One fresh ctx per iteration models a request lifecycle and
					// keeps Emit cost bounded — otherwise the accumulator grows
					// across iterations and Emit becomes O(b.N).
					ctx := v.start(context.Background())

					var wg sync.WaitGroup
					wg.Add(g)
					for range g {
						go func() {
							defer wg.Done()
							var emitted []slog.Attr
							for i := range pushesPerIter {
								v.push(ctx, testAttrs(i)...)
								if i%5 == 0 {
									emitted = v.emit(ctx)
								}
							}
							runtime.KeepAlive(emitted)
						}()
					}
					wg.Wait()
					sinkAttrs = v.emit(ctx)
				}
			})
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
