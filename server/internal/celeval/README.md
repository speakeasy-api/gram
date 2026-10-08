# Cached CEL evaluation

`celeval` owns compilation, a bounded LRU, singleflight, and redacted evaluation
errors. Domains own declarations, bindings, input conversion, enrichment/I/O,
and result interpretation. Each compiler has one immutable environment and
program configuration; expression text is its only cache key. There is no global
cache. Only successful programs are cached, never results or activation data.

```go
env, err := cel.NewEnv(cel.Variable("name", cel.StringType))
if err != nil { return err }
compiler, err := celeval.New(env, celeval.Config{
    Capacity: 128,
    Predicate: true,
    MaxExpressionBytes: 4096,
    CostLimit: new(uint64(10000)),
})
if err != nil { return err }
program, err := compiler.Compile(`name.startsWith("example")`)
if err != nil { return err }
matched, err := celeval.EvalPredicate(ctx, program, map[string]any{"name": name})
```

Use `Predicate: false` and `Eval` for other result types and CEL evaluation
details. `EvalPredicate` also checks the runtime result type. Programs can be
evaluated concurrently with independent activations. CEL's interpreter state is
per-call, but custom providers, bindings, program-option closures, and observers
must be immutable or concurrency-safe. Keep request state in activations. The
constructor copies the environment and option slice; it cannot deep-copy closure
captures or custom providers. Do not use mutable global evaluation bindings.

## Limits and errors

- Capacity bounds retained program **count**, not bytes, in-flight compilations,
  or programs still referenced by callers. Evicted programs remain usable.
- `MaxExpressionBytes` bounds source bytes before parsing. Zero adds no limit.
  CEL's parser limits still apply. Compilation and singleflight waits are
  synchronous, not bounded by evaluation context or cost.
- `CostLimit` enables CEL's runtime operation-cost accounting. Nil adds no limit;
  a pointer to zero is a zero budget. It is not a wall-clock, memory, compilation,
  or input-size budget. Custom functions may do arbitrary CPU work, allocation,
  regex matching or I/O inside one interpreter operation. A custom CEL cost
  estimator can account for that work, but does not preempt it or undo its effects.
- Evaluation checks context before/after running and cooperatively during CEL
  comprehensions. It cannot interrupt a custom function; domain I/O must use its
  own context/timeouts. Avoid I/O in expression functions where possible.
- Match `ErrCompile`, `ErrEvaluation`, `ErrCostLimit`, `ErrCanceled`,
  `ErrExpressionSize`, and `ErrResultType` with `errors.Is`. Cancellation also
  matches `context.Canceled` or `context.DeadlineExceeded`. A predicate type error
  matches `ErrCompile` at compile time or `ErrEvaluation` at runtime.
- Default error messages contain no source or activation values. There is no
  internal logging. Explicitly unwrapping `*Error` exposes CEL diagnostics, which
  may contain sensitive data. Calling raw `cel.Program` evaluation methods also
  bypasses the shared error policy.
- Failed compilations are never cached. Concurrent overlapping misses share the
  failure; subsequent requests retry. This preserves custom-rule behavior.

## Consumers and migration

Custom rules use a risk-owned predicate compiler with the existing 8192-entry
capacity and no new source or cost limits. Risk still owns matchers, JSON paths,
message/tool scoping, and per-call span collection. Detection evaluation uses
the shared boolean helper with a background context to preserve the existing
non-cancelable domain API. Save-time validation retains its detailed diagnostics.
Runtime failures still propagate to the scanner rather than becoming nonmatches.
Invalid regex/glob matchers still return false. No expression syntax or persisted
contract changes.

Spend rules can use the same compiler: their typed scalar/list environment,
strict boolean output, and successful-compilation-only caching fit directly.
Actor normalization and fail-open logging belong in spendrules. The gate is left
unchanged in this refactor: its approximate 1024-entry wholesale flush would
become an exact LRU, and duplicate cold compilations would become singleflight.
Neither changes predicate results, but both change cache behavior. A future
migration should retain retry-on-compile-failure, empty-list normalization,
fail-open handling, and current limits/cancellation policy. Save-time diagnostics
should remain domain-owned rather than silently replacing them with redacted
runtime errors.

This internal refactor changes no API, dashboard data, permissions, or Platform
MCP capability, so no MCP tool or demo-seed change is needed.
