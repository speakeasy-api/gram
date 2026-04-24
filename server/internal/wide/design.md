# wide: collection design

## Goal

Provide a concurrency-safe attribute collector where the dominant workload is
serial pushes followed by a single emit — the pattern seen in HTTP request
handling.

## Approach: lock-free prepend list

Each `Push` allocates a small node and atomically prepends it to a singly
linked list via `CompareAndSwap` on the head pointer. `Emit` walks the list,
collects all attributes into a slice, and reverses it to restore insertion
order.

```
head ──▶ [node C] ──▶ [node B] ──▶ [node A] ──▶ nil
          (newest)                   (oldest)
```

## Why not a mutex-guarded slice?

A mutex adds overhead to every `Push` and `Emit` call even when there is no
contention, which is the common case (single-goroutine request handlers).
Benchmarks show the mutex variant is ~35% slower than the lock-free list in the
serial push+emit path.

## Why not a growing context chain?

An alternative is to have `Push` return a new `context.Context` via
`context.WithValue` on every call. This avoids shared mutable state entirely
but builds a long context chain — one extra layer per push — which increases
the cost of every subsequent `context.Value` lookup for all context keys, not
just ours.

## Trade-offs

| Property                   | Lock-free list                         | Mutex slice              | Context chain                                       |
| -------------------------- | -------------------------------------- | ------------------------ | --------------------------------------------------- |
| Serial push+emit speed     | Fastest                                | ~35% slower              | Comparable, but degrades `context.Value` lookups    |
| Concurrent push            | Safe (CAS retry)                       | Safe (lock)              | Safe (immutable)                                    |
| Concurrent push throughput | Lower than mutex under high contention | Higher under contention  | N/A (no shared state)                               |
| Memory per push            | 80 B node alloc                        | Amortised (slice growth) | `context.WithValue` wrapper + state copy            |
| Emit cost                  | List walk + reverse                    | Slice copy under lock    | Chain walk + collect                                |
| API                        | `Push(ctx, attrs...)`                  | Same                     | `ctx = Push(ctx, attrs...)` — caller must propagate |

## Benchmark summary (10 pushes + 1 emit, arm64)

| Variant            | ns/op      | B/op      | allocs/op |
| ------------------ | ---------- | --------- | --------- |
| Baseline (unsafe)  | ~1,580     | 3,544     | 17        |
| Mutex              | ~2,115     | 4,448     | 18        |
| **Lock-free list** | **~1,010** | **2,072** | **23**    |

The lock-free list trades a higher allocation count (one node per push) for
lower total bytes and faster throughput. The benchmark suite in
`context_bench_test.go` covers serial and concurrent scenarios across all three
variants.
