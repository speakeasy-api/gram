# Product metrics encoding evidence

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

| Query | Tuples | JSON | Tuple memory | JSON memory |
| --- | ---: | ---: | ---: | ---: |
| 24h equality filter | 13 ms | 32 ms | 23.6 MB | 30.7 MB |
| 30d equality filter | 117 ms | 514 ms | 25.1 MB | 31.0 MB |

24h scanned 57,344 rows / 32.1 MB for tuples and 49,152 / 21.6 MB for JSON.
30d scanned 909,888 rows / 599.8 MB for tuples and 909,888 / 478.0 MB for JSON.
On-disk size: tuples 8.1 MB (3 parts), JSON 65.7 MB (2 parts). Merge timing explains
the part-count/granule differences. `EXPLAIN indexes=1` pruned to 7/123 granules
for the tuple 24h query, using organization, project, metric and time. Both
representations returned identical totals (360 and 10,000).

An initial unrestricted bulk load exceeded the worktree's 2 GiB container limit.
Bounded blocks and threads completed successfully. This supports bounding insert
and query resources explicitly, rather than relying on server defaults.

Decision: native typed tuples, with full canonical identity in the merge key.
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

## Migration and aggregate validation

Atlas `migrate validate --env clickhouse` replayed the Atlas history against its
configured sandbox. The repository's `clickhouse:migrate --engine golang-migrate`
task replayed all local migrations against a fresh database provisioned with the
standard local principals. Both completed successfully.

`product_metrics_rollups.sql` passed against the migrated database. It checks
duplicate inserts across blocks, exact sums beyond int64, typed attributes and
tenant/project/resource/scope separation, weighted histogram means across minute
buckets, and identity preservation after merges. Serving assertions use explicit
aggregates without `FINAL`. The fixture's `OPTIMIZE ... FINAL` is only a test
operation to force background-merge-equivalent compaction.

## Deployment prerequisites

Infrastructure owns the database, application credentials, and roles. The existing
application writer needs INSERT on `product_metric_contributions`, SELECT on the
two rollups, and privileges needed by the invoker materialized views to write the
targets. Use existing database-scoped application grants. No marts view is added,
so no marts-reader or definer grant is broadened. Apply topology and schema before
activating the subscriber. Local replay does not establish Cloud compatibility.

Design references: ClickHouse incremental materialized views, primary keys ordered
by scoped filters, lifecycle-driven partitions, batch inserts, explicit synchronous
insert completion, and serving without `FINAL`. The optional external ClickHouse
best-practices/advisor skills were unavailable in this environment; repository
ClickHouse conventions and executable measurements govern the implementation.
