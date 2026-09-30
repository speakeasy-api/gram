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
- Producers own dimensions and cardinality. `Registry.RegisterDimensions` installs
  a per-descriptor contract of namespace/key/type and optional allowed typed values.
  Publisher and consumer both enforce installed contracts; an empty contract permits
  no producer attributes. Unconfigured definitions retain generic semantics for
  internal/synthetic use. Production adapters should explicitly install contracts
  in both composition roots. There is no global dimension allowlist, identity
  enrichment or hard ingestion series cap. Do not automatically
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
   targets and catalogue; the repository needs SELECT on those tables. No marts grants change.
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

## Series catalogue and tier selection

Raw contributions retain full typed attributes. A materialized SHA-256 fingerprint
identifies tenant/project, descriptor, numeric kind and canonical resource/scope/point
attributes; it excludes description, observation IDs and timestamps. Serving tables
store that fingerprint instead of repeating attributes. The catalogue's merge key
also retains full canonical identity, so a digest collision does not discard either
identity. Reads reject conflicting catalogue entries before filtering.

Counter and Histogram each have one-minute, hourly and UTC daily targets, populated
directly by independent raw-insert MVs. Catalogue min/max updates are idempotent;
numeric rollups remain duplicate-inclusive. There is no transaction spanning these
targets. Queries discover active IDs from the exact selected rollup ranges and
verify catalogue coverage before applying filters. Missing metadata fails the read
with a repair error rather than silently omitting measurements. A missing rollup
write still requires operator comparison/repair from raw evidence.

The planner chooses the coarsest tier that divides the requested epoch-aligned
interval exactly. It uses finer tiers for partial edges. A 90-day daily chart can
use daily interiors, hourly edges and minute tails; a seven-minute interval uses
minute rows. Requests never silently change resolution. UTC days are the contract;
these tiers do not represent arbitrary local-time calendar days.
Serving sort keys put time before series ID, after the fixed tenant/descriptor
prefix. This favors broad-series range queries and exact boundary pruning. The
implementation uses bounded hash aggregation rather than relying on an in-order
aggregation optimization that the attribute-grouped query plan does not provide.

Every read has a ten-second end-to-end deadline, including waiting for one of four
process-wide read slots and all discovery/catalogue phases. Discovery is bounded
at 250,000 active series per descriptor/range, then filtering at 50,000 matched
series. Matched series multiplied by requested output buckets must not exceed
10 million. These conservative budgets supplement the scan/memory/result limits;
they are not an exact cost formula. ID sets use native external tables with bounded
set/join memory. Attributes are evaluated in the catalogue, and only the selected
group labels join narrow numeric history. Concurrent ingestion is not a snapshot:
new series arriving after discovery appear on a subsequent read.

## Windows and read guarantees

- Rollups represent UTC half-open minute/hour/day windows. Event time
  selects the bucket; observed time and ingestion time do not alter series identity.
- Counter rows hold monotonic delta sums; Histogram rows hold count/sum/min/max.
  Merge keys contain tenant/project, descriptor, series fingerprint and numeric
  representation. Explicit `sum`/`min`/`max` regrouping is correct
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
  Hourly/daily storage and catalogue metadata have a 91-day physical TTL guard so
  they cannot expire while overlapping the 90-day supported range; that guard day
  is never exposed through queries. Minute/raw TTLs remain 90 days.
- Arbitrary retained keys support typed equality, missing-key filtering and dynamic
  grouping in each namespace. Missing groups are distinct from empty-string or
  empty-array groups. Grouping reduces unselected dimensions explicitly within the
  chosen tenant/descriptor. Adding attributes requires no schema/MV changes.
- Queries fail rather than truncate when they exceed 10,000 groups/results,
  10 million scanned rows, 1 GiB scanned bytes, 256 MiB query memory, 16 MiB returned
  data, or ten seconds. Up to 32 filter/group expressions bound query complexity,
  not producer dimensions or stored series. These are defensive execution budgets,
  not promised latency/scale acceptance criteria.
- Raw contributions are retained for 90 days from event-minute start.
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
   Restore catalogue metadata from canonical raw identities into an isolated
   catalogue target, validating one canonical identity per fingerprint. A true
   fingerprint collision needs a coordinated identity-format migration, not an
   arbitrary winner. Rebuild minute, hourly and daily targets consistently from
   the same deduplicated source snapshot. The finest repaired aggregates may also
   be regrouped into coarse targets without replaying the live source.
3. Compare counts, sums, extrema and tenant/series coverage. Replace each affected
   rollup partition from the isolated target, rather than appending repaired totals.
   Handle partitions with no retained source rows explicitly. Never replay raw rows
   into the live source, which adds increments again.
4. Verify the catalogue and all six rollup targets before resuming writers. Later deliveries, including
   retries of repaired observations, remain duplicate-inclusive and may require
   another repair. Keep recovery copies until validation completes.

`server/clickhouse/tests/product_metrics_rollups.sql` demonstrates deduplicated
counter/histogram rebuilding, all-tier partition replacement and catalogue recovery
in a disposable database.
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

Operators can inspect physical storage and scanned work without high-cardinality
service telemetry labels:

```sql
SELECT table, count() AS parts, sum(rows) AS rows, sum(bytes_on_disk) AS bytes
FROM system.parts
WHERE active AND database = currentDatabase()
   AND startsWith(table, 'product_metric_')
GROUP BY table;

SELECT query_id, query_duration_ms, read_rows, read_bytes, memory_usage
FROM system.query_log
WHERE type = 'QueryFinish' AND event_time >= now() - INTERVAL 1 HOUR
  AND arrayExists(t -> startsWith(t, concat(currentDatabase(), '.product_metric_')), tables)
ORDER BY event_time DESC LIMIT 100;
```

## Synthetic capacity evidence

The opt-in `TestSeededSeriesQueries` exercises the complete Go read path against
an explicitly selected synthetic database:

```sh
PRODUCT_METRICS_BENCH_DSN='<CLICKHOUSE_DSN>' GOTESTSUM_FORMAT=standard-verbose mise run test:server -tags=productmetrics_bench -run '^TestSeededSeriesQueries$' -v ./internal/productmetrics/
```

It uses `gram.synthetic.requests` / `gram.synthetic.duration` for `synthetic-large`
in the placeholder project `00000000-0000-4000-8000-000000000001`, scope
`gram.synthetic` version `1`. `PRODUCT_METRICS_BENCH_SUFFIX` selects another seeded
profile. It reports 24h/five-minute, 30d/hourly and 90d/daily results with model and
environment filters, optional region grouping, one/four readers, and per-request
scan totals across all four phases. Rejected workloads are reported explicitly.
The SQL seed fixture documents the reproducible bounded/high-cardinality profiles.

### Catalogue/tier benchmark, 2026-09-30

ClickHouse 26.2.19.43, two CPUs / 4 GiB RAM, six million contributions per profile
(three million per instrument), spread across 90 days. Warm caches, compacted
tables, no simultaneous ingestion, 20 samples per case. Measurements include the
complete Go query path and all discovery/catalogue phases. The bounded profile has
at most 1,024 series per tenant/instrument; filters select up to 128 of them.
Both filters (`model-0`, production environment) and eight-region grouping apply:

| Range / interval | Counter p95, 1 / 4 readers | Histogram p95, 1 / 4 readers | Counter total scan |
| ---------------- | -------------------------: | ---------------------------: | -----------------: |
| 24h / 5 minutes  |                89 / 193 ms |                 125 / 185 ms |           39.9 MiB |
| 30d / 1 hour     |             276 / 1,298 ms |                 217 / 939 ms |          167.0 MiB |
| 90d / 1 day      |               166 / 425 ms |                 148 / 343 ms |           79.0 MiB |

Peak server memory per phase for grouped bounded-profile queries was approximately
23–28 MiB. Daily resolution scans less than 30 days at hourly resolution. Moving
time ahead of series ID reduced the bounded-profile 90-day total scan from about
700 MiB to 79 MiB, especially by avoiding broad monthly scans for partial edges.
The point budget rejects high-cardinality 30-day hourly requests rather than
silently changing resolution; those callers need coarser intervals or narrower
filters. In the final time-first high-cardinality run, 90-day counter queries
completed at 3.16 seconds p95 with one reader, but four readers hit the ten-second
request deadline. That stress run did not pass its concurrency experiment; the
catalogue and tiers do not make unconstrained dimensions inexpensive. The earlier
series-first run also showed substantial catalogue cost. These short local
experiments are directional evidence, not Cloud SLOs.

The measurements below are historical evidence for the original wide minute-only
layout, not acceptance results for the catalogue/tier implementation.

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
