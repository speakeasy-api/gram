# Product metrics

Internal tenant-scoped analytical measurements, independent of product facts,
billing attribution and existing raw agent metrics. Producers register code-owned
definitions before publishing. No customer API, dashboard, exporter or production
producer is supplied here.

A **contribution** is one producer-owned measurement that adds to an aggregate:
a Counter delta increment or a single Histogram observation.

## Contract

- Counter inputs are nonnegative monotonic **delta increments**. Histogram inputs
  are individual observations, aggregated as count/sum/min/max, without buckets or
  quantiles. Neither input is an invented OTLP point type. Cumulative counters,
  gauges and preaggregated intervals are unsupported.
- Names follow OTel metric syntax. Gram-owned names use `gram.<domain>.<quantity>`;
  use UCUM units (`s`, `By`, `1`, `{evaluation}`), not units in names. Producers
  are responsible for choosing valid UCUM units and semantic conventions.
- A registry rejects incompatible unit/instrument definitions for a scope/name,
  including across instrumentation versions. Rename incompatible metrics. A scope
  version identifies instrumentation, not automatically a new metric meaning.
- Organization and project are mandatory typed ownership. The canonical resource
  adds custom `gram.organization.id` and `gram.project.id` attributes for future
  export. Producers must omit these reserved resource keys.
- Resource, scope and point attributes have separate namespaces. Sorted keys and
  explicit scalar/array type tags preserve identity, including missing vs empty,
  int64 vs float64, and empty arrays. Duplicate keys and nonfinite values fail.
  Descriptor description is metadata. Integer values never pass through float64.
- Producers own dimensions and cardinality. There is no central dimension allowlist,
  identity enrichment, series catalogue or hard series cap. Do not automatically
  add user IDs, delivery IDs or observation IDs as dimensions.
- Contribution IDs identify **observations**, not series. Allocate distinct IDs
  for distinct measurements, even if values and timestamps coincide. Retry with
  the same ID and unchanged payload. Identity is scoped by tenant and instrumentation
  scope, including its version. Producers must preserve the ID, event timestamp
  and payload across retries and reprocessing, and minimize duplicate publication:
  live rollups remain duplicate-inclusive. Best-effort batch suppression is not an
  exactly-once guarantee.
- Await the publisher result. Product transaction/outbox integration stays with
  each producer; a database commit and direct Pub/Sub publish are not atomic.

## Assessment

Temporal actions/month: **0**. Event-driven ingestion belongs in a Pub/Sub batch
handler, without workflow starts or signals.

Platform MCP: omit. The outcome is internal contribution ingestion for trusted
code-owned producers, targeting tenant-scoped analytical storage. There is no
member/admin operation or public authorization contract to expose, and no existing
tool's customer behavior changes. Contract tests provide synthetic evidence.
Demo seed: no dashboard data consumer, so use deterministic synthetic fixtures.

## Deployment and consumption

1. Provision the generated Pub/Sub topology for the target environment. Both the
   topic hint and subscription retain messages for four days; acknowledged
   messages are not retained and there is no DLQ.
2. Apply the ClickHouse migration with the existing infrastructure-owned database
   and application credentials. The writer needs INSERT on the source and MV
   targets; the repository needs SELECT on the targets. No marts grants change.
   Keep writers paused throughout the raw engine-swap migration or rollback;
   it copies retained rows and reattaches the incremental views without replaying
   their increments. Keep consumption disabled during initial rollout.
3. Register each producer's definitions at the streams composition root and in
   its publisher. The generic stack registers no production definitions. Unknown
   or conflicting descriptors are permanently invalid, not learned into a cache.
4. Set `GRAM_PRODUCT_METRICS_CONSUME=true` on `gram streams`. It is an independent
   deployment switch, unrelated to risk/signal evaluation or inference keys.
5. Activate producers only after the subscriber is healthy. Await publish
   confirmation; producers choose their transactional publication boundary.

Flush thresholds: 1,000 messages, 10 MiB, or one second. Receive buffers are
bounded at 20 MiB. A byte threshold is a flush trigger and can overshoot by one
Pub/Sub message (at most the broker's 10 MiB payload bound). The consumer performs
one synchronous insert per batch (`async_insert=0`,
`materialized_views_ignore_errors=0`, `insert_deduplicate=0`). It acknowledges
valid messages only after insertion and dependent materialized-view processing
return successfully. Permanently invalid decoded contributions are acknowledged
individually even when valid neighbors retry. This subscription opts into dropping
undecodable protobufs as well, avoiding an endless poison-message retry without a
DLQ. Shared subscriber defaults remain retry-on-decode-error.

Within a batch, tenant/scope/version/contribution-ID identifies a delivery. Equal
canonical payloads suppress repeated delivery; changing a payload under that ID is
an invalid identity conflict (the first observation is retained). Distinct IDs are
always distinct observations. Description metadata is excluded from equality.

An insert error or timeout can occur after source or target blocks were persisted.
Retries can inflate counts/sums and weight means toward retried deliveries. Source
and multiple materialized views are **not** an atomic transaction. Neither raw
retention nor best-effort batch suppression provides exactly-once processing or a
guarantee that every target equals every source row after ambiguous failures.

## Windows and read guarantees

- Rollups represent UTC half-open minute windows `[start, start+1m)`. Event time
  selects the bucket; observed time and ingestion time do not alter series identity.
- Counter rows hold monotonic delta sums; Histogram rows hold count/sum/min/max.
  Merge keys contain tenant/project, descriptor, numeric representation and all
  resource/scope/point attributes. Explicit `sum`/`min`/`max` regrouping is correct
  before and after background merges. No serving query uses `FINAL`, source-event
  winners, deduplication, or raw contribution scans.
- Integers are summed in Int128 and returned as `big.Int`; float series stay
  separate. `Mean` divides total sum by count, preserving integer rational means.
  IEEE-754 rounding applies to floating sums; nonfinite returned totals fail.
- Reads require organization, project, scope name/version, metric, unit, instrument,
  and a nonempty whole-minute range. Intervals are positive whole minutes aligned
  to the Unix epoch. First/last buckets are clipped to the query range and marked
  `Partial`; current windows are also marked partial. Missing minutes yield no rows.
- Retention is 90 days by bucket start. The earliest supported minute is the
  ceiling of `now-90d`. Both reads and ingestion reject earlier data, independent
  of asynchronous TTL deletion. Producers may deliver late data into retained
  buckets; these are not finalized windows. Future events are accepted only within
  five minutes of clock skew, and queries never extend beyond the current minute.
- Arbitrary retained keys support typed equality, missing-key filtering and dynamic
  grouping in each namespace. Missing groups are distinct from empty-string or
  empty-array groups. Grouping reduces unselected dimensions explicitly within the
  chosen tenant/descriptor. Adding attributes requires no schema/MV changes.
- Queries fail rather than truncate when they exceed 10,000 groups/results,
  10 million scanned rows, 1 GiB scanned bytes, 256 MiB query memory, 16 MiB returned
  data, or ten seconds. Up to 32 filter/group expressions bound query complexity,
  not producer dimensions or stored series. These are defensive execution budgets,
  not promised latency/scale acceptance criteria.
- Raw contributions and rollups are retained for 90 days from event-minute start.
  Raw storage uses `ReplacingMergeTree(ingested_at)`, monthly UTC event-time
  partitions, and the organization/project/scope-name/scope-version/contribution-ID
  replacement key. Event-time partitioning keeps retries in the same partition.
  `ingested_at` selects the latest delivery copy, not a measurement correction.
  Background replacement is eventual and does not retract live MV increments.

## Manual rollup repair

Raw contributions provide evidence for an operator-run, out-of-band repair within
the retained horizon. They do not provide automatic reconciliation. Producers must
minimize duplicate publication and preserve immutable IDs, timestamps and payloads
across retries and reprocessing; conflicting payloads across batches cannot be
reliably repaired by picking a delivery copy.

1. Select affected event-month partitions and a fixed retained-minute cutoff.
   Pause and drain all contribution writers for the repair, including late arrivals
   and retries, until replacement finishes. Coordinate readers if they require a
   consistent view across both rollup tables; the replacements are not one atomic
   transaction.
2. Read retained raw contributions with explicit deduplication (`FINAL`) into
   isolated aggregate targets using the live MVs' grouping and aggregate formulas.
   Rebuild every retained row for each affected partition, across all tenants and
   metrics: replacing a monthly partition with a tenant-only rebuild loses data.
   Finish before the source evidence reaches its TTL boundary; expired evidence
   requires producer-owned facts.
3. Compare counts, sums, extrema and tenant/series coverage. Replace each affected
   rollup partition from the isolated target, rather than appending repaired totals.
   Handle partitions with no retained source rows explicitly. Never replay raw rows
   into the live source, which adds increments again.
4. Verify both rollup targets before resuming writers. Later deliveries, including
   retries of repaired observations, remain duplicate-inclusive and may require
   another repair. Keep recovery copies until validation completes.

`server/clickhouse/tests/product_metrics_rollups.sql` demonstrates deduplicated
counter/histogram rebuilding and partition replacement in a disposable database.
It is a validation fixture, not a production repair command.

## Operational and analytical diagnostics

`gram.product_metrics.contributions` has only the bounded `outcome` dimension:
inserted, invalid, unregistered, expired, future, duplicate, identity_conflict, retry.
There are no tenant, project, metric-name or attribute-value service telemetry
labels. Decode failures are logged by the shared subscriber.

`Repository.Diagnostics` returns approximate active/new series, delivery count and
typed attribute diversity under the same scoped query limits. New means first seen
within the selected retained lookback, not lifetime novelty; late arrivals and
approximation can change it. These are analytical results, not telemetry labels.
Query buckets expose delivery count for contributions-per-bucket diagnostics.

Operators can inspect global physical storage and scanned work without a series
catalogue or high-cardinality service metrics:

```sql
SELECT table, count() AS parts, sum(rows) AS rows, sum(bytes_on_disk) AS bytes
FROM system.parts
WHERE active AND database = currentDatabase()
  AND table IN ('product_metric_contributions', 'product_metric_sums_1m',
                'product_metric_histograms_1m')
GROUP BY table;

SELECT query_id, query_duration_ms, read_rows, read_bytes, memory_usage
FROM system.query_log
WHERE type = 'QueryFinish' AND event_time >= now() - INTERVAL 1 HOUR
  AND hasAny(tables, [concat(currentDatabase(), '.product_metric_sums_1m'),
                     concat(currentDatabase(), '.product_metric_histograms_1m')])
ORDER BY event_time DESC LIMIT 100;
```

## Synthetic capacity evidence

Run the opt-in capacity experiment with:

```sh
GOTESTSUM_FORMAT=standard-verbose mise run test:server -tags=productmetrics_bench -run '^TestSyntheticLoad$' -v ./internal/productmetrics/
```

2026-09-29 local ClickHouse 26.2.19.43: 120,000 contributions each for high- and
low-repetition series, 90% of traffic in one synthetic tenant, 2/20 attributes,
1,000-record synchronous batches, merges stopped. Event times span 30 days, with
half of observations concentrated in the most recent 24h. Unlike the isolated
encoding probe, this testcontainer used host resource limits (32 GiB Linux host).

| Scenario        | Writes/sec | 24h read p50/p95 | 30d read p50/p95 |
| --------------- | ---------: | ---------------: | ---------------: |
| High repetition |     18,044 |     100 / 130 ms |      91 / 153 ms |
| Low repetition  |     19,840 |      88 / 130 ms |     103 / 130 ms |

Reads use dynamic typed filtering/grouping with four concurrent readers, 20
samples per case, against unmerged parts. High repetition scanned at most 2,400 /
3,550 rows, 0.49 / 1.79 MB, and 1.57 / 1.96 MB peak query memory for 24h/30d.
Low repetition scanned 119,700 / 122,400 rows, 84.5 / 87.5 MB, and 2.72 / 3.00 MB
peak memory. The high-repetition fixture compacted 120,000 observations to 3,600
rollup rows before merges (33× reduction), 240 parts / 0.54 MB on disk. After adding
the low-repetition fixture: 123,600 total rows, 367 parts / 2.14 MB. Unique series
do not materially reduce, as expected.

Writes exceed the agreed 1,500/sec proxy (10× the observed production peak
published-log rate, rounded up). This is a short synthetic throughput experiment,
not a prolonged soak or Cloud guarantee. Read latency/memory acceptance and actual
concurrent-read demand remain open. Encoding comparison, index pruning evidence
and migration replay results live in the ClickHouse SQL fixture documentation.
