package celeval

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"
	"github.com/stretchr/testify/require"
)

func TestCompilerLRU(t *testing.T) {
	t.Parallel()
	env, err := cel.NewEnv()
	require.NoError(t, err)
	c, err := New(env, Config{Capacity: 2})
	require.NoError(t, err)
	one, err := c.Compile("1")
	require.NoError(t, err)
	two, err := c.Compile("2")
	require.NoError(t, err)
	again, err := c.Compile("1")
	require.NoError(t, err)
	require.Same(t, one, again)
	_, err = c.Compile("3")
	require.NoError(t, err)
	require.Equal(t, 2, c.cache.Len())
	require.True(t, c.cache.Contains("1"))
	require.False(t, c.cache.Contains("2"))
	recompiled, err := c.Compile("2")
	require.NoError(t, err)
	require.NotSame(t, two, recompiled)
	// Eviction never invalidates a program already held by a caller.
	out, _, err := Eval(t.Context(), two, map[string]any{})
	require.NoError(t, err)
	require.Equal(t, types.Int(2), out)
}

func TestConcurrentMissesCompileOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		env, err := cel.NewEnv()
		require.NoError(t, err)
		var constructions atomic.Int64
		entered := make(chan struct{})
		release := make(chan struct{})
		c, err := New(env, Config{Capacity: 2}, cel.CustomDecorator(func(i interpreter.Interpretable) (interpreter.Interpretable, error) {
			if constructions.Add(1) == 1 {
				close(entered)
			}
			<-release
			return i, nil
		}))
		require.NoError(t, err)
		const callers = 32
		programs := make([]cel.Program, callers)
		errs := make([]error, callers)
		var wg sync.WaitGroup
		for i := range callers {
			wg.Go(func() { programs[i], errs[i] = c.Compile("true") })
		}
		<-entered
		// Every caller is blocked in compilation or in the shared flight before
		// releasing construction, so cache hits cannot hide duplicate cold work.
		synctest.Wait()
		close(release)
		wg.Wait()
		require.EqualValues(t, 1, constructions.Load())
		for i := range callers {
			require.NoError(t, errs[i])
			require.Same(t, programs[0], programs[i])
		}
	})
}

func TestEnvironmentIsolation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value bool
	}{
		{name: "true binding", value: true},
		{name: "false binding", value: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env, err := cel.NewEnv(cel.Function("verdict", cel.Overload("verdict_bool", nil, cel.BoolType,
				cel.FunctionBinding(func(...ref.Val) ref.Val { return types.Bool(tc.value) }))))
			require.NoError(t, err)
			c, err := New(env, Config{Capacity: 2, Predicate: true})
			require.NoError(t, err)
			prg, err := c.Compile("verdict()")
			require.NoError(t, err)
			got, err := EvalPredicate(t.Context(), prg, map[string]any{})
			require.NoError(t, err)
			require.Equal(t, tc.value, got)
		})
	}
}

func TestConcurrentActivations(t *testing.T) {
	t.Parallel()
	env, err := cel.NewEnv(cel.Variable("value", cel.IntType))
	require.NoError(t, err)
	c, err := New(env, Config{Capacity: 2, CostLimit: new(uint64(100))})
	require.NoError(t, err)
	prg, err := c.Compile("value + 1")
	require.NoError(t, err)
	const callers = 32
	results := make([]ref.Val, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			results[i], _, errs[i] = Eval(t.Context(), prg, map[string]any{"value": i})
		})
	}
	wg.Wait()
	for i := range callers {
		require.NoError(t, errs[i])
		require.Equal(t, types.Int(i+1), results[i])
	}
}

func TestCompileFailuresNotCached(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		expr string
		want error
	}{
		{name: "syntax", expr: `"sensitive" +`, want: ErrCompile},
		{name: "type", expr: `"sensitive" + 1`, want: ErrCompile},
		{name: "nonboolean", expr: `"sensitive"`, want: ErrResultType},
		{name: "dynamic", expr: `dyn(true)`, want: ErrResultType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env, err := cel.NewEnv()
			require.NoError(t, err)
			c, err := New(env, Config{Capacity: 2, Predicate: true})
			require.NoError(t, err)
			_, first := c.Compile(tc.expr)
			_, second := c.Compile(tc.expr)
			require.ErrorIs(t, first, tc.want)
			require.ErrorIs(t, second, ErrCompile)
			require.NotSame(t, first, second)
			require.NotContains(t, first.Error(), "sensitive")
			require.Zero(t, c.cache.Len())
		})
	}
}

func TestProgramConstructionFailure(t *testing.T) {
	t.Parallel()
	env, err := cel.NewEnv()
	require.NoError(t, err)
	var attempts atomic.Int64
	c, err := New(env, Config{Capacity: 2}, cel.CustomDecorator(func(i interpreter.Interpretable) (interpreter.Interpretable, error) {
		attempts.Add(1)
		return nil, errors.New("sensitive program diagnostic")
	}))
	require.NoError(t, err)
	for range 2 {
		_, err := c.Compile("true")
		require.ErrorIs(t, err, ErrCompile)
		require.EqualError(t, err, "CEL compilation failed")
	}
	require.EqualValues(t, 2, attempts.Load())
}

func TestRuntimeFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		expr string
		want error
	}{
		{name: "division", expr: "1 / 0 == 0", want: ErrEvaluation},
		{name: "missing input", expr: "private_input == 1", want: ErrEvaluation},
		{name: "nonboolean", expr: "1", want: ErrResultType},
		{name: "unknown", expr: "private_input", want: ErrResultType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env, err := cel.NewEnv(cel.Variable("private_input", cel.DynType))
			require.NoError(t, err)
			c, err := New(env, Config{Capacity: 2})
			require.NoError(t, err)
			prg, err := c.Compile(tc.expr)
			require.NoError(t, err)
			activation := map[string]any{}
			if tc.name == "unknown" {
				activation["private_input"] = types.NewUnknown(1, nil)
			}
			_, err = EvalPredicate(t.Context(), prg, activation)
			require.ErrorIs(t, err, tc.want)
			require.ErrorIs(t, err, ErrEvaluation)
			require.NotContains(t, err.Error(), "private_input")
		})
	}
}

func TestExpressionSize(t *testing.T) {
	t.Parallel()
	env, err := cel.NewEnv()
	require.NoError(t, err)
	c, err := New(env, Config{Capacity: 2, MaxExpressionBytes: 4})
	require.NoError(t, err)
	_, err = c.Compile(`"é"`)
	require.NoError(t, err)
	_, err = c.Compile(`"éé"`)
	require.ErrorIs(t, err, ErrExpressionSize)
	require.Equal(t, 1, c.cache.Len())
}

func TestCostConfigurationIsolation(t *testing.T) {
	t.Parallel()
	env, err := cel.NewEnv(cel.Variable("values", cel.ListType(cel.IntType)))
	require.NoError(t, err)
	limit := uint64(1)
	limited, err := New(env, Config{Capacity: 2, Predicate: true, CostLimit: &limit})
	require.NoError(t, err)
	limit = 10000 // Changing the caller's configuration cannot change the compiler.
	unlimited, err := New(env, Config{Capacity: 2, Predicate: true})
	require.NoError(t, err)
	const expr = "values.all(v, v > 0)"
	prg, err := limited.Compile(expr)
	require.NoError(t, err)
	_, err = EvalPredicate(t.Context(), prg, map[string]any{"values": []int{1, 2, 3}})
	require.ErrorIs(t, err, ErrCostLimit)
	prg, err = unlimited.Compile(expr)
	require.NoError(t, err)
	matched, err := EvalPredicate(t.Context(), prg, map[string]any{"values": []int{1, 2, 3}})
	require.NoError(t, err)
	require.True(t, matched)
}

func TestCanceledBeforeEvaluation(t *testing.T) {
	t.Parallel()
	env, err := cel.NewEnv()
	require.NoError(t, err)
	c, err := New(env, Config{Capacity: 2})
	require.NoError(t, err)
	prg, err := c.Compile("true")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = EvalPredicate(ctx, prg, map[string]any{})
	require.ErrorIs(t, err, ErrCanceled)
	require.ErrorIs(t, err, context.Canceled)
}

func TestExpiredContext(t *testing.T) {
	t.Parallel()
	env, err := cel.NewEnv()
	require.NoError(t, err)
	c, err := New(env, Config{Capacity: 2})
	require.NoError(t, err)
	prg, err := c.Compile("true")
	require.NoError(t, err)
	ctx, cancel := context.WithDeadline(t.Context(), time.Time{})
	defer cancel()
	_, err = EvalPredicate(ctx, prg, map[string]any{})
	require.ErrorIs(t, err, ErrCanceled)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestCancellationDuringComprehension(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls atomic.Int64
	env, err := cel.NewEnv(cel.Variable("values", cel.ListType(cel.IntType)),
		cel.Function("visit", cel.Overload("visit_int", []*cel.Type{cel.IntType}, cel.BoolType,
			cel.UnaryBinding(func(ref.Val) ref.Val {
				calls.Add(1)
				cancel()
				return types.True
			}))))
	require.NoError(t, err)
	c, err := New(env, Config{Capacity: 2})
	require.NoError(t, err)
	prg, err := c.Compile("values.all(v, visit(v))")
	require.NoError(t, err)
	values := make([]int, 1000)
	_, err = EvalPredicate(ctx, prg, map[string]any{"values": values})
	require.ErrorIs(t, err, ErrCanceled)
	require.ErrorIs(t, err, context.Canceled)
	require.Positive(t, calls.Load())
	require.Less(t, calls.Load(), int64(len(values)))
}

func TestCustomFunctionErrorRedaction(t *testing.T) {
	t.Parallel()
	env, err := cel.NewEnv(cel.Variable("input", cel.StringType),
		cel.Function("fail", cel.Overload("fail_string", []*cel.Type{cel.StringType}, cel.BoolType,
			cel.UnaryBinding(func(v ref.Val) ref.Val {
				return types.NewErr("private diagnostic: %s", v)
			}))))
	require.NoError(t, err)
	c, err := New(env, Config{Capacity: 2, Predicate: true})
	require.NoError(t, err)
	prg, err := c.Compile("fail(input)")
	require.NoError(t, err)
	_, err = EvalPredicate(t.Context(), prg, map[string]any{"input": "sensitive activation"})
	require.ErrorIs(t, err, ErrEvaluation)
	require.EqualError(t, err, "CEL evaluation failed")
	var diagnostic *Error
	require.ErrorAs(t, err, &diagnostic)
	require.Contains(t, diagnostic.Unwrap().Error(), "sensitive activation")
}

func TestZeroCostBudget(t *testing.T) {
	t.Parallel()
	env, err := cel.NewEnv(cel.Variable("value", cel.IntType))
	require.NoError(t, err)
	c, err := New(env, Config{Capacity: 2, CostLimit: new(uint64(0))})
	require.NoError(t, err)
	prg, err := c.Compile("value + 1")
	require.NoError(t, err)
	_, _, err = Eval(t.Context(), prg, map[string]any{"value": 1})
	require.ErrorIs(t, err, ErrCostLimit)
}

func TestOptionSliceIsolation(t *testing.T) {
	t.Parallel()
	env, err := cel.NewEnv(cel.Variable("value", cel.IntType))
	require.NoError(t, err)
	opts := []cel.ProgramOption{cel.CostLimit(0)}
	c, err := New(env, Config{Capacity: 2}, opts...)
	require.NoError(t, err)
	opts[0] = cel.CostLimit(10000)
	prg, err := c.Compile("value + 1")
	require.NoError(t, err)
	_, _, err = Eval(t.Context(), prg, map[string]any{"value": 1})
	require.ErrorIs(t, err, ErrCostLimit)
}

func TestInvalidConfiguration(t *testing.T) {
	t.Parallel()
	env, err := cel.NewEnv()
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		env    *cel.Env
		config Config
	}{
		{name: "nil environment", config: Config{Capacity: 1}},
		{name: "zero capacity", env: env},
		{name: "negative capacity", env: env, config: Config{Capacity: -1}},
		{name: "negative byte limit", env: env, config: Config{Capacity: 1, MaxExpressionBytes: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := New(tc.env, tc.config)
			require.Error(t, err)
		})
	}
}
