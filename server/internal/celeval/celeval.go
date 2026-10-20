// Package celeval provides environment-bound CEL program caching and evaluation.
package celeval

import (
	"context"
	"errors"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"
	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"
)

var (
	// ErrCompile identifies parsing, type checking, or program construction failures.
	ErrCompile = errors.New("CEL compilation failed")

	// ErrEvaluation identifies runtime failures, including unexpected result types.
	ErrEvaluation = errors.New("CEL evaluation failed")

	// ErrCostLimit identifies exhaustion of the interpreter cost budget.
	ErrCostLimit = errors.New("CEL evaluation cost limit exceeded")

	// ErrCanceled identifies canceled or expired evaluation contexts.
	ErrCanceled = errors.New("CEL evaluation canceled")

	// ErrExpressionSize identifies expressions exceeding the configured byte limit.
	ErrExpressionSize = errors.New("CEL expression size limit exceeded")

	// ErrResultType identifies a non-boolean predicate.
	ErrResultType = errors.New("CEL predicate must return bool")
)

// Error redacts CEL diagnostics from its default message. Unwrap explicitly
// exposes the underlying diagnostic, which may contain expressions or inputs;
// callers must not log it without their own disclosure policy.
type Error struct {
	kind  error
	cause error
}

func (e *Error) Error() string        { return e.kind.Error() }
func (e *Error) Unwrap() error        { return e.cause }
func (e *Error) Is(target error) bool { return target == e.kind }

// Config fixes the policy for the lifetime of a compiler.
type Config struct {
	// Capacity is the positive maximum number of cached programs.
	Capacity int

	// Predicate requires a statically boolean result. False permits any CEL type.
	Predicate bool

	// MaxExpressionBytes rejects oversized source before parsing. Zero disables
	// this additional limit; CEL's own parser limits still apply.
	MaxExpressionBytes int

	// CostLimit enables runtime cost accounting when non-nil, including a zero
	// budget. The value is copied at construction, not retained by reference.
	CostLimit *uint64
}

// Compiler owns a bounded, concurrency-safe LRU of successful programs. Failed
// compilations are shared by concurrent waiters but never cached. It retains no
// activations or evaluation results. A zero Compiler is not usable.
type Compiler struct {
	env                *cel.Env
	opts               []cel.ProgramOption
	predicate          bool
	maxExpressionBytes int
	cache              *lru.Cache[string, cel.Program]
	flight             singleflight.Group
}

// interruptCheckFrequency checks context cancellation every 100 comprehension
// iterations, balancing cancellation responsiveness with interpreter overhead.
const interruptCheckFrequency = 100

// New binds a cache to one environment and program configuration. The environment
// is extended into a private copy and the option slice is copied. Custom type
// providers, function bindings and option closures must remain immutable and
// concurrency-safe; they must not capture request-specific state.
func New(env *cel.Env, cfg Config, opts ...cel.ProgramOption) (*Compiler, error) {
	if env == nil || cfg.Capacity <= 0 || cfg.MaxExpressionBytes < 0 {
		return nil, errors.New("invalid CEL compiler configuration")
	}
	privateEnv, err := env.Extend()
	if err != nil {
		return nil, &Error{kind: ErrCompile, cause: err}
	}
	cache, err := lru.New[string, cel.Program](cfg.Capacity)
	if err != nil {
		return nil, &Error{kind: ErrCompile, cause: err}
	}
	options := append([]cel.ProgramOption{cel.InterruptCheckFrequency(interruptCheckFrequency)}, opts...)
	if cfg.CostLimit != nil {
		options = append(options, cel.CostLimit(*cfg.CostLimit))
	}
	return &Compiler{
		env:                privateEnv,
		opts:               options,
		predicate:          cfg.Predicate,
		maxExpressionBytes: cfg.MaxExpressionBytes,
		cache:              cache,
		flight:             singleflight.Group{},
	}, nil
}

// Compile returns a cached program or compiles the expression once for concurrent
// misses. Compilation is synchronous: context cancellation and runtime cost
// limits do not bound parsing, checking, program construction, or flight waits.
func (c *Compiler) Compile(expr string) (cel.Program, error) {
	if c.maxExpressionBytes > 0 && len(expr) > c.maxExpressionBytes {
		return nil, ErrExpressionSize
	}
	if prg, ok := c.cache.Get(expr); ok {
		return prg, nil
	}
	v, err, _ := c.flight.Do(expr, func() (any, error) {
		if prg, ok := c.cache.Get(expr); ok {
			return prg, nil
		}
		ast, issues := c.env.Compile(expr)
		if issues != nil && issues.Err() != nil {
			return nil, &Error{kind: ErrCompile, cause: issues.Err()}
		}
		if c.predicate && !ast.OutputType().IsExactType(cel.BoolType) {
			return nil, &Error{kind: ErrCompile, cause: ErrResultType}
		}
		prg, err := c.env.Program(ast, c.opts...)
		if err != nil {
			return nil, &Error{kind: ErrCompile, cause: err}
		}
		c.cache.Add(expr, prg)
		return prg, nil
	})
	if err != nil {
		return nil, err
	}
	prg, ok := v.(cel.Program)
	if !ok {
		return nil, &Error{kind: ErrCompile, cause: errors.New("unexpected compilation result")}
	}
	return prg, nil
}

// Eval evaluates with independent CEL interpreter state and redacts failures.
// Callers own the activation and must not mutate it during evaluation. Custom
// functions must keep mutable per-call state in that activation, not closures.
// CEL context checks are cooperative; they cannot interrupt a custom function.
func Eval(ctx context.Context, prg cel.Program, activation any) (ref.Val, *cel.EvalDetails, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, &Error{kind: ErrCanceled, cause: err}
	}
	out, details, err := prg.ContextEval(ctx, activation)
	var canceled interpreter.EvalCancelledError
	if errors.As(err, &canceled) && canceled.Cause == interpreter.CostLimitExceeded {
		return nil, details, &Error{kind: ErrCostLimit, cause: err}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, details, &Error{kind: ErrCanceled, cause: ctxErr}
	}
	if err != nil {
		return nil, details, &Error{kind: ErrEvaluation, cause: err}
	}
	return out, details, nil
}

// EvalPredicate evaluates and requires a boolean result, including for programs
// compiled without static predicate validation.
func EvalPredicate(ctx context.Context, prg cel.Program, activation any) (bool, error) {
	out, _, err := Eval(ctx, prg, activation)
	if err != nil {
		return false, err
	}
	b, ok := out.(types.Bool)
	if !ok {
		return false, &Error{kind: ErrEvaluation, cause: ErrResultType}
	}
	return bool(b), nil
}
