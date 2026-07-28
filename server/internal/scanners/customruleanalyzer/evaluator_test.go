package customruleanalyzer

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

func TestEvaluator_ReusesCompiledProgram(t *testing.T) {
	t.Parallel()

	e, err := newEvaluator(8)
	require.NoError(t, err)

	msg := celenv.Message{Content: "here is a secret value", Type: "user_message", Tools: nil}
	const expr = `content.matchRegex("secret")`

	_, matched, err := e.execute(expr, msg)
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, 1, e.cache.Len())

	// Second call with the same expression is a cache hit: no new entry.
	_, matched, err = e.execute(expr, msg)
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, 1, e.cache.Len())

	// A distinct expression compiles and caches under its own key.
	_, _, err = e.execute(`content.matchRegex("other")`, msg)
	require.NoError(t, err)
	require.Equal(t, 2, e.cache.Len())
}

// Concurrent cold misses for the same expression must resolve to a single cached
// program (no duplicate compilations, no races). Run with -race to catch unsafe
// access to the engine or cache.
func TestEvaluator_ConcurrentMissesCompileOnce(t *testing.T) {
	t.Parallel()

	e, err := newEvaluator(8)
	require.NoError(t, err)

	msg := celenv.Message{Content: "here is a secret value", Type: "user_message", Tools: nil}
	const expr = `content.matchRegex("secret")`

	// Each goroutine writes its own slot, so the slices are race-free without a
	// mutex, and require assertions run on the test goroutine after Wait.
	const goroutines = 32
	errs := make([]error, goroutines)
	matches := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			_, matched, err := e.execute(expr, msg)
			errs[i] = err
			matches[i] = matched
		}(i)
	}
	wg.Wait()

	for i := range goroutines {
		require.NoErrorf(t, errs[i], "goroutine %d", i)
		require.Truef(t, matches[i], "goroutine %d", i)
	}

	// A single expression yields a single cache entry regardless of contention.
	require.Equal(t, 1, e.cache.Len())
}

func TestEvaluator_EvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()

	e, err := newEvaluator(2)
	require.NoError(t, err)

	msg := celenv.Message{Content: "abc", Type: "user_message", Tools: nil}
	for _, expr := range []string{
		`content.matchRegex("a")`,
		`content.matchRegex("b")`,
		`content.matchRegex("c")`,
	} {
		_, _, err := e.execute(expr, msg)
		require.NoError(t, err)
	}

	// Capacity is 2, so the oldest entry has been evicted.
	require.Equal(t, 2, e.cache.Len())
}

func TestEvaluator_CompileErrorNotCached(t *testing.T) {
	t.Parallel()

	e, err := newEvaluator(8)
	require.NoError(t, err)

	msg := celenv.Message{Content: "abc", Type: "user_message", Tools: nil}
	_, _, err = e.execute(`this is not valid cel !!!`, msg)
	require.Error(t, err)
	require.Equal(t, 0, e.cache.Len())
}
