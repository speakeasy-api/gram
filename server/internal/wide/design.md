# wide: collection design

## Goal

Provide a concurrency-safe attribute collector where the dominant workload is
serial pushes followed by a single emit — the pattern seen in HTTP request
handling.

## Approach: lock-free prepend list

Each `Push` stores its attributes in a small node and atomically prepends the
node to a singly linked list via `CompareAndSwap` on the head pointer. `Emit`
walks the list, collects all attributes into a slice, and reverses it to
restore insertion order.

To avoid an allocation and copy on every `Push`, the node retains the provided
attribute slice. Callers must not modify the slice or its elements after
passing it to `Start` or `Push`.

```
head ──▶ [node C] ──▶ [node B] ──▶ [node A] ──▶ nil
          (newest)                   (oldest)
```

## Why not a mutex-guarded slice?

A mutex adds overhead to every `Push` and `Emit` call even when there is no
contention, which is the common case (single-goroutine request handlers).
Controlled benchmarks show the lock-free list has ~37% lower latency than the
mutex variant in the serial push+emit path.

## Why not a growing context chain?

An alternative is to have `Push` return a new `context.Context` via
`context.WithValue` on every call. This avoids shared mutable state entirely
but builds a long context chain — one extra layer per push — which increases
the cost of every subsequent `context.Value` lookup for all context keys, not
just ours.

## Trade-offs

| Property                   | Lock-free list                         | Mutex slice              | Context chain                                       |
| -------------------------- | -------------------------------------- | ------------------------ | --------------------------------------------------- |
| Serial push+emit speed     | ~37% lower latency than mutex          | Slower                   | Comparable, but degrades `context.Value` lookups    |
| Concurrent push            | Safe (CAS retry)                       | Safe (lock)              | Safe (immutable)                                    |
| Concurrent push throughput | Lower than mutex under high contention | Higher under contention  | N/A (no shared state)                               |
| Memory per push            | One node; retains caller attrs         | Amortised (slice growth) | `context.WithValue` wrapper + state copy            |
| Emit cost                  | List walk + reverse                    | Slice copy under lock    | Chain walk + collect                                |
| API                        | `Push(ctx, attrs...)`                  | Same                     | `ctx = Push(ctx, attrs...)` — caller must propagate |

## Benchmark summary (10 pushes + 1 emit, arm64)

| Variant            | ns/op      | B/op      | allocs/op |
| ------------------ | ---------- | --------- | --------- |
| Baseline (unsafe)  | ~3,850     | 3,544     | 17        |
| Mutex              | ~4,530     | 4,448     | 18        |
| **Lock-free list** | **~2,850** | **2,264** | **25**    |

The lock-free list trades one node allocation per push for lower total bytes
and faster throughput. The benchmark suite in `context_batch_test.go` covers
all three variants in serial scenarios; concurrent scenarios cover only the
concurrency-safe Mutex and LockFree variants.
