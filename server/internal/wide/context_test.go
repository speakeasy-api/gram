package wide_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/wide"
)

const (
	testEventKey      = "event"
	testSchemeKey     = "scheme"
	testOrgIDKey      = "org.id"
	testOrgSlugKey    = "org.slug"
	testMethodKey     = "method"
	testURLKey        = "url"
	testStatusCodeKey = "status_code"
	testGenericKey    = "k"
	testAKey          = "a"
	testBKey          = "b"
	testCKey          = "c"
	testOneKey        = "one"
	testTwoKey        = "two"
	testExtraKey      = "extra"
	testIndexKey      = "i"
	testBenchmarkKey  = "key"
	testValueKey      = "v"
)

func TestEmitOrdering(t *testing.T) {
	t.Parallel()

	ctx := wide.Start(t.Context(), slog.String(testEventKey, "WideEvent"))
	wide.Push(ctx,
		slog.String(testSchemeKey, "https"),
		slog.String(testOrgIDKey, "org_123"),
		slog.String(testOrgSlugKey, "acme"),
	)
	wide.Push(ctx,
		slog.String(testMethodKey, "GET"),
		slog.String(testURLKey, "/v1/widgets"),
		slog.Int(testStatusCodeKey, 200),
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

func TestContains(t *testing.T) {
	t.Parallel()

	ctx := wide.Start(t.Context(), slog.String(testAKey, "1"))
	wide.Push(ctx, slog.String(testBKey, "2"))

	require.True(t, wide.Contains(ctx, testAKey))
	require.True(t, wide.Contains(ctx, testBKey))
	require.False(t, wide.Contains(ctx, testCKey))
	require.False(t, wide.Contains(t.Context(), testAKey))
}

func TestPushNoStart(t *testing.T) {
	t.Parallel()
	// No panic, no observable error. Push on a ctx without Start is a no-op.
	wide.Push(t.Context(), slog.String(testGenericKey, "v"))
}

func TestEmitIsRepeatable(t *testing.T) {
	t.Parallel()

	ctx := wide.Start(t.Context(), slog.String(testAKey, "1"))
	wide.Push(ctx, slog.String(testBKey, "2"))
	first := wide.Emit(ctx)
	wide.Push(ctx, slog.String(testCKey, "3"))
	second := wide.Emit(ctx)

	require.Equal(t, []string{"a", "b"}, keysOf(first), "first snapshot must not see later pushes")
	require.Equal(t, []string{"a", "b", "c"}, keysOf(second))
}

func TestStartWithAttrsNoPush(t *testing.T) {
	t.Parallel()

	ctx := wide.Start(t.Context(), slog.String(testAKey, "1"), slog.String(testBKey, "2"))
	require.Equal(t, []string{"a", "b"}, keysOf(wide.Emit(ctx)))
}

func TestPushEmptyVariadic(t *testing.T) {
	t.Parallel()

	ctx := wide.Start(t.Context(), slog.String(testAKey, "1"))
	wide.Push(ctx)
	require.Equal(t, []string{"a"}, keysOf(wide.Emit(ctx)))
}

func TestCancelledContextEmitStillWorks(t *testing.T) {
	t.Parallel()

	parent := wide.Start(t.Context(), slog.String(testAKey, "1"))
	ctx, cancel := context.WithCancel(parent)
	wide.Push(ctx, slog.String(testBKey, "2"))
	cancel()

	require.Equal(t, []string{"a", "b"}, keysOf(wide.Emit(ctx)))
}

func TestDerivedContextSharesState(t *testing.T) {
	t.Parallel()

	type otherKey struct{}
	parent := wide.Start(t.Context(), slog.String(testAKey, "1"))
	child := context.WithValue(parent, otherKey{}, "v")
	wide.Push(child, slog.String(testBKey, "2"))

	// The *ctxState pointer is shared across context.WithValue layers, so a
	// Push on the child is visible from the parent.
	require.Equal(t, []string{"a", "b"}, keysOf(wide.Emit(parent)))
	require.Equal(t, []string{"a", "b"}, keysOf(wide.Emit(child)))
}

func TestIndependentStartsAreIsolated(t *testing.T) {
	t.Parallel()

	ctx1 := wide.Start(t.Context(), slog.String(testOneKey, "1"))
	ctx2 := wide.Start(t.Context(), slog.String(testTwoKey, "2"))
	wide.Push(ctx1, slog.String(testExtraKey, "e"))

	require.Equal(t, []string{"one", "extra"}, keysOf(wide.Emit(ctx1)))
	require.Equal(t, []string{"two"}, keysOf(wide.Emit(ctx2)))
}

func TestLargeNumberOfPushes(t *testing.T) {
	t.Parallel()

	const n = 10_000
	ctx := wide.Start(t.Context())
	for i := range n {
		wide.Push(ctx, slog.Int(testIndexKey, i))
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
		attrs[i] = slog.Int(testIndexKey, i)
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
