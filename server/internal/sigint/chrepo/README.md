# Sensor reading storage

`ReadingCHWriter` is a Pub/Sub batch consumer of `gram.sigint.v1.Reading`,
independent of the lake subscription. It synchronously inserts complete attempts
into `sigint_readings`. Malformed messages are logged without label contents and
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

`status` is `answered` for every published reading today. The column also admits
`not_applicable`, `draft` and `failed` (with `failure_reason`), so a later producer
can record why an eligible message has no answer. Coverage counts these outcomes
and messages that never produced a row. It does not drop them. Only answered rows
carry a result type, probabilities, a selected signal, an expected index or a
confidence. `ParseReading` validates every row before insert. The table has no
CHECK constraints, so rules can change without a migration.

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

## Platform MCP assessment

Outcome: internal ingestion only. No product read path exists yet, so there is no
new Platform MCP tool. When one is added, the dashboard and Platform MCP should
read readings through one analytics catalog dataset, which keeps one set of
query semantics.
Temporal actions/month: 0, Pub/Sub batch handling only.
