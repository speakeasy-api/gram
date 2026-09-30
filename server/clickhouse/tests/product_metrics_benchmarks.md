# Product metrics encoding evidence

The encoding measurements below describe the attribute representation, not the
current serving layout. Serving rollups are narrow series-ID tables. Full typed
attributes live in raw contributions and the series catalogue.

## Series catalogue and resolution tiers

The raw table materializes a SHA-256 series fingerprint over tenant/project,
descriptor (including instrumentation version and instrument), numeric kind, and
all three canonical typed attribute arrays. Description and delivery IDs are not
series identity. Producers sort and validate attributes before insertion.

The catalogue retains full identity in its merge key, including the fingerprint,
so conflicting canonical identities cannot replace each other. Queries validate
active catalogue identities before filtering and reject missing/conflicting entries
rather than silently dropping or combining measurements. No serving query uses
`FINAL`. ID sets travel as native external tables, not interpolated SQL lists.

Six narrow serving tables hold Counter and Histogram aggregates at one-minute,
one-hour and UTC one-day resolutions. All six MVs consume raw insert blocks
directly. Source replacement does not retract any tier's duplicate increments.
The catalogue has a seventh idempotent min/max MV. MV writes are not atomic;
ambiguous failures need retry and, if necessary, operator repair.

The supported query horizon remains 90 days. Minute/raw TTLs are 90 days. Coarse
tiers and catalogue metadata have a 91-day physical TTL guard so a bucket or its
metadata cannot expire while it still overlaps the supported horizon. Queries
never expose the guard day. Fine tiers answer partial coarse-bucket boundaries.

Migration preserves existing minute delivery totals, derives hourly/daily totals
from them, and reconstructs catalogue entries before discarding repeated attributes
from serving rows. Pause writers throughout. Local rollback restores attributes
from checked catalogue identities and preserves minute totals. Repair fixtures
exercise all six target tables and catalogue recovery from retained raw data.

## Method

`product_metrics_encoding.sql` is executable against a disposable database on
ClickHouse 26.2.19.43. It compares canonical typed JSON text against native
`Array(Tuple(key String, type String, value String))`. Values are canonical JSON
scalars/arrays with explicit OTel type tags, preserving int64 precision and empty
arrays. Native tuples allow `has` and `arrayFirst` filters without parsing JSON
per row. Attribute namespaces use separate columns.

One million synthetic records span 30 days, with 90% assigned to one tenant,
two or twenty attributes per series, and repeated/unique series mixed. Container:
2 CPU, 2 GiB memory. Query settings: two threads, 8,192-row blocks. These are
single-run local measurements, not Cloud latency guarantees.

| Query               | Tuples |   JSON | Tuple memory | JSON memory |
| ------------------- | -----: | -----: | -----------: | ----------: |
| 24h equality filter |  13 ms |  32 ms |      23.6 MB |     30.7 MB |
| 30d equality filter | 117 ms | 514 ms |      25.1 MB |     31.0 MB |

24h scanned 57,344 rows / 32.1 MB for tuples and 49,152 / 21.6 MB for JSON.
30d scanned 909,888 rows / 599.8 MB for tuples and 909,888 / 478.0 MB for JSON.
On-disk size: tuples 8.1 MB (3 parts), JSON 65.7 MB (2 parts). Merge timing explains
the part-count/granule differences. `EXPLAIN indexes=1` pruned to 7/123 granules
for the tuple 24h query, using organization, project, metric and time. Both
representations returned identical totals (360 and 10,000).

An initial unrestricted bulk load exceeded the worktree's 2 GiB container limit.
Bounded blocks and threads completed successfully. This supports bounding insert
and query resources explicitly, rather than relying on server defaults.

Decision: native typed tuples, with full canonical identity in the rollup merge key.
No hash-only identity, JSON dynamic-path limits, fixed dimension columns, or
attribute-name allowlist. Serving keys start organization/project/metric/time.

## Workload proxy and acceptance

Production aggregate Pub/Sub published-log volume was inspected through Datadog
on 2026-09-29, using message-size **sample counts**, not publish-operation counts.
The observed seven-day average was about 15 messages/sec and maximum one-minute
rate about 145/sec. Inbound OTel logs were lower (about 8/sec average, 111/sec
peak), so the broader telemetry log topic is the conservative proxy. Do not add
both: pipeline stages overlap. Data trailed wall-clock time by about an hour.

The synthetic write target is **1,500 contributions/sec**, rounded above 10× the
observed peak. This assumes one contribution per message for sizing only. Real
producers must multiply by contributions per message and measure their chosen
series cardinality. This is neither a unique-message count nor a production SLA.

Read latency/memory acceptance and expected concurrent reads remain an explicit
review question. Benchmarks report evidence rather than claiming an unagreed SLA.

## Raw identity and retention

The raw table uses `ReplacingMergeTree(ingested_at)`, monthly UTC event-time
partitions, and an organization/project/scope-name/scope-version/contribution-ID
replacement key. IDs are selected by producers and identify immutable observations.
Retries and reprocessing must preserve the ID, event timestamp and payload.
Distinct observations with identical timestamps/attributes keep distinct IDs.
`ingested_at` selects the latest delivery copy, not a correction to its measurement.

Raw and rollup retention are both 90 days from the event-minute start. This keeps
the same historical repair horizon while stakeholder requirements are clarified.
Replacement merges do not retract increments from the live rollups. Raw `FINAL`
reads support debugging and explicit out-of-band rebuilding, without introducing
deduplication to serving queries or an automatic reconciliation loop.

## Migration and aggregate validation

Atlas `migrate validate --env clickhouse` replayed the Atlas history against its
configured sandbox. The repository's `clickhouse:migrate --engine golang-migrate`
task replayed all local migrations against a fresh database provisioned with the
standard local principals. Both completed successfully.

`product_metrics_rollups.sql` passed against the migrated database. It checks
duplicate inserts across blocks, exact sums beyond int64, typed attributes and
tenant/project/resource/scope separation, weighted histogram means across minute
buckets, and identity preservation after merges. It also proves raw replacement
across different ingestion days, tenant/project/scope/version/ID isolation, 90-day
event-time TTL, unchanged duplicate-inclusive rollups after raw compaction, and
manual counter/histogram repair using isolated targets and partition replacement.
Pass `--param_repair_partition=<YYYYMM for UTC now minus two minutes>` when running
the fixture. Serving assertions use explicit aggregates without `FINAL`.
`FINAL` raw reads and `OPTIMIZE ... FINAL` are confined to rebuild/validation work.

The forward engine-swap migration preserves retained raw rows and existing rollup
totals. It detaches/recreates MVs around copying the source, so migration does not
replay contributions or leave MVs attached to the old table. Both migration flavors
replay successfully. The local upgrade, downgrade, and re-upgrade were also tested
with `product_metrics_migration_seed.sql` and `product_metrics_migration_verify.sql`:
retained observations and totals survive, and newly inserted observations reach
the reattached views. Downgrade restores the original engine/TTL but cannot restore
raw delivery copies already removed by replacement merges.

## Deployment prerequisites

Infrastructure owns the database, application credentials, and roles. The existing
application writer needs INSERT on `product_metric_contributions`, SELECT on the
two rollups, and privileges needed by the invoker materialized views to write the
targets. Use existing database-scoped application grants. No marts view is added,
so no marts-reader or definer grant is broadened. Apply topology and schema before
activating the subscriber. Local replay does not establish Cloud compatibility.

Pause contribution writers throughout the engine-swap migration and any rollback.
Keep the default-off subscriber disabled for initial rollout. The migration is
forward-only in the Atlas history because the original draft migration was already
published. No existing migration artifact is rewritten.

Design references: ClickHouse incremental materialized views, primary keys ordered
by scoped filters, lifecycle-driven partitions, batch inserts, explicit synchronous
insert completion, and serving without `FINAL`. The optional external ClickHouse
best-practices/advisor skills were unavailable in this environment; repository
ClickHouse conventions and executable measurements govern the implementation.
