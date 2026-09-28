# Classifier contract

The classifier evaluates independent questions against one shared JSON entry.
Callers own domain definitions, content preparation, and interpretation of answers.
Implementations own model-specific compilation, token accounting, request packing,
bounded concurrency, transport, and response validation.

## Entry point

`Classify(ctx, *Request)` validates, compiles, partitions, and evaluates a logical
batch. Callers do not manage plans or make a separate assessment call.
The interface does not prescribe a tokenizer or hardcode a provider's limits.

Construct a request with shared state and append questions using chainable `Ask`:

```go
req := classifier.NewRequest(classifier.Text("… state …")).
    Ask(classifier.Noul("urgent", classifier.Text("Is action urgent?"),
        classifier.WithPositive(classifier.Text("Immediate action is needed.")),
        classifier.WithNegative(classifier.Text("Action can wait.")),
    )).
    Ask(classifier.Choice("department", classifier.Text("Who should handle this?"),
        classifier.NewOption("billing", classifier.Text("Payments and invoices")),
        classifier.NewOption("support", classifier.Text("Technical assistance")),
    )).
    Ask(classifier.Score("frustration", classifier.Text("How frustrated is the author?"),
        classifier.NewOption("low", classifier.Text("Calm")),
        classifier.NewOption("high", classifier.Text("Very angry")),
    ))

result := c.Classify(ctx, req)
for outcome := range result.Iter() {
    // Process successful answers and question-level failures.
    _ = outcome
}
if err := result.Err(); err != nil {
    // Handle the operation error after processing known outcomes.
}
```

Construction is permissive; execution validates the complete batch.
`Ask` mutates and returns the same request. Do not mutate requests during execution.
Choice and Score constructors copy their option slices, preserving order.
State accepts the same structured `Entry` values as instructions and criteria.

## Question boundaries

`Question` has one of three variants:

- **Noul:** an independent probability of a positive outcome.
- **Choice:** one choice from mutually exclusive options, with a distribution.
- **Score:** an expected zero-based index across ordered levels, with a distribution.

Questions are independent and can be partitioned together regardless of which
domain object produced them. One Choice or Score question is indivisible:
splitting its options changes its meaning. Question and option keys are opaque
correlation identifiers, not prompt content. Labels that should influence inference
belong in the instructions or option descriptions.

Instructions, Noul outcome criteria, and option/level descriptions use `Entry`:
a JSON string, object, array, or null. Nested values can be any valid JSON value.
Use `Text("...")` for prose, `NewEntry(value)` to encode Go values, or
`ParseEntry(data)` for existing JSON. Entries own their bytes, preserve encoded
numbers without float64 conversion, and support JSON marshaling/unmarshaling.
The zero value encodes explicit `null`; it does not represent field omission.

## Budget semantics

Implementations pack whole questions alongside the complete shared input and
must never silently truncate content to fit. An input/question pair that exceeds
the provider's limit cannot be fixed by splitting that question's options.

### Jev

`jev.New(guardianPolicy, key)` accepts an OpenRouter API key as `conv.Secret` against
`https://openrouter.ai/api/v1/systemone`, using `jev-latest`. OpenRouter maps this
bare model ID to its `typesafe/` namespace. The adapter sends structured
entries directly and maps opaque question/option keys to numeric wire identifiers.
Jev requires non-null state, 1–255 Choice options, and 2–10 Score levels.

Use `jev.WithEndpoint(server.URL)` with `httptest.NewServer` to exercise the
adapter against a test HTTP server. The option accepts the complete endpoint URL.

The complete batch is sent first, with no local token estimation or upfront
partitioning. HTTP 413 responses and HTTP 400 responses with
`{"detail":{"error_type":"max_tokens_exceeded"}}` split multi-question requests recursively into
halves, preserving the complete state and whole questions. Split requests run
sequentially. Other validation responses are provider rejections, not assumed to
be size errors. No input is truncated.

Each split produces two non-empty, strictly smaller question sets. Singleton
rejections terminate, bounding an N-question batch to at most 2N−1 HTTP attempts.

If a single-question request receives either size rejection, its outcome is input-too-large
and `result.Err()` matches `errors.Is(result.Err(), classifier.ErrRequestTooLarge)`.
Other questions still run, and successful answers and known usage remain in the
returned result. This error does not distinguish oversized input from an oversized
question; it means the pair cannot fit. If cancellation also occurs, both errors
remain identifiable through `errors.Is`.

Each instance bounds concurrency to four physical requests across all calls,
with a 60-second per-request timeout. Rate limits and transient failures are
reported with retry hints; callers own retries. Usage includes all known attempts
and is marked incomplete when rejected or interrupted attempts omit usage.

Guardian admission is configured at 1,200 requests per minute with a burst of 20,
waiting for capacity within the request timeout. Every physical request, including
split requests, consumes capacity. This is a replenishing rate with burst allowance,
not a strict rolling-minute ceiling. The `jev` namespace partitions by endpoint
host rather than API key, conservatively sharing capacity across credentials.
Guardian context subsets can further partition that capacity. Enforcement requires
a policy with a real limiter; the server uses Redis-backed shared state, while
Guardian's default no-op limiter does not enforce admission. Token throughput is
not limited by this configuration.

References: [OpenRouter System One API](https://openrouter.ai/docs/api/api-reference/systemone/submit-a-system-one-request),
[TypeSafe API](https://docs.typesafe.ai/api), [TypeSafe limits](https://docs.typesafe.ai/models).

## Failure semantics

Malformed batches, including duplicate keys and ambiguous question variants, fail
before any provider requests. Provider size rejections become question failures.

On normal completion, every question has exactly one answer or failure, in the
original order. A failed partition does not discard successful answers from other
partitions. Callers can retry failed questions and decide which combinations of
answers constitute a complete domain result.

Cancellation or an operation-wide failure may yield a partial result with a
non-nil `result.Err()`. Missing outcomes do not prove the provider did no work. Usage includes only
known counts and explicitly reports whether accounting is complete.

`Classify` completes execution before returning. `result.Iter()` is a repeatable
`iter.Seq[QuestionOutcome]` over collected outcomes, not a network stream. Breaking
iteration does not cancel work or change the error. `Err()` is available before,
during, and after iteration; always check it, even after an early break.

## Scope

This package defines the interface, data structures, disabled implementation, and
Jev adapter. Exported structs intentionally remain plain Go values. There is no
sensor/signal dependency, persistence, asset fetching, threshold policy, or delivery
orchestration here.

This is an internal contract, so no Platform MCP or staff Admin MCP surface changes
are needed. The existing decision to defer sigint Platform MCP tools until the
feature is production-ready still applies.

## Disabled operation and tests

Use `classifier.Noop{}` when classification is disabled. It performs no
work and returns an empty result whose `Err()` is `classifier.ErrDisabled`. This preserves
the distinction between disabled evaluation and successful question outcomes.

Tests of consumers can use `classifiertest.NewMock(t)`. It implements the interface
with testify's `mock.Mock` and verifies expectations during test cleanup:

```go
mockClassifier := classifiertest.NewMock(t)
mockClassifier.On("Classify", ctx, request).Return(expectedResult).Once()
```

Use `classifier.NewResult(outcomes, err)` to construct provider results or configure
partial-result handling in tests. Populate metadata and usage fields as needed.
Testify helpers such as `mock.MatchedBy` and `Run` are
available for argument matching and call-time behavior.
