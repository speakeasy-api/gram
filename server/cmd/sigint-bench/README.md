# Signal query validation

Run `mise run sigint:benchmark` from the repository root. It owns a disposable
ClickHouse 26.6.8.7 container and uses only synthetic observations. The fixture
contains duplicate deliveries, answer-to-failure corrections and historical
imports with immutable request-time cohorts. Every completed measurement is
checked against a generator-derived latest-attempt oracle.

## Workload controls

| Flag                 | Meaning                                           |
| -------------------- | ------------------------------------------------- |
| `--readings-per-day` | 1–100,000 logical readings per sensor/day         |
| `--days`             | 1–730 days of source history                      |
| `--sensors`          | 1–50 independently keyed sensors                  |
| `--labels`           | 1–1,000 distinct department values                |
| `--session-size`     | 1–10,000 original messages per conversation       |
| `--concurrency`      | 1–16 concurrent chart readers                     |
| `--iterations`       | 1–30 warm samples per case                        |
| `--event-time-index` | Materialize an immutable source-time minmax index |
| `--out`              | JSON report path outside the repository           |

The baseline explicitly drops the shipped time index. Indexed cases add the
experimental equivalent only inside the disposable database.

## Cases and interpretation

- Full charts: 6 hours/minute, 7 days/hour, 90 days/day, and the entire generated
  history/calendar month. Top groups are selected over the whole window. Every
  nonempty selected bucket is checked; failures remain in outcome companions.
- Session findings: exact distinct conversations, concentration, separately
  checked message/session co-occurrence and previous-window counts.
- Evidence: bounded recent candidates with exact matching/source coordinates.
  This measures the ClickHouse lookup, excluding PostgreSQL authorization and HTTP.
- Arbitrary attributes: an inclusion filter on a captured `custom.cohort` key.
- Manual cohort: an independently identified run with one immutable request time;
  index analysis shows the additional request-time partition predicate.
- Concurrent charts: warm readers overlap sixteen batches of duplicate deliveries
  and, separately, newly arriving observations beyond the pinned source-time cutoff.
  The oracle verifies that redelivery and new arrivals cannot change that answer.

Reports record engine version, fixture dimensions, initial active parts, empirical
cold/warm p50/p95, rows/bytes, selected marks and peak per-query memory. `EXPLAIN
ESTIMATE` and index analysis describe the representative physical read; actual
counters sum every read in the measured chart/finding. Concurrency counters describe
one completed request, not the entire concurrent cohort or server RSS.
Reports are saved after each chart case with `complete: false`, then marked
complete only after the remaining cases pass. Metadata-only explanations disable
scan caps so they can describe a refused plan; actual data reads retain every cap.

Four query threads are pinned. Query and query-condition caches are disabled;
index diagnostics also disable streaming skip-index evaluation. “Cold” clears
ClickHouse read caches, not the operating-system page cache. Five-sample percentiles
are local diagnostics, not production SLO estimates. Concurrent cases have no cold
measurement. Active background merges can change part/granule counts between runs.

Interactive reads retain the production compiler's five-second, 25-million-row,
four-billion-byte and one-billion-memory-byte limits. Refusals are reported rather
than mislabeled as partial charts. Coarser output does not reduce canonical work.
Large retained histories that exceed those limits need measured acceleration before
being offered as an interactive workload.

Rules applied: `schema-pk-filter-on-orderby`, `query-index-skipping-indices`,
`query-join-filter-before`, `agent-query-safety`, `insert-batch-size` and
`insert-optimize-avoid-final`. The benchmark never forces `OPTIMIZE FINAL`.

## Measured envelope — 2026-10-18

All cases below use 730 days, the source-time index, four query threads and five
warm samples. These cover the workload axes rather than their full Cartesian
product. Initial active parts were 47, 109 and 165 in the 2.19M, 73M and 219M
runs; merging explains why physical amplification is not monotonic with size.

| Logical readings | Per sensor/day | Sensors | Labels | Messages/session | Concurrent readers |
| ---------------- | -------------- | ------- | ------ | ---------------- | ------------------ |
| 2.19M            | 1,000          | 3       | 5      | 100              | 4                  |
| 36.5M            | 1,000          | 50      | 1,000  | 100              | 4                  |
| 73M              | 10,000         | 10      | 50     | 1,000            | 4                  |
| 219M             | 100,000        | 3       | 1,000  | 10,000           | 8                  |

Warm empirical p95, milliseconds:

| Logical readings | 6h / minute chart | 7d / hour chart | 90d / day chart | 24mo / month chart      | 7d session findings | Concurrent + duplicates | Concurrent + new arrivals |
| ---------------- | ----------------- | --------------- | --------------- | ----------------------- | ------------------- | ----------------------- | ------------------------- |
| 2.19M            | 47.6              | 49.3            | 75.5            | 339.9                   | 129.2               | 51.3                    | —                         |
| 36.5M            | 67.2              | 74.2            | 135.3           | 724.1                   | 210.4               | 81.1                    | 71.2                      |
| 73M              | 56.6              | 218.4           | 327.1           | 2,054.1                 | 225.5               | 68.4                    | 60.9                      |
| 219M             | 88.8              | 360.2           | 2,681.6         | **Scan-budget refusal** | 952.6               | 210.6                   | 220.8                     |

Every returned measurement passed its latest-attempt oracle. The 219M full-history
case selects about 73M logical readings from one sensor and refuses the
25-million-row read budget; it returns no partial chart. This is the demonstrated
boundary for the canonical-only source, not a supported two-year interactive
workload at that ingestion rate. Correction-aware acceleration remains follow-on
work, with independent threshold/attribute compatibility and maintenance costs.

Representative physical work (counts sum the reads in one request):

| Case              | Cold / warm p50 / p95 ms    | Read rows  | Read bytes    | Selected marks | Peak query memory MiB |
| ----------------- | --------------------------- | ---------- | ------------- | -------------- | --------------------- |
| 2.19M, 6h chart   | 65.0 / 38.5 / 47.6          | 32,768     | 11,631,298    | 4              | 17.5                  |
| 73M, 24mo chart   | 2,299.8 / 2,023.5 / 2,054.1 | 16,083,978 | 5,631,693,960 | 2,830          | 509.3                 |
| 219M, 6h chart    | 113.2 / 70.9 / 88.8         | 114,688    | 41,493,984    | 10             | 34.3                  |
| 219M, 90d chart   | 2,479.4 / 2,420.8 / 2,681.6 | 18,510,556 | 6,694,791,260 | 2,400          | 374.8                 |
| 219M, 7d findings | 718.8 / 734.0 / 952.6       | 6,465,591  | 1,319,982,169 | 786            | 172.8                 |
| 219M, manual 6h   | 55.9 / 56.2 / 59.7          | 75,000     | 13,886,304    | 12             | 9.2                   |

For the 219M six-hour case, index analysis narrows 28,421 marks to 168 with the
primary key, then five marks in two parts with the time index. The representative
read estimates 40,960 rows; actual work over both reads is 114,688 rows and ten
selected marks. Estimates are planning diagnostics, not substitutes for actual
`FINAL` read counters. The manual timestamp reduces eligible parts from 166 to
11, and its run/sensor prefix selects one part with 25,000 rows. Its three reads
including request-time validation total 75,000 rows.

Reproduce the broader and stress runs:

```sh
mise run sigint:benchmark --days 730 --readings-per-day 1000 --sensors 50 --labels 1000 --session-size 100 --concurrency 4 --iterations 5 --event-time-index --out /tmp/opencode/sigint-wide.json
mise run sigint:benchmark --days 730 --readings-per-day 10000 --sensors 10 --labels 50 --session-size 1000 --concurrency 4 --iterations 5 --event-time-index --out /tmp/opencode/sigint-dense.json
mise run sigint:benchmark --days 730 --readings-per-day 100000 --sensors 3 --labels 1000 --session-size 10000 --concurrency 8 --iterations 5 --event-time-index --out /tmp/opencode/sigint-stress.json
```
