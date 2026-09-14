# Billing usage from meter readings

## Approved decisions

- Source: `billing_meter_readings_by_time` only. Leave deprecated `billing_meter_readings` and existing invoice, allowance, PAYG, and finalization paths alone.
- **Checkpoint:** `47cea857b8` preserves the rejected full-retention rebuild implementation and experiment harness; `3772f08d0f` contains its earlier implementation. Neither checkpoint establishes the release performance gate.
- **Incremental cutover:** insert-triggered materialized aggregation into daily `SummingMergeTree` series. No refreshable view, retained-history rebuild, Temporal schedule, staging/attempt tables, coverage markers, raw reporting scans, or writer fallback.
- **Duplicates are accepted reporting input.** A physical redelivery may contribute again even if raw `ReplacingMergeTree` later collapses it. Producer deduplication or future explicit out-of-band repair owns this discrepancy. Queries must not pretend that background source merges retract MV contributions.
- **UTC-day precision, explicitly selected by the user.** API ranges are half-open UTC-midnight boundaries; reject subday requests. Calendar selection and chart drilldown select whole UTC days. Today's bucket updates as inserts become visible. Maximum range remains three calendar months, clamped at month end.
- **One migration:** every schema revision deletes this branch's generated ClickHouse migrations and regenerates one Atlas migration plus matching local up/down files against the existing `main` baseline. Never delete or rewrite production migration history. Do not apply consolidated migrations blindly over an already-migrated local experiment.
- Historical incompleteness is accepted. No automatic production backfill, legacy fallback, or invented history. Existing raw data stays available for a separately controlled backfill or repair.

## Workload and resource model

- Target tens of millions of readings per day or more. Retained raw volume must not determine recurring aggregation work.
- An incremental MV processes only each incoming block. Memory is bounded by batch size and fixed facet expansion, not constant regardless of batch size. Existing batching/async insertion remains the ingestion mechanism; no per-reading jobs.
- Store independent facet marginals, never the Cartesian product of facet combinations. There are 13 storage, 5 bandwidth, and 7 risk selections including each family's total.
- Reporting I/O scales with active series/day cells in the selected facet. Exact period top six still must consider every active identity; bounded memory does not imply constant latency.
- Period ranking streams identities in storage order instead of retaining a hash entry per identity. Cap in-order aggregation blocks and top-N remerge buffers at 1 MiB, with two query threads/read streams. The second aggregation has at most seven identities × 93 days; Go receives only those bounded daily cells.
- Query/read limits and external-spill thresholds are safety rails, not the mechanism that makes aggregation memory bounded. ClickHouse read buffers, part metadata, and allocator peaks still vary; do not claim identical whole-query RSS across datasets.

## Schema

- `billing_meter_daily_summaries`: organization, family, reading kind, facet, UTC day, series kind/key, label, unit, measurement method, `quantity Int128`, `reading_count Int64`.
- `SummingMergeTree((quantity, reading_count))`; explicitly sum only these metrics. Signed counts leave room for controlled compensating repair later. Positive activity counts preserve net-zero adjustment groups.
- Monthly partitions; primary key `(organization_id, family, reading_kind, facet, series_kind, series_key, day)`; sorting key extends it with measurement metadata and label. Series identity precedes day deliberately: equality predicates fix the first four columns, allowing period aggregation to stream the following series prefix. Monthly partition pruning still bounds the time interval. Labels in the sorting key avoid arbitrary label selection during summation; readers choose a deterministic label across parts.
- One insert-triggered MV preserves the existing canonical family/facet normalization, expands incoming readings into independent marginals, then groups the incoming block. No `FINAL`, joins to mutable directory data, or full-table source scans.
- Read with explicit `sum()` and grouping before and after background merges. No `OPTIMIZE FINAL` prerequisite.
- Raw retention and dedicated aggregate tables preserve a future repair path. Source UPDATE/DELETE/merges do not propagate to an incremental MV. A repair must explicitly coordinate aggregate replacement or compensating deltas; no placeholder repair job ships.

## Request and response

- `usage.getMeterUsage` remains an additive organization-scoped GET method under `server/design/usage/design.go`, implemented in `server/internal/usage/meter_usage.go`. Organization comes from auth; existing `org:read` enforcement remains.
- Dedicated ClickHouse read-replica connection only. Missing configuration or reader failure must not fall back to the writer. Infrastructure owns reader endpoints and SELECT privileges.
- Filter daily summaries by organization, family, fixed `reading_kind = 'usage'`, facet, and `[from day, to day)`. No public reading-kind selector or response field.
- Rank six identities over the whole period by descending quantity with deterministic identity tie-breaks.
- Group the selected facet's daily quantities into these six identities plus remainder. Compute the response's total and daily buckets from the returned series; do not subtract totals fetched from a separately changing source. Detect incompatible units/methods across all input groups, not just winners.
- Keep grouping state and response bounded; no full day/value expansion transferred to Go or the browser. Retain all stored series, not daily top six.
- Distinguish `value`, `unset`, and `remainder` identities. Directory group sets remain sorted intact sets. MCP identity uses type plus ID, with slug only as a label.
- Project-facet chart and table labels use available project slugs from accessible project metadata, falling back to IDs. This is display-only: API series keys, grouping, attribution, and quantities remain unchanged.
- Exact signed decimal strings on the wire; BigInt for client aggregation; Number only for chart coordinates. Dense UTC daily buckets, aligned series arrays, exact weekly/monthly/cumulative totals. No partial-day clipping.
- Display token quantities in decimal KTok/MTok/BTok and bytes in binary KiB/MiB/GiB, rounded to at most one decimal. Keep full quantities in hover details. Compact scaling and elapsed-day rates use integer arithmetic; promote rounded values across unit boundaries.
- Every series sum equals its total; every bucket equals the sum of its series values; both reconcile to period total.
- Reports are eventually consistent. `queried_at` is retrieval time, not an ingestion watermark or read-your-write promise.

## Meter semantics and product boundary

| Family     | Meter selection              | Unit   | Default   |
| ---------- | ---------------------------- | ------ | --------- |
| Storage    | `gram.agent_session.storage` | tokens | Total     |
| Bandwidth  | MCP ingress and egress       | bytes  | Direction |
| Risk scans | All six registered scanners  | tokens | Scanner   |

- Storage is stored-message workload, not provider inference consumption. Bandwidth measures application-visible body bytes. Risk measures content scanned, not detections or unique content; multiple scanners legitimately count separately.
- Report ordinary usage only; adjustments are not planned for the dashboard or API. Existing internal correction records and schema remain intact but excluded from reporting. Never reconcile usage to invoice estimates.
- Frozen producer attribution remains the upstream contract. No current-directory enrichment or actor-to-billing-user inference in reporting.
- Existing independent billing-position/admin-estimator UI remains intact. Preserve all three family views, legends, cumulative mode, and exact tooltips.
- Demo reseeding deletes only its organization's raw and aggregate rows before reinsertion; the MV populates summaries naturally. No global rebuild or summary-publication exception in tenant-safety checks.

## Steps

- [x] Rebased `meter-facets` onto `origin/main`; retained upstream ledger/writer changes.
- [x] Added explicit reader configuration/pool and repository injection; no writer fallback.
- [x] Replaced the checkpointed rebuild with incremental UTC-day SummingMergeTree reporting and one consolidated migration. Verified deliveries, late/unmerged increments, tenant safety, exact period ranking, and write overhead. The five-reader 20,000-user gates pass under the one-GiB server budget; the larger-cardinality latency limitation and separate raw-ingestion evidence are recorded below.
- [x] Added the Goa method, embedded cycle windows, family/facet catalog, generated SDK and response contract; descriptions and validation enforce UTC-day precision.
- [x] Separated billing position/estimates from the storage explorer.
- [x] Added bandwidth/scanner views, exact aggregates, and chart controls; calendar selection and chart drilldown select UTC days.
- [x] Seeded all nine meters, verified local rendering and tenant safety, and completed two incremental reseeds.

## Verification

1. Real ClickHouse: inserts immediately produce every appropriate marginal; duplicate deliveries contribute as documented; late arrivals update old days without any rebuild; correction readings do not affect ordinary usage. Check before merges, without forcing compaction.
2. Exercise tenant isolation, every family/facet, incompatible metadata outside winners, period winners absent from every daily top six, deterministic labels, and exact conservation.
3. API: paired UTC-midnight boundaries, timezone-equivalent midnight, rejected subday requests, leap dates/month-end maximum, dense zeros, quantities above JavaScript's safe-integer limit.
4. Browser: date picker, day-snapped chart drilldown, all three families, no adjustment selector, legends/cumulative mode, and unchanged billing position. No changes to shared Costs semantics.
5. Replay the single generated migration on a clean database, run focused server tests and demo safety, regenerate Goa/SDK, run server lint and dashboard checks once after integration.
6. Benchmark ingestion with matched batch sizes and bounded memory; compare empty versus retained aggregate state. Separately benchmark representative full-window series cardinality with five readers and capture query-log accounting and EXPLAIN pruning. Report what was actually loaded, not an assumed traffic forecast.

## Measured replacement evidence

- Disposable ClickHouse 26.2, existing 1 GiB server-memory limit, native SELECT-only reporting principal. Replayed the single generated migration on a clean database. No history backfill or forced summary compaction.
- Reporting fixture: synthetic daily-summary cells, not a claim that the corresponding raw traffic was replayed. Five concurrent readers, 25 requests per scenario, exact expected totals in every response:

| Billing-user cardinality | Window  | Summary cells | p95       | Max query memory | Max rows read | Max bytes read |
| ------------------------ | ------- | ------------- | --------- | ---------------- | ------------- | -------------- |
| 20,000                   | 31 days | 620,000       | 162 ms    | 4,411,511 B      | 1,245,184     | 92,989,412     |
| 20,000                   | 92 days | 1,840,000     | 1,313 ms  | 17,447,000 B     | 3,702,784     | 275,901,740    |
| 200,000                  | 92 days | 18,400,000    | 10,549 ms | 43,381,945 B     | 36,826,368    | 2,841,674,264  |

- The specified 20,000-user cycle/quarter latency gates pass. The 200,000-user probe does **not** meet the two-second quarter gate; exact ranking still scans all active identities. None of these read queries spilled.
- Actual reader `EXPLAIN PIPELINE` shows `AggregatingInOrderTransform × 2` and `FinishAggregatingInOrderTransform` for period ranking. The remaining hash aggregation occurs only after classification into six winners plus remainder. Query limits alone would not establish this property.
- Separate ingestion A/B/A/B: one million raw rows per mode, 20,000-row batches, 250 ms pacing, all nine meters. Baseline median/p95 99/360 ms; incremental 128/266 ms. Active-call throughput 140,745 versus 126,320 rows/s; peak query memory 169,366,338 versus 168,013,225 B. Every incremental insert read at most 40,000 rows despite retained summaries, consistent with processing the incoming block twice rather than scanning history. Total-facet quantities and physical counts exactly reconcile to the one million inserted raw rows.
- An earlier unpaced burst exceeded the one-GiB server limit during background work. Bounded MV input state does not remove the need for bounded ingestion concurrency/backpressure; the paced result is not evidence for arbitrary burst capacity.
- The following HTTP and browser evidence predates removal of adjustment reporting; current behavior is usage-only.
- Authenticated local HTTP smoke: all 25 family/facet combinations for both reading kinds conserve exact series, daily, and period totals across 30 dense UTC days. Subday request returns 400; timezone-equivalent UTC midnight returns 200 with the same total.
- Incremental demo safety passed 14 tests, including neighboring-tenant checks; two successive local reseeds completed. All 11 focused server tests passed; the strengthened late-redelivery/unmerged-parts regression passed separately. Goa/SDK generation, server lint, and dashboard type-check passed. Server lint reports the existing `exhaustruct` deprecation warning.
- Browser verification covered storage, bandwidth, signed risk adjustments, and cumulative mode. In an America/Los_Angeles browser, selecting September 3–5 emitted `[September 3 00:00Z, September 6 00:00Z)`; a real chart drag emitted `[September 10 00:00Z, September 15 00:00Z)` and returned 200. An authenticated request against the restarted server also returned 200.
- Removed throwaway reporting/ingestion commands; stopped the disposable benchmark server and browsers. The obsolete local rebuild schedule was deleted; the worker was restored on the incremental implementation.
- Query logs, pipelines, insert accounting, and HTTP matrix are ignored local artifacts under `.playwright-cli/meter-incremental/`; no synthetic throughput figure represents measured production traffic.

## Usage-only cutover verification

- Removed public reading-kind selection and adjustment presentation; generated Goa and SDK contracts contain no reading-kind field. Internal correction storage and schema are unchanged.
- All 10 focused server tests and 14 demo-safety tests passed. Correction-exclusion coverage includes positive and negative corrections and incompatible correction metadata. Two local reseeds passed with 864 unique ordinary readings across nine meters and 873 delivered summary counts, with no correction fixtures.
- Against the restarted local API, all 25 family/facet combinations returned 200 without `reading_kind` and conserved exact series, daily, and period totals across 30 UTC days.
- Browser screenshots verified storage, bandwidth, and risk-scan views with ordinary usage labels, rendered charts, and no adjustment selector. The generated browser request omits `reading_kind`. Storage/risk display tokens; bandwidth displays bytes.
- Dashboard type-check, `hk fix`, and server lint passed; lint reports only the existing `exhaustruct` deprecation warning. Closed the verification browser and removed its temporary authenticated profile.
- Compact-unit verification exercised 13 quantity boundary/rounding cases, exact values beyond JavaScript's safe-integer range, fractional-day rates, and zero-duration handling. Live browser checks covered storage, bandwidth, risk scans, cumulative chart axes, and full-value hover details. Dashboard type-check passed.

## Rollout and unresolved dependencies

- Product choices above are resolved. Producer dedup/frozen-attribution enforcement and optional raw-history backfill/repair are separate work, not hidden automatic behavior.
- Provision the existing reader configuration and SELECT permissions for the new serving surface before rollout. Local ClickHouse is not proof of ClickHouse Cloud or production replica latency.
- Existing experimental local migration revisions require deliberate reconciliation or a clean verification database after consolidation; do not reset unrelated data.
- Full-retention rebuild acceptance was never completed. Earlier evidence remains in checkpoint history and ignored `.playwright-cli/meter-*` artifacts; none of its latency or replica-publication claims certify the replacement.
- Relevant skills: `clickhouse`, `clickhouse-best-practices`, `clickhouse-architecture-advisor`, `golang`, `gram-management-api`, `frontend`, `vercel-react-best-practices`, `page-toolbar`, `gram-demo-seed`, `gram-temporal` for removal, `pitchfork`, `gram-playwright-cli`.

## Sources

- Per `query-mv-incremental`: [incremental materialized views](https://clickhouse.com/docs/materialized-view/incremental-materialized-view) process inserted blocks, not existing data or later source mutations.
- [SummingMergeTree](https://clickhouse.com/docs/engines/table-engines/mergetree-family/summingmergetree): explicitly sum during reads because background merging is incomplete; non-key, non-summed values are arbitrary.
- Per `schema-pk-filter-on-orderby`: organization/family/kind/facet equality predicates align with the primary-key prefix; series-before-day ordering enables [in-order aggregation](https://clickhouse.com/docs/sql-reference/statements/select/group-by#group-by-optimization-depending-on-table-sorting-key), rather than a hash table proportional to facet cardinality.
- Per `insert-batch-size`: batch inserts amortize part creation; measure the actual producer shape rather than assuming per-row inserts are inexpensive.
