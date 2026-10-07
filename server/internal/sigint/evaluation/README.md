# Sensor evaluation

`gram streams` consumes `gram.conversation.v1.MessageEvent` through
`gram.sigint.v1.Evaluator` and publishes successful `gram.sigint.v1.Reading`
messages. Configure **`GRAM_SIGINT_OPENROUTER_API_KEY`** with a platform-owned
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

`Evaluator.Evaluate` accepts an `Input`: I/O-free `Event()` metadata and a lazy
`Resolve(ctx)` returning classifier text or structured JSON. Metadata contains
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

Adapters wrap `ErrInvalidInput` for permanent content failures. Conversation asset
validation also returns terminal failures; other resolution errors retry.
Resolved JSON must be non-null and at most 16 MiB. Evaluation is
per event; cross-event windows and correlation are separate input-building work.

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
  `gram.sigint.evaluation.skipped`.

Every eligible runnable sensor contributes to one logical classifier request per event.
A multi-label reading requires all its Noul outcomes to succeed.

## Identity and content

Reading identity is a UUIDv5 derived from a versioned JSON tuple containing organization,
project, event kind, immutable logical event ID and sensor ID. The tuple is
`["sigint-reading-v1", organization_id, project_id, event_kind, event_id, sensor_id]`.
Repeated evaluations share
this identity even if the sensor configuration changes between attempts;
downstream processors account for repeated readings.
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

Readings preserve ingestion identity separately from billing attribution:
`actor` contains the persisted message's Gram user ID and external user ID plus
the ingestion event's observed email;
`billing_user_id` is the producer's explicit usage allocation, including for
assistant-generated messages with no actor. Neither identity is inferred from the
other. `source` namespaces external identities within the organization. Account
ID comes from the current conversation row; account type/billing mode, assistant
ID and the historical `replayed` marker come from the event's ingestion context.
Absent provenance remains absent. Directory
enrichment belongs downstream, as it does for agent-session storage metering.
These fields do not participate in reading identity or the definition hash.

Successful readings are published and their publication results awaited before
acknowledgment.
Readings exceeding 9 MiB of serialized protobuf are rejected before publication,
leaving headroom under Pub/Sub's 10 MiB limit. They are permanent failures with
reason `reading_too_large`; other successful readings are still published.
Permanent failures emit error logs and
`gram.sigint.evaluation.failures` with a bounded `reason` attribute. Transient
classification/storage failures and publication failures nack the source delivery.
Redelivery may reevaluate all sensors and republish successes with the same
logical reading ID and different answers/attempt IDs. No evaluation checkpoints,
DLQ, reading persistence or duplicate-winner policy are introduced.

Conversation and reading topics and the evaluator subscription retain messages
for four days; acknowledged subscription messages are not retained. Provision
the generated infrastructure and configure the key before enabling consumption.

Platform MCP assessment: this is an internal ingestion/evaluation capability,
with no new administrator action or reading query API. Tool exposure remains
deferred until signals intelligence is production-ready. Existing demo sensor
configuration remains applicable; this layer adds no dashboard-rendered data.

Temporal actions/month: 0 (Pub/Sub batch handler, no Temporal).
