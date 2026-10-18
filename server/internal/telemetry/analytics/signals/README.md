# Canonical signal measurements

The shared analytics engine compiles a sensor, explicit match rule, trigger/run,
source-time window and arbitrary label selectors into key-qualified `FINAL`
reads. Customer values and label keys are bound parameters. Counts describe
original captured messages, not tasks, time spent or independent defects.

## Measurement contract

- Multi-label: selected signal probability meets the explicit threshold.
- Exclusive: the selected signal matches; its reported probability is not a second gate.
- Ordered: cumulative probability at or above a declared zero-based level meets
  the threshold. The probability-weighted expected index is not used.
- Rates divide by answered readings that actually measured the selected signal
  or level. `unmeasured`, failed, outside-scope and draft outcomes remain visible.
- Distinct conversations are exact over each bucket, each selected group and
  the complete filtered window. Cross-group or cross-bucket distinct counts are
  not summed. Whole-window totals include groups outside the top selection.
- Missing attributes differ from present empty strings. Unknown time buckets
  contain null matches/rates, while answered negative buckets contain zeroes.
- Original-message measurements require explicit original capture provenance.
  Unknown lineage, including readings predating that capture, remains outside
  this measured population.
- Output intervals are UTC minutes, hours, days, Monday weeks and calendar months.
  First/last partial buckets are marked. `auto` budgets complete series across
  selected groups; explicit overly fine intervals are refused.
- Definition hashes and classifier/input metadata accompany the measures.
  Probability distributions are exact counts in fixed tenth-wide bins, with
  probability one in the final bin; they are not prevalence estimates.
- Live query time and latest observed insertion time are diagnostic freshness
  facts. Neither is a replayable historical snapshot or a pipeline watermark.
  Census-based coverage remains unknown.

## Physical read contract

Scope predicates are inside the canonical source: organization, project,
trigger, run, sensor and nanosecond source-time bounds. Request-time logical
expiry follows the table's two-calendar-year TTL. Manual reads discover and
validate their single request timestamp before applying an exact partition
predicate. They refuse inconsistent run timestamps rather than silently dropping
partitions.

Mutable label/outcome predicates run after replacement. Explicit settings prevent
PREWHERE movement and enable partition-independent FINAL under the immutable
request-time invariant. The ordinary path reads no message content. It reads
probability subcolumns rather than signal display slugs.

The planner reports `physical_resolution: canonical` separately from the output
interval: monthly output does not imply hourly/monthly storage. Each physical
read is bounded to five seconds, 25 million rows, four billion read bytes and one
billion memory bytes. Overflow is an actionable refusal, never a partial chart.
These are initial engineering budgets, not a production latency SLO.

Applied rules: `schema-pk-filter-on-orderby`, `schema-pk-prioritize-filters`,
`query-index-skipping-indices`, `agent-query-safety`. The source-time minmax-index
experiment is **derived** from immutable event time and measured sparse-index
boundary amplification, not a replacement for primary-key scoping.

## Reproducible local benchmark

`mise run sigint:benchmark` owns a disposable ClickHouse **26.6.8.7**, four query
threads, synthetic source-time history, duplicated deliveries, answer-to-failure
corrections and historical imports. It runs group selection plus chart-series
reads and checks both against a latest-attempt generator oracle. `--days`,
`--readings-per-day`, `--sensors`, `--iterations` control workload. `--event-time-index`
adds and materializes a minmax index only in that disposable database.

Reports contain engine version, part counts, exact-oracle checks, index plans,
physical rows/bytes, peak memory and cold/warm latency. Cold means ClickHouse
mark/uncompressed/query caches were cleared; it does not claim a cold OS page
cache. Small empirical percentile samples are diagnostic, not a statistical SLO.

Reference local run (2026-10-18, 730 days, three sensors, five department labels):

| Logical readings | Index              | Window / output   | Warm p95, full chart | Physical rows, both reads |
| ---------------- | ------------------ | ----------------- | -------------------- | ------------------------- |
| 2.19 million     | none               | 6 hours / minute  | 339 ms               | 756,688                   |
| 2.19 million     | source-time minmax | 6 hours / minute  | 63 ms                | 32,768                    |
| 2.19 million     | source-time minmax | 24 months / month | 305 ms               | 1,833,070                 |
| 21.9 million     | source-time minmax | 6 hours / minute  | 68 ms                | 32,768                    |
| 21.9 million     | source-time minmax | 90 days / day     | 311 ms               | 1,912,622                 |
| 21.9 million     | source-time minmax | 24 months / month | 1,914 ms             | 15,217,208                |

All reported cases passed the oracle. The minmax experiment removes old
source-time boundary granules that the sparse primary index must otherwise read
in each request-time part. Long-window work remains proportional to canonical
readings; the largest two-year case identifies the remaining acceleration
opportunity. Production workload sizes and latency must be measured on the
deployment before promoting these reference budgets to commitments.
