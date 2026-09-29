# Sensor evaluation

`gram streams` consumes `gram.conversation.v1.Message` through
`gram.sigint.v1.Evaluator` and publishes successful `gram.sigint.v1.Reading`
messages. Configure **`GRAM_SIGINT_OPENROUTER_API_KEY`** with a platform-owned
OpenRouter inference key. The receiver is not registered when this is unset.
The shared Jev client uses this key for every tenant; evaluation does not resolve
customer credentials or provision organization OpenRouter keys.

The organization must have `signals_intelligence` enabled. A single SQL statement
loads active project-owned sensor definitions and signal membership in order.
Only user and assistant messages are evaluated, including historical replays.
Four messages can be evaluated concurrently; the shared Jev client independently
caps in-flight provider requests at four and uses Guardian rate admission.

## Compilation and readiness

Based on the [System One request schema](https://openrouter.ai/docs/api/api-reference/systemone/submit-a-system-one-request):

- Instructions must be present (an empty string is a valid API value).
- `multi_label`: one Noul question per signal. Missing criteria are optional;
  absent Noul criteria are omitted, while one-sided criteria serialize the other
  side as an empty string rather than null.
- `exclusive`: one Choice question, 1–255 options; null descriptions are allowed.
- `ordered_score`: one Score question, 2–10 ordered, non-null level descriptions.
- Empty sensors and incomplete definitions are skipped, counted as `draft` in
  `gram.sigint.evaluation.skipped`.

Every runnable sensor contributes to one logical classifier request per message.
A multi-label reading requires all its Noul outcomes to succeed.

## Identity and content

Reading identity is a UUIDv5 derived from a versioned JSON tuple containing organization,
project, immutable logical message ID and sensor ID. Repeated evaluations share
this identity even if the sensor configuration changes between attempts;
downstream processors account for repeated readings.
The fixed namespace is UUIDv5(URL namespace, `gram:sigint:reading`). The UUID is
serialized as a canonical string on the wire and can be stored in native UUID
columns in PostgreSQL and ClickHouse.
Each evaluation has a separate attempt UUID and completion timestamp.
Sensor and signal slugs are always populated in readings from the same configuration
snapshot. Choice results also carry the selected signal's slug. Slugs are metadata
and do not affect reading identity.
Definition hashes are metadata for traceability, not identity, and include mode, instructions,
signal IDs, criteria and order. Message content
is not hashed for reading identity or metadata. Inline and spilled protobuf bodies
resolve to the same input. Source JSON preserves number representations; tool
calls and referenced textual content remain available to classification.

Asset reads use the configured storage backend, enforce project paths, check
declared hashes/sizes, and cap resolved input at 16 MiB. Unsupported binary parts,
malformed content, missing assets and integrity failures are permanent failures.
Other storage errors retry.

## Delivery and observability

Successful readings are published and their publication results awaited before
acknowledgment.
Readings exceeding 9 MiB of serialized protobuf are rejected before publication,
leaving headroom under Pub/Sub's 10 MiB limit. They are permanent failures with
reason `reading_too_large`; other successful readings are still published.
Permanent failures emit error logs and
`gram.sigint.evaluation.failures` with a bounded `reason` attribute. Transient
classification/storage failures and publication failures nack the message.
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
