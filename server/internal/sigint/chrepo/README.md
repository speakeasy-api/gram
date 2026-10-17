# Sensor reading storage

The `ReadingCHWriter` and `OutcomeCHWriter` subscriptions are batch consumers of
`gram.sigint.v1.Reading` and `gram.sigint.v1.Outcome`, independent of the lake
subscription. Both synchronously insert complete attempts into `sigint_readings`. Malformed messages are logged without label contents and
acknowledged. Storage errors nack the batch, so duplicate rows are expected.

## One row per logical reading

The table uses `ReplacingMergeTree(evaluated_at)`: one row per reading ID (one
sensor's outcome for one source event), with the latest evaluation winning. Source
occurrence time is immutable for a logical reading and is part of its monthly
partition and sorting key, so attempts converge in one partition. The conversation
adapter takes it from the creation event, never from later row updates. A future
producer must preserve that invariant. Equal completion times use ClickHouse's
latest-insert tie break.

Merges are eventual. Readers deduplicate by ID before counting, with `FINAL` or
`LIMIT 1 BY id` ordered by `evaluated_at DESC, inserted_at DESC`. The second key
approximates the latest-insert tie break, since a later batch carries a later
`inserted_at`. Rows of one batch tie on both, so only `FINAL` is exact for equal
completion times. Retries must never inflate counts.
Do not add incremental materialized views over this table: they observe every
physical insert, including duplicates, and replacement does not retract them.

## Outcomes and coverage

Readings land as `answered`. Outcomes land as `not_applicable`, `draft` or
`failed` (with a bounded snake_case `failure_reason`). They leave the result type,
probabilities, selected signal, expected index and confidence empty.
The Pub/Sub writers validate every message with `ParseReading` or `ParseOutcome`
before insert. Demo seed rows bypass them, and
`TestSeededSigintReadingsSatisfyWriterRules` holds them to the same rules. The
table has no CHECK constraints, so rules can change without a migration. An
outcome shares its reading's ID and source time, so replacement on `evaluated_at`
picks the latest attempt across both topics: a later answer replaces an earlier failure and vice versa. Coverage
counts outcomes and messages that never produced a row. It does not drop them.
Draft outcomes have no definition hash.

`Outcome` nests self-contained copies of `Reading`'s event, actor, account and
input messages, because Pub/Sub topic schemas cannot import other types. The
copies match field for field, so `ParseOutcome` converts them through the wire
format. `TestOutcomeContextMirrorsReading` fails when they drift.

`input_*` columns record the message context an attempt actually supplied:
strategy, requested and supplied message counts including the target, and budget
truncation. Readings published before that field existed leave them empty.

## Limits

Storage accepts at most 64 attribute labels, 32 KiB of combined label keys and
values, 1024 signal probabilities and a 9 MiB protobuf. These are payload budgets,
not restrictions on label vocabulary. Retention is two calendar years of source
time. The lake keeps the complete protobuf archive.

## Validation and rollout

Run `mise run test:server ./internal/sigint/...` and the demo seed safety tests.
Apply the schema migration before the subscriber and its topology revision.

## Run-scoped write target

`sigint_readings_v2` is provisioned separately for a subsequent writer cutover.
Its result and attribution columns match `sigint_readings`. The new write contract
adds three fields, carried identically by answered readings and terminal outcomes:

| Column                    | Streaming evaluation                                     | Manual evaluation                             |
| ------------------------- | -------------------------------------------------------- | --------------------------------------------- |
| `trigger_kind`            | `stream`                                                 | `manual`                                      |
| `run_id`                  | Zero UUID                                                | Nonzero UUID assigned to the accepted request |
| `evaluation_requested_at` | Creation mutation's immutable `MessageEvent.occurred_at` | Accepted run's immutable creation timestamp   |

`MessageEvent.message_created_at` supplies the source `event_occurred_at`, not
the request timestamp. Historical imports can therefore retain their original
message time while receiving the same evaluation-result lifetime as a manual run.
All targets in a manual run share its run ID and request timestamp. Retrying a
request preserves them, while an intentional new run receives a new ID.

### Identity and replacement

The cutover producer must derive reading IDs with the existing UUIDv5 namespace and these
versioned JSON tuples:

```text
["sigint-reading-v2", organization_id, project_id, "stream", event_kind, event_id, sensor_id]
["sigint-reading-v2", organization_id, project_id, "manual", run_id, event_kind, event_id, sensor_id]
```

The manual run ID uses its canonical UUID string. A reading and an outcome for
the same evaluation use the same identity. `evaluation_attempt_id` changes on
each execution, but is absent from the identity and replacement key.

The sorting key is `(organization_id, project_id, trigger_kind, run_id, sensor_id,
event_occurred_at, id)` and the sparse primary key omits the final `id`. Streaming
and separate manual runs cannot replace one another. Within a scope, the highest
`evaluated_at` replaces the complete row, including transitions between answered
and unanswered statuses. Status, definition hash and attempt ID stay outside the
sorting key so they can change between attempts.

### One retention clock

Both triggers partition monthly and expire two calendar years after
`evaluation_requested_at`. The producer must supply that timestamp explicitly
and preserve it across retries and redelivery, including retries crossing a month
boundary. Neither the worker's current time nor `evaluated_at` may be substituted
on retry. A new evaluation of an old message therefore has a fresh result lifetime
regardless of trigger, and late attempts do not extend their original lifetime.

`event_occurred_at` remains immutable within a logical reading because it is part
of the sorting key. Use it for source-time analytics. A source-time predicate does
not imply a request-time partition range, especially for historical imports or
manual evaluations of old messages.

### Read contract

On ClickHouse 26.6, use scoped `FINAL` reads when exact latest-attempt results are
required. Streaming sensor history constrains organization, project, `stream`, the
zero run UUID, sensor and a bounded source-time range. Manual-run inspection
constrains organization, project, `manual` and an exact run UUID. Select the needed
columns and verify primary-key pruning with `EXPLAIN indexes = 1`. Benchmark peak
memory with fresh overlapping parts and representative concurrent reads.

Background replacement reduces duplicate deliveries without a guarantee that all
duplicates disappear. Ordinary reads can observe multiple attempts. Incremental
aggregates over physical inserts do not retract replaced attempts.

The scope and partition invariants follow
[ClickHouse replacement and partition semantics](https://clickhouse.com/docs/en/guides/replacing-merge-tree).

### Cutover requirements

Before directing writes to this table, update both protobuf event contracts and
their generated bindings, the shared reading-ID function, producers, parsers,
outcome-to-reading context conversion, insert columns and demo seed. Writers must
reject unknown triggers, a nonzero streaming run ID, a missing/zero manual run ID,
or a missing/invalid request timestamp before batching. Preserve all current
result and attribution validation. The migration creates the target schema only.

## Platform MCP assessment

Outcome: internal ingestion only. No product read path exists yet, so there is no
new Platform MCP tool. When one is added, the dashboard and Platform MCP should
read readings through one analytics catalog dataset, which keeps one set of
query semantics.
Temporal actions/month: 0, Pub/Sub batch handling only.
