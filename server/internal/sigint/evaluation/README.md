# Sensor evaluation

`gram streams` consumes `gram.conversation.v1.MessageEvent` through
`gram.sigint.v1.Evaluator`. It publishes successful `gram.sigint.v1.Reading`
messages and a `gram.sigint.v1.Outcome` for every other terminal sensor decision. Configure **`GRAM_SIGINT_OPENROUTER_API_KEY`** with a platform-owned
OpenRouter inference key. The receiver is not registered when this is unset,
unless temporary ack-only mode is enabled.
The shared Jev client uses this key for every tenant; evaluation does not resolve
customer credentials or provision organization OpenRouter keys.

For an initial rollout, pass `--sigint-ack-only` to `gram streams` or set
`GRAM_SIGINT_ACK_ONLY=true`. This starts the subscription without requiring an
inference key and acknowledges every decoded batch before entitlement checks,
sensor loading, content resolution, classification, or reading publication.
It takes precedence even when an inference key is configured. This drains the
subscription; messages acknowledged in this mode are not evaluated later when
the flag is disabled. To begin evaluation, disable the flag and configure the
platform inference key. The flag defaults to false and affects only sigint.

The organization must have `signals_intelligence` enabled. A single SQL statement
loads active project-owned sensor definitions and signal membership in order.
Only `TYPE_CREATED` events for user and assistant messages are evaluated, including
historical imports. Attribution updates and attachment-added events are acknowledged
without evaluation.
Four events can be evaluated concurrently; the shared Jev client independently
caps in-flight provider requests at four and uses Guardian rate admission.

## Source adapters and shared evaluation

`Evaluator.Evaluate` accepts an `Input`: I/O-free metadata and `NewBuffer(maxSize)`
creating a per-evaluation buffer. Its lazy `Resolve(ctx, size)` returns classifier
text or structured JSON. Sources without history provide a single-entry buffer
that rejects sizes above one with `ErrInvalidInput`. Metadata contains
tenant scope, a source-event reference, and independently optional actor, billing,
account, assistant, and replay attribution. Event IDs are opaque strings, not
necessarily UUIDs. The shared evaluator has no conversation-role or conversation-ID
requirement. It enforces tenant validity, entitlement, source-aware sensor
selection, readiness, content limits, classification, and publication semantics.

`ConversationHandler` adapts the conversation subscription. It validates message
and conversation UUIDs, groups eligible references by organization/project, and
batch-loads current messages and attachment locators from PostgreSQL. Duplicate
message references share a lookup. Missing messages and deleted conversations or
projects are acknowledged; database failures nack the affected tenant's deliveries.
The adapter owns the `{role, content, parts}` projection. Asset reads occur only
after entitlement and runnable sensors have been established.

Both message queries select explicit columns. History returns the message ID,
role, content representations, tool calls, row-local-content flag, attachment
locators and an overflow marker. PostgreSQL measures the cumulative inline payload
of the selected predecessors against the remaining content budget before returning
payloads. Over-budget rows retain their IDs and positions but omit content and
locators, marking where the buffer must stop expanding into older history.
The target lookup additionally selects identity, ordering and attribution fields
needed to anchor the window and publish readings.

`Source.Load` receives the event kind alongside organization and project. The
repository currently selects configuration only for `conversation.message`;
other kinds have no eligible sensors. Per-sensor source applicability is deferred.
This prevents conversation-specific configuration from silently applying to a
new stream. A future MCP receiver must introduce explicit applicability and an
adapter for execution arguments/results before registering its subscription.
The shared path is tested with standalone `mcp.tool_call` and custom events.

Readings contain `event.kind`, `event.id`, and `event.occurred_at`, with optional
typed conversation-message or tool-call context. New kinds may omit that context;
they do not need synthetic messages or conversation IDs. An adapter must assign
an event ID unique within organization/project/kind; a connection-local protocol
call ID alone is not sufficient. `Reading.source` continues to namespace external
actor identities and is distinct from the event kind.

Adapters wrap `ErrInvalidInput` for permanent content failures. Oversized history
truncates context; other conversation asset-validation errors remain terminal,
and transient resolution errors retry.
Resolved JSON must be non-null and at most 16 MiB, including the complete window.

### Future improvement: conditional content projection

Defer selecting only the content representation the resolver uses:

- Split transcript rows use row-local `content` and ignore `content_raw` and its asset.
- Other rows prefer inline `content_raw`, then `content_asset_url`, then fallback `content`.

Conditional SQL projections could avoid transferring unused or duplicated content
while preserving tool calls and attachments. Preserve the current size accounting:
fallback `content` bytes count toward the input budget even when original content
is used. Returning `octet_length(content)` instead of unused text may preserve that
accounting. Compare serialized output size and execution time on representative
messages, and retain the request goldens and content-limit coverage when pursuing
this optimization. The current queries intentionally fetch all content representations.

## Message context

Sensors select `input_strategy`: `single_message` (default) or `sliding_window`.
`window_size` accepts 2–10 total messages including the target, default 5.
Omitted update fields preserve settings; switching strategies retains window size.

System messages are excluded from both target and history lookups for every
strategy. They do not consume window capacity. Sliding windows classify the
target using up to N−1 preceding non-system messages in its project, conversation
and generation. History follows
`(created_at, seq)` order: sequence only breaks timestamp ties. The target snapshot
anchors the query, so later messages cannot enter the window. Short histories
evaluate immediately. Late imports and timestamp corrections affect subsequent
evaluations; retries resolve current history under the same target reading identity.

State is `{messages: [{role, content, parts}, ...], target_message_index: N-1}`,
where N is the actual message count. The `content` field is omitted when original
content is absent or JSON null; row-local text, tool calls and attachments use `parts`.
Messages run oldest to newest with the target last. For sliding-window inputs,
every question explicitly instructs the classifier to classify only that
target and use the preceding messages as evidence. Initial eligibility checks the
incoming event's creation type and user/assistant role; the persisted target must
also have a user/assistant role. Sensor matching evaluates the persisted target
role. Non-system context roles do not need to match the target's sensor predicate.

The evaluator groups sensors by effective input strategy and size. Compatible
sensors share one request. A per-evaluation buffer is sized to the largest window
among matched, runnable sensors; each group resolves its slice while preparing
classifier state. The target is resolved before any groups run, so target errors
are handled once. History is fetched lazily once, with a limit of buffer capacity
minus one. Resolved content is reused across ascending window sizes within the
16 MiB payload budget. This is not an exact heap ceiling: persisted target rows,
decoded objects, metadata and encoding copies also consume memory. When history
exceeds the budget, the buffer uses the largest contiguous suffix that fits,
including the target, and updates `target_message_index` to the effective count.
It stops at the first oversized predecessor rather than skipping to older turns.
A requested five-message window can therefore evaluate with two messages, or with
only the target. The sliding-window question framing remains in effect. Window
JSON framing counts toward the budget; an oversized target still fails evaluation.
Other failures in older content affect only larger slices, and a history
lookup failure does not prevent target-only readings. Redelivery creates a fresh
buffer. The classifier contract remains questions plus state. Definition hashes
include strategy, size and the versioned target-framing contract. Each reading's
`input` records what that attempt actually supplied: strategy, requested and
supplied message counts including the target, and whether the content budget
truncated available history.

### Request goldens

`TestSingleMessageModelRequestGolden` and `TestSlidingWindowModelRequestGolden`
each drive the conversation handler through the real Jev client to a local HTTP
model recorder. Their complete outbound bodies are pinned in
`testdata/requests/single_message.golden.yaml` and
`testdata/requests/sliding_window.golden.yaml`. Each covers all three classification
modes, original structured content, tool calls and attachments. The sliding-window
fixture also pins role order, the target marker and exclusion of archival siblings
and messages outside the window.

Other HTTP-request goldens cover matching, grouped redelivery and independent
events under `testdata/requests/`. Input-state goldens for filesystem content,
empty history and omitted null payloads live under `testdata/state/`; they pin
resolved state before Jev request compilation. These use `testenv.RequireYAMLGolden` with explicit fixture
paths. Normal runs only compare against the committed fixtures and
fail when a fixture is missing or differs. Regenerate explicitly from captured
output with:

```sh
mise run test:server ./internal/sigint/evaluation/ -args -update
```

Review and commit the generated diff, then run the tests without `-update`.
The generator emits only captured data; explanatory comments belong in this README.
Generated goldens retain the YAML encoder's formatting and are excluded from oxfmt.
The grouped-redelivery fixture contains three initial requests (target-only, a
shared two-message window, and a five-message window using short history), followed
by the same groups after the history changes.
The independent-event fixture captures all three evaluations, including redelivery
and a changed event kind. The null-content fixture is shared by inline and spilled
JSON null payloads; both intentionally omit `content`.
The budget-fallback fixture pins target-focused requests for windows of 2, 5 and 10
when only two messages fit. It also covers naturally short history with the same
requests but no truncation metric.

Fixtures use readable YAML, including multiline instructions, while captured
requests remain JSON. Comparison parses YAML strictly and ignores object-key order
and formatting while preserving exact numbers and string contents.
Numeric scalars use JSON number syntax and are read from the YAML syntax tree as
`json.Number`, preserving large integers, fractional precision and exponent text.

## Compilation and readiness

Sensor `match_expression` predicates use the shared cached CEL compiler and the
typed `message.role` field (lowercase persisted roles). Matching happens before
asset resolution and classification. Nonmatches are counted as skipped; invalid
predicates and cost exhaustion are terminal per-sensor failures, while canceled
contexts retry. The compiler retains at most 1024 programs, limits expressions to
4 KiB, and budgets 10,000 CEL operations per evaluation. Expressions and the
matching-contract version participate in definition hashes, not reading identity.

The matching contract currently exposes role only. Every input explicitly supplies
message metadata or declares it unavailable. An unavailable message remains an
unbound CEL variable, so a negative role comparison cannot treat it as an empty
role; predicates independent of message metadata can still succeed. Failure
metrics distinguish compilation, result type, expression size, evaluation, and
cost-limit errors. Actor, source, replay, and tool metadata are not exposed yet.

Based on the [System One request schema](https://openrouter.ai/docs/api/api-reference/systemone/submit-a-system-one-request):

- Instructions must be present (an empty string is a valid API value).
- `multi_label`: one Noul question per signal. Missing criteria are optional;
  absent Noul criteria are omitted, while one-sided criteria serialize the other
  side as an empty string rather than null.
- `exclusive`: one Choice question, 1–255 options; null descriptions are allowed.
- `ordered_score`: one Score question, 2–10 ordered, non-null level descriptions.
- Empty sensors and incomplete definitions are skipped, counted as `draft` in
  `gram.sigint.evaluation.skipped`. Incomplete sensors whose match expression
  selects the event publish `STATUS_DRAFT` outcomes. Matching runs first, so a
  nonmatching incomplete sensor publishes `STATUS_NOT_APPLICABLE`.

Every eligible runnable sensor contributes to one logical classifier request per
event and effective input configuration.
A multi-label reading requires all its Noul outcomes to succeed.

## Identity and content

Reading identity is a UUIDv5 derived from a JSON tuple containing tenant,
evaluation scope, source event and sensor:

```text
["sigint-reading", organization_id, project_id, "stream", event_kind, event_id, sensor_id]
["sigint-reading", organization_id, project_id, "manual", run_id, event_kind, event_id, sensor_id]
```

Every input supplies a validated `readings.Context`: an explicit `stream` or
`manual` trigger, a nonzero run UUID only for manual evaluations, and an immutable
request timestamp. The conversation adapter uses the creation mutation's
`MessageEvent.occurred_at` as request time, independently of the original
`message_created_at` source time. Manual inputs use their accepted run's creation
time. Both readings and outcomes carry that context, and ClickHouse uses request
time for partitioning and two-year retention. Invalid or absent context is terminal
before sensor loading or inference.

Retries share identity even if sensor configuration changes between attempts.
Separate manual runs and streaming results retain independent identities.
The fixed namespace is UUIDv5(URL namespace, `gram:sigint:reading`). The UUID is
serialized as a canonical string on the wire and can be stored in native UUID
columns in PostgreSQL and ClickHouse.
Each evaluation has a separate attempt UUID and completion timestamp.
For conversations, the logical event ID is the persisted message UUID, not the
`MessageEvent.id` mutation UUID or Pub/Sub delivery ID. The reading refers to the
message's creation time. Redelivery loads current state again, so the evaluated
content and attribution can differ between attempts under the same reading ID.
Sensor and signal slugs are always populated in readings from the same configuration
snapshot. Choice results also carry the selected signal's slug. Slugs are metadata
and do not affect reading identity.
Definition hashes are metadata for traceability, not identity, and include mode, instructions,
signal IDs, criteria and order. Event content
is not hashed for reading identity or metadata. Original JSON comes from the
message row or its existing content asset and preserves number representations.
Split Anthropic transcript rows use their row-local text and tool calls rather
than repeating the full archival content for each sibling. Associated textual
attachments are loaded from their existing asset locators.

Asset reads use the configured storage backend, enforce project paths, and cap
resolved input at 16 MiB. Malformed content, non-UTF-8 attachments and missing
assets are permanent failures. Other storage errors retry. Message events carry
no content bodies or publication-specific spilled assets.

## Delivery and observability

### Transcript lineage

Only original message creations are eligible targets. Exact copies from an
earlier transcript generation, synthetic compaction/recovery messages and
unattributable rewritten history are excluded before classification. Historical
imports and offline-spool replay are independent: their original messages remain
eligible at their source event time.

Creation events persist generation, sequence, capture kind and an original-message
reference where exact copying is established. Proxy rewrites match positional
prefixes and a unique contiguous retained tail anchored at the previous
generation's end. A repeated phrase alone does not establish a copy. Unmatched
rewrite history is unknown; appended messages after a matched tail are original.
Producer-declared compactions mark unmatched text synthetic. This conservative
rule can exclude edited/rewritten messages whose original identity is unavailable;
it does not represent complete population coverage.

Readings preserve ingestion identity separately from billing attribution:
`actor` contains the persisted message's Speakeasy user ID and external user ID plus
the ingestion event's observed email;
`billing_user_id` is the producer's explicit usage allocation, including for
assistant-generated messages with no actor. Neither identity is inferred from the
other. `source` namespaces external identities within the organization. Account
ID comes from the current conversation row; account type/billing mode, assistant
ID and the historical `replayed` marker come from the event's ingestion context.
Absent provenance remains absent. Creation events snapshot `model_name`,
`model_family` (recognized model vendor), `inference_provider` (reported inference
provider) and `department` in their string attributes. Department uses the
actor's active directory profile within the message's organization: the latest
linked profile wins, with an unambiguous observed-email fallback. Billing
allocation never supplies an actor or department. Blank/unknown values are
omitted. Imports record the directory state known at ingestion, not an inferred
historical department. Evaluation carries these attributes through retries and
outcomes without re-enrichment, except when the oversized-outcome safeguard
drops attributes to meet the transport budget. They describe the source message, independently
of the classifier's model/provider metadata.
Completion responses supply the reported serving provider; first-party imports
name their known service. Hook account-provider metadata alone does not establish
an inference provider, so those messages omit `inference_provider` when it is unknown.
These fields do not participate in reading identity or the definition hash.

Successful readings are published and their publication results awaited before
acknowledgment.

### Outcomes

Coverage needs every eligible sensor decision, not only answers. Each loaded
sensor that does not produce a reading publishes one `gram.sigint.v1.Outcome`,
awaited before acknowledgment like readings:

| Decision                                       | Status                  | `failure_reason`                                                                                      |
| ---------------------------------------------- | ----------------------- | ----------------------------------------------------------------------------------------------------- |
| Match expression excludes the event            | `STATUS_NOT_APPLICABLE` | empty                                                                                                 |
| Incomplete definition (checked after matching) | `STATUS_DRAFT`          | empty                                                                                                 |
| Invalid predicate or exhausted matching budget | `STATUS_FAILED`         | `match_compile`, `match_result_type`, `match_expression_size`, `match_evaluation`, `match_cost_limit` |
| Unusable input shared by a group of sensors    | `STATUS_FAILED`         | `invalid_content`, `content_too_large`, `invalid_reference`, `missing_asset`                          |
| Non-retryable classifier failure               | `STATUS_FAILED`         | `classifier_rejected`                                                                                 |
| Answer does not fit the sensor mode            | `STATUS_FAILED`         | `invalid_answer`                                                                                      |
| Reading over the 9 MiB transport budget        | `STATUS_FAILED`         | `reading_too_large`                                                                                   |

An outcome carries the reading ID the same sensor, event and evaluation scope
would produce, the trigger/run and immutable request time, attempt ID, tenant,
event reference, sensor ID/slug, definition hash (empty for a
draft), decision time and the same attribution as a reading. Failures after input
resolution also record the supplied input, and failures after inference record
the classifier model metadata. One attempt ID covers every reading and outcome of
an event evaluation. Retryable failures publish nothing and nack the delivery, so
redelivery decides them later. Event-level failures before sensors load (invalid
identity, timestamp, event, or evaluation context) have no sensor to attribute and
publish no outcome; a message census must account for those, including
`invalid_evaluation_context`. The failure and skip counters are unchanged.
Readings exceeding 9 MiB of serialized protobuf are rejected before publication,
leaving headroom under Pub/Sub's 10 MiB limit. They are permanent failures with
reason `reading_too_large`; other successful readings are still published.
Permanent failures emit error logs and
`gram.sigint.evaluation.failures` with a bounded `reason` attribute. Transient
classification/storage failures and publication failures nack the source delivery.
Budget-truncated windows increment `gram.sigint.evaluation.window_truncations`
once per affected sensor evaluation attempt, with `reason=content_too_large`,
`requested_window_size` and `effective_window_size`. Sensors sharing a request
each count; redelivery counts another attempt. Naturally short histories do not
increment this counter, and truncation alone does not increment failures.
Each affected sensor also emits a warning-level wide event, `sensor evaluation
window truncated`, with organization/project, event kind/ID, chat and target-message
IDs, sensor ID/mode/definition hash, requested/effective window sizes, content byte
budget and truncation reason. The log describes the context adjustment before
classification; it does not imply that classification or publication succeeded.
All evaluator logs carry `gram.component=sigint-evaluator`.
Redelivery may reevaluate all sensors and republish successes with the same
logical reading ID and different answers/attempt IDs. Independent ClickHouse
consumers store both topics in `sigint_readings_v2`, where the latest completion
wins within its trigger/run scope. Request time remains fixed across redelivery.

Conversation and reading topics and the evaluator subscription retain messages
for four days; acknowledged subscription messages are not retained. Provision
the generated infrastructure and configure the key before enabling consumption.

Platform MCP assessment: project editors configure message context using the
existing `create_sensor` and `update_sensor` tools. Their proposals, previews and
confirmation tokens cover strategy and size; `find_sensors` and `get_sensor`
return those settings for live verification. The shipped authoring workflow
documents the same choices as the dashboard. Demo sensors exercise target-only,
five-message and ten-message inputs.

Temporal actions/month: 0 (Pub/Sub batch handler, no Temporal).
