package customruleanalyzer

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/celeval"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

func TestEvaluator_ReusesCompiledProgram(t *testing.T) {
	t.Parallel()
	e, err := newEvaluator(8)
	require.NoError(t, err)
	const expr = `content.matchRegex("secret")`
	first, err := e.compiler.Compile(expr)
	require.NoError(t, err)
	second, err := e.compiler.Compile(expr)
	require.NoError(t, err)
	require.Same(t, first, second)
	spans, matched, err := e.execute(expr, celenv.Message{Content: "a secret", Type: "user_message"})
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, []celenv.Span{{Target: "content", Start: 2, End: 8, Value: "secret"}}, spans)
	_, matched, err = e.execute(`content.matchRegex("other")`, celenv.Message{Content: "a secret"})
	require.NoError(t, err)
	require.False(t, matched)
}

func TestEvaluator_ConcurrentSpanIsolation(t *testing.T) {
	t.Parallel()
	e, err := newEvaluator(8)
	require.NoError(t, err)
	const goroutines = 32
	errs := make([]error, goroutines)
	spans := make([][]celenv.Span, goroutines)
	matches := make([]bool, goroutines)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() {
			spans[i], matches[i], errs[i] = e.execute(`content.matchRegex("value[0-9]+")`,
				celenv.Message{Content: fmt.Sprintf("value%d", i), Type: "user_message"})
		})
	}
	wg.Wait()
	for i := range goroutines {
		require.NoError(t, errs[i])
		require.True(t, matches[i])
		value := fmt.Sprintf("value%d", i)
		require.Equal(t, []celenv.Span{{Target: "content", Start: 0, End: len(value), Value: value}}, spans[i])
	}
}

func TestEvaluator_FailureHandling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		expr    string
		wantErr error
	}{
		{name: "compile", expr: `private_invalid_name`, wantErr: celeval.ErrCompile},
		{name: "boolean", expr: `42`, wantErr: celeval.ErrResultType},
		{name: "runtime", expr: `1 / 0 == 0`, wantErr: celeval.ErrEvaluation},
		{name: "invalid regex is nonmatch", expr: `content.matchRegex("[")`},
		{name: "false discards spans", expr: `content.matchText("abc") && false`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, err := newEvaluator(8)
			require.NoError(t, err)
			spans, matched, err := e.execute(tc.expr, celenv.Message{Content: "abc"})
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.NotContains(t, err.Error(), tc.expr)
			} else {
				require.NoError(t, err)
			}
			require.False(t, matched)
			require.Empty(t, spans)
		})
	}
}
