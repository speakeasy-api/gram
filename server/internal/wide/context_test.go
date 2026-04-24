package wide_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/wide"
)

func TestEmitOrdering(t *testing.T) {
	t.Parallel()

	ctx := wide.Start(t.Context(), slog.String("event", "WideEvent"))
	wide.Push(ctx,
		slog.String("scheme", "https"),
		slog.String("org.id", "org_123"),
		slog.String("org.slug", "acme"),
	)
	wide.Push(ctx,
		slog.String("method", "GET"),
		slog.String("url", "/v1/widgets"),
		slog.Int("status_code", 200),
	)

	want := []string{
		"event",
		"scheme", "org.id", "org.slug",
		"method", "url", "status_code",
	}
	require.Equal(t, want, keysOf(wide.Emit(ctx)))
}

func TestEmitNoStart(t *testing.T) {
	t.Parallel()
	require.Nil(t, wide.Emit(t.Context()))
}

func TestEmitEmpty(t *testing.T) {
	t.Parallel()

	got := wide.Emit(wide.Start(t.Context()))
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestPushNoStart(t *testing.T) {
	t.Parallel()
	// No panic, no observable error. Push on a ctx without Start is a no-op.
	wide.Push(t.Context(), slog.String("k", "v"))
}

func TestEmitIsRepeatable(t *testing.T) {
	t.Parallel()

	ctx := wide.Start(t.Context(), slog.String("a", "1"))
	wide.Push(ctx, slog.String("b", "2"))
	first := wide.Emit(ctx)
	wide.Push(ctx, slog.String("c", "3"))
	second := wide.Emit(ctx)

	require.Equal(t, []string{"a", "b"}, keysOf(first), "first snapshot must not see later pushes")
	require.Equal(t, []string{"a", "b", "c"}, keysOf(second))
}

func TestStartWithAttrsNoPush(t *testing.T) {
	t.Parallel()

	ctx := wide.Start(t.Context(), slog.String("a", "1"), slog.String("b", "2"))
	require.Equal(t, []string{"a", "b"}, keysOf(wide.Emit(ctx)))
}

func TestPushEmptyVariadic(t *testing.T) {
	t.Parallel()

	ctx := wide.Start(t.Context(), slog.String("a", "1"))
	wide.Push(ctx)
	require.Equal(t, []string{"a"}, keysOf(wide.Emit(ctx)))
}

func TestPushedAttrsAliasing(t *testing.T) {
	t.Parallel()

	// Characterization: Push stores the caller's slice header by reference, so
	// post-Push mutation of the caller-owned slice is visible through Emit.
	// design.md describes attrs as "fixed at allocation" but that is an internal
	// invariant, not a caller-facing contract. Update this test if Push ever
	// copies.
	ctx := wide.Start(t.Context())
	attrs := []slog.Attr{slog.String("k", "original")}
	wide.Push(ctx, attrs...)
	attrs[0] = slog.String("k", "mutated")

	got := wide.Emit(ctx)
	require.Len(t, got, 1)
	require.Equal(t, "mutated", got[0].Value.String())
}

func TestCancelledContextEmitStillWorks(t *testing.T) {
	t.Parallel()

	parent := wide.Start(t.Context(), slog.String("a", "1"))
	ctx, cancel := context.WithCancel(parent)
	wide.Push(ctx, slog.String("b", "2"))
	cancel()

	require.Equal(t, []string{"a", "b"}, keysOf(wide.Emit(ctx)))
}

func TestDerivedContextSharesState(t *testing.T) {
	t.Parallel()

	type otherKey struct{}
	parent := wide.Start(t.Context(), slog.String("a", "1"))
	child := context.WithValue(parent, otherKey{}, "v")
	wide.Push(child, slog.String("b", "2"))

	// The *ctxState pointer is shared across context.WithValue layers, so a
	// Push on the child is visible from the parent.
	require.Equal(t, []string{"a", "b"}, keysOf(wide.Emit(parent)))
	require.Equal(t, []string{"a", "b"}, keysOf(wide.Emit(child)))
}

func TestIndependentStartsAreIsolated(t *testing.T) {
	t.Parallel()

	ctx1 := wide.Start(t.Context(), slog.String("one", "1"))
	ctx2 := wide.Start(t.Context(), slog.String("two", "2"))
	wide.Push(ctx1, slog.String("extra", "e"))

	require.Equal(t, []string{"one", "extra"}, keysOf(wide.Emit(ctx1)))
	require.Equal(t, []string{"two"}, keysOf(wide.Emit(ctx2)))
}

func TestLargeNumberOfPushes(t *testing.T) {
	t.Parallel()

	const n = 10_000
	ctx := wide.Start(t.Context())
	for i := range n {
		wide.Push(ctx, slog.Int("i", i))
	}

	got := wide.Emit(ctx)
	require.Len(t, got, n)
	for i := range n {
		require.Equal(t, int64(i), got[i].Value.Int64(), "position %d", i)
	}
}

func TestLargeBatchPerPush(t *testing.T) {
	t.Parallel()

	const n = 1_000
	attrs := make([]slog.Attr, n)
	for i := range n {
		attrs[i] = slog.Int("i", i)
	}

	ctx := wide.Start(t.Context())
	wide.Push(ctx, attrs...)

	got := wide.Emit(ctx)
	require.Len(t, got, n)
	for i := range n {
		require.Equal(t, int64(i), got[i].Value.Int64(), "position %d", i)
	}
}

func keysOf(attrs []slog.Attr) []string {
	out := make([]string, len(attrs))
	for i, a := range attrs {
		out[i] = a.Key
	}
	return out
}
