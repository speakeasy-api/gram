# Billing usage from meter readings

## Context

- Production now writes `billing_meter_readings_by_time` (user-confirmed). It is the sole source for the new usage explorer. `billing_meter_readings` is deprecated and will be removed separately: no reads, unions, fallback, new seed rows, or removal of that table in this change.
- TUM is exactly `MeterAgentSessionStorage` (`stokens`); MCP ingress/egress are bytes; all six risk scanners meter scanned content in `stokens`.
- All nine usage meters follow the first-accepted immutable-fact contract: quantity, occurrence time and reporting attribution are frozen; adjustments are separate facts. The new table still uses unversioned `ReplacingMergeTree` to converge duplicate deliveries, so readers must use `FINAL` before aggregation.
- The fact-table/writer cutover already landed. The current PR implements the explorer with bounded raw `FINAL` reads and promoted attributes; it does not yet implement daily summaries. Step 3 below replaces that historical request path rather than tuning it further.

## Approach

- One meter-backed usage explorer for TUM, MCP bandwidth, and risk content scans; keep units separate.
- Serve fully included, published UTC days from daily summaries. Use bounded organization/meter/time `billing_meter_readings_by_time FINAL` reads only for unsettled days and partial historical boundary days, with `do_not_merge_across_partitions_select_final = 1`. No telemetry-derived meter figures or deprecated-table dependency.
- Preserve cycle/custom-period selection and daily/weekly/monthly/cumulative chart controls.
- Confirmed scope: **usage visualization only**. Preserve invoice estimates, pricing, allowance/overage calculations and finalized records in a separate, clearly labeled billing section. Never normalize meter totals to legacy billed totals.
- API constraint confirmed: add a **new Goa method under `server/design/usage/design.go`**. Do not replace/remove existing methods. Migrate only the dashboard usage explorer to it.
- All new meter-reporting reads **must use the ClickHouse read replica**, never the existing writer connection.

### ClickHouse decision

- Deployed layout: `ENGINE = ReplacingMergeTree` (no version argument), `PARTITION BY toYYYYMM(occurred_at)`, `PRIMARY KEY (organization_id, meter_id, occurred_at)`, `ORDER BY (organization_id, meter_id, occurred_at, project_id, id)`.
- Frozen occurrence time/full sorting key keep every retry in the same month and replacement group. Organization + exact meter IDs + `[from,to)` now support primary-key and partition pruning. Keep time predicates directly in the query; remove the old moved-timestamp/global-dedup query strategy.
- Use `FINAL` and `SETTINGS do_not_merge_across_partitions_select_final = 1` before aggregating raw facts, both in snapshot builds and live reads. Published daily summaries need neither raw-ledger deduplication nor `FINAL`. Unversioned replacement/background merges do not enforce first acceptance of conflicting payloads; accepted upstream representation remains the producer contract.
- User-approved performance follow-up: promote the 16 retained reporting attributes from the frozen Map into scalar materialized columns. Use `String` for unbounded identities/labels and `LowCardinality(String)` for categorical attributes. Keep the source Map and accepted facts unchanged. Atlas alone generates and manages migrations from `schema.sql`—never hand-edit generated migration files. Materialize existing parts as a separate operational action, not as hand-authored migration SQL.
- Daily-rollup design supersedes further full-range raw-query tuning; see below. Do not use an insert-triggered summing MV: identical physical retries reach it before replacement merges.
- Sources: [deployed schema](https://github.com/speakeasy-api/gram/blob/aac036405e2ed87edac0517117413004cb1781bf/server/clickhouse/schema.sql), [writer/read contract](https://github.com/speakeasy-api/gram/blob/aac036405e2ed87edac0517117413004cb1781bf/server/internal/metering/chrepo/queries.go), [ReplacingMergeTree / FINAL](https://clickhouse.com/docs/guides/replacing-merge-tree).

### Daily-summary read model

- **Native implementation verified locally; full-volume acceptance remains open.** Keep the raw ledger authoritative. `billing_meter_daily_summary_refresh` reads `FINAL`, builds independent daily facet summaries, and atomically replaces `billing_meter_daily_summaries` without Temporal. Benchmark this full-retained-history rebuild before choosing targeted day maintenance. Atlas manages serving/view schemas and generated migrations.
- Size correctly: for one selected facet, summary rows scale with the sum of its active value counts across included days, not just the number of days. At 90 days, direction has at most 180 cells, 20 departments have 1,800, and 20,000 daily-active users have 1.8 million. These are sizing examples, not performance measurements. The API remains limited to three calendar months, which can intersect 93 UTC dates, not exactly 90.
- Summary grain: `(organization, family, reading kind, facet, series kind, series key, UTC day, unit, measurement method)`. Store an exact `Int128` quantity, reading count, and deterministic label independent of identity. Keep incompatible measurement metadata detectable; preserve net-zero adjustment groups with activity.
- Aggregate each supported facet independently: department totals, user totals, model totals, etc. Never build the Cartesian product of facet combinations. This supports the current one-breakdown/no-cross-facet-filter API; arbitrary combined filters would require a new grain/design.
- Retain every observed facet value, not daily top six. Store sparse nonempty day/value cells; zero-fill only the bounded response. Also store family/reading-kind/day totals, including reading counts, for direct totals and remainder calculation. Do not sum across different facet marginals: each separately accounts for the same family facts.
- Order summaries by organization, family, reading kind, facet, day, series kind, series key, and measurement metadata. A native full refresh can partition by month; targeted day replacement needs matching day-partitioned staging/serving tables. Publish coverage metadata atomically with the quantities, including empty coverage. A query-window cap is not a retention TTL: retain summaries for older selectable billing periods.

#### Request algorithm

1. Resolve the replica-visible published boundary. Split `[from,to)` into complete published days and disjoint raw fragments for unsettled days or partial historical boundaries. Normal operation reads historical summaries plus today's bounded raw aggregation.
2. Combine those sources logically and sum the selected facet by `(series kind, series key)` across the whole period, without grouping by day. Rank usage by signed period total descending, adjustments by absolute signed period total descending; break ties by stable identity. Select six values.
3. Construct daily series only for those six identities. Read daily family totals and derive `remainder[day] = total[day] - sum(selected[day])`. Use reading counts to detect omitted activity even when its signed quantity nets to zero; do not emit a remainder when nothing was omitted.
4. Build dense clipped UTC buckets and exact series/period totals from that same logical snapshot. Historical generations and live input must be consistent between ranking, totals, and series; do not independently refetch a changing live tail or assume a SQL CTE materializes it.

- Target intermediate aggregation state: one period accumulator per distinct facet value, then at most `days × 7` output cells. Remove the current day/value expansion followed by period and ranking windows. Server-side query operators may scan narrow summary rows more than once; do not transfer all day/value cells to the browser.
- A value outside each day's top six may still rank in the full period. The winner set must include the live contribution before selection, and remain fixed for all displayed days. Preserve exact conservation, unset identity, deterministic labels, and separate signed adjustments.

#### Publication, live tail, and repair

- First publish yesterday after 01:00 UTC for the expected one-hour retry window. Continue using `FINAL`: this allowance does not prove merges finished. Native refresh frequency controls historical repair staleness independently of the daily aggregation grain; choose it from measured rebuild cost and the required staleness bound. Yesterday stays live until successful publication, not merely until a clock boundary.
- Reader queries remain replica-only. Confirm required publication markers alongside summary results; missing coverage must never appear as valid zero or cause writer fallback. Failed builds leave the previous complete snapshot readable. Bound raw catch-up work explicitly rather than silently reverting an entire three-month request to the failed historical raw-query strategy.
- Native baseline: use non-`APPEND` refreshes over all retained reporting history. Each refresh recomputes the complete result and picks up late originals/adjustments visible to that build. No dirty-day queue or ingest-path orchestration is required. `APPEND` is not atomic day replacement and would require its own retry/generation design; a yesterday-only replacement view would discard older summaries.
- Native scheduling serializes refreshes of a given view; it does not accumulate catch-up executions for missed slots. Temporal actions/month: **0** for this approach. Measure refresh duration, peak memory, spill/storage, and interference with ingestion and readers; moving work off the request path does not eliminate its cost.
- Only if full rebuilding is too expensive: externally orchestrate complete dirty-day builds with atomic `REPLACE PARTITION`. Then durable pre-ACK dirty generations, builder visibility, concurrent-insert races, and serialized publication become prerequisites. Temporal is one scheduler option, not a requirement. Never introduce per-reading Temporal calls.
- Initialize derived summaries from facts already present in the new ledger. This is not legacy-history reconstruction or new raw readings. The demo seed writes only its scoped ledger facts, then requests the same native publisher with a bounded wait. The publisher owns the summary table exclusively: no seed-side summary inserts, deletes, or tenant publication protocol. Native refresh rebuilds derived reporting data globally without changing other tenants' ledger facts.
- References: [refreshable materialized views](https://clickhouse.com/docs/sql-reference/statements/create/view#refreshable-materialized-view) document native schedules, non-`APPEND` atomic replacement, and refresh coordination; [`REPLACE PARTITION`](https://clickhouse.com/docs/sql-reference/statements/alter/partition#replace-partition) documents the targeted alternative. Verify deployment/version/privilege/replication behavior separately; local success is not Cloud proof.

### Read-replica wiring

- The dedicated reader pool/configuration is implemented in `server/cmd/gram/flags_clickhouse.go`, `deps.go`, and `start.go`. Reuse it for summary and live reads; no new writer fallback.
- Add `clickhouse-read-*` / `CLICKHOUSE_READ_*` host, native port, database, username, password and TLS-verification configuration for the API command. Build a distinct traced connection pool and inject **only that pool** into the new meter read repository. Reuse the existing dial/ping/TLS/close machinery through explicit connection options; leave writer callers unchanged.
- Require explicit reader endpoint configuration for the API command; no silent writer fallback on missing config or replica failure. Reader configuration must not become mandatory for unrelated writer/migration commands. Clean up both clients on startup failure and shutdown.
- Add explicit local reader values to `mise.toml`, pointing at local ClickHouse and following the PostgreSQL read-replica convention. Production reader endpoint/credentials must be configured before deployment; a table cutover does not establish replica-client configuration.
- Use a SELECT-only reader principal on the fact table and published summary surface, provisioned through infrastructure ownership conventions, not application DDL. Keep connection errors and secrets out of responses/logs. Replica lag is eventual consistency: query time is retrieval time, not ingestion completeness; no read-your-write promise or fallback to primary. Keep staging/publication privileges away from the API reader.
- The native view currently declares `gram` as its definer. Provision and review its required database-scoped `SELECT`, `INSERT`, `CREATE TABLE`, and `DROP TABLE` permissions before deployment; the local bootstrap grant is not Cloud provisioning. These publication privileges must not be granted to the API reader.
- Benchmark on the read replica. Verify selected endpoint with distinct reader/writer configurations, permission-limited credentials and server query evidence. A single local ClickHouse instance proves routing/configuration, **not actual replica lag behavior**.

### Meter views and facets

| View                    | Meter selection / unit                           | Default stacks   | Facets                                                                                                                                               |
| ----------------------- | ------------------------------------------------ | ---------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| Tokens under management | `gram.agent_session.storage` / s-tokens          | Total            | Project, model, provider, billing mode, assistant; billing user ID, division, department, job title, employee type, cost center, directory group set |
| MCP bandwidth           | `gram.mcp.bandwidth.ingress` + `.egress` / bytes | Ingress / egress | Project, MCP server (type + ID identity, slug label), server type                                                                                    |
| Risk content scans      | Exact six `MeterRisk*` definitions / s-tokens    | Scanner          | Project, policy, judge model/provider, tool name                                                                                                     |

- Risk means **volume scanned**, not detection count or unique content: content scanned by several scanners legitimately contributes to each. No scan-count headline inferred from signed readings.
- Scanner series: Gitleaks, Presidio, prompt injection, prompt policy, custom rules, CLI destructive. Model/provider here identify the **scanner judge**, not the scanned agent; non-LLM scanners normally leave these unset.
- Bandwidth is application-visible body bytes, not headers/framing or upstream fanout. Its producer emits no user/directory facets; risk emits message-user identity but no billing-user enrichment. Storage has no emitted message-type dimension. Show only family-supported facets.
- TUM is canonical stored-message workload, not provider token consumption. Remove input/output/cache token-type stacks and old cache/inference-exclusion copy. Never add risk s-tokens to TUM just because the units match.
- Attribute absence is explicit `(unset)`; do not infer billing user from message actor or carry current telemetry identity into the ledger. High-cardinality message/request/conversation IDs remain provenance, not default chart facets.
- Billing-user grouping uses only the frozen user ID, including its display label; no email lookup or fallback. Billing-user roles and custom domain are not reporting dimensions. The raw provenance Map remains unchanged.
- One shared period across three segmented views, TUM selected initially. Each view has usage/rate, time series, and selected-facet cumulative table. Preserve daily/weekly/monthly (UTC Monday weeks), cumulative mode, legend toggles and click/drag time drill-down.
- Default to ordinary usage (`reading_kind = 'usage'`). Offer adjustments as a separately labeled reading-kind selection using the same explorer; never net them into usage or reconcile them onto originals. Adjustment values remain signed: allow negative buckets, decreasing cumulative series and net-zero periods containing activity.

### Additive API contract

- Add `usage.getMeterUsage` (GET) in `server/design/usage/design.go`, implementation `server/internal/usage/meter_usage.go`, repository `server/internal/metering/chrepo/usage.go`; inject the dedicated ClickHouse **read-replica** repository into `usage.Service`.
- Request: family (`agent_session_storage`, `mcp_bandwidth`, `risk_content_scans`), optional paired `from`/`to`, one allowlisted breakdown, and reading kind (`usage` default, `adjustment`). Omitted range resolves the active cycle; response includes 12 cycle **windows**, not legacy token figures. Reuse `BillingCycles` and billing metadata for windows only.
- Response: the explicit JSON contract below supplies headline, chart and table without a duplicate rows collection. Direction/scanner selections use exact meter IDs. No invoice price/allowance or historical-coverage fields.
- Resolve organization from auth and require `org:read`; query organization-owned ledger directly, retaining deleted projects. Do not accept arbitrary SQL/attribute keys. Validate paired timestamps, range order, maximum three-calendar-month window in UTC (clamped at month end, preserving time of day), and family-compatible dimension.
- Query `billing_meter_readings_by_time FINAL` with organization, meter IDs, occurrence bounds and reading kind, and set `do_not_merge_across_partitions_select_final = 1`. Deduplicate physical retry copies before summing. `produced_at` is provenance, not a replacement-version ordering rule for this table.
- Sum `value` separately per reading kind using each fact's own time/provenance. No join to original readings, no adjustments subtracted from ordinary usage, and no deprecated-source reconciliation. Preserve unit/method grouping; reject incompatible mixtures.
- Compute headline, daily buckets and selected-facet rows from the same deduplicated read/result. Rank facet values across the entire period (by volume for usage, absolute net volume for adjustments, deterministic tie-break), not per day; preserve remainder sign. Use integer/BigInt arithmetic until chart-coordinate conversion; exact tooltips/tables retain decimal strings.
- Billing-user directory groups are multi-valued: expose intact sorted sets as **Directory group set**, not exploded overlapping stacks. This preserves additive totals without inventing a cost allocation rule.
- Cache keys include org/family/window/breakdown/reading kind. Bounded staleness for every period: immutable facts can arrive late, including adjustments; closed calendar windows are not proof that ingestion is complete.

#### Response shape and chart contract

Proposed JSON wire shape; author in Goa and generate SDK types rather than hand-maintaining this TypeScript:

```ts
type MeterUsageResponse = {
  family: "agent_session_storage" | "mcp_bandwidth" | "risk_content_scans";
  reading_kind: "usage" | "adjustment";
  window: {
    from: string; // RFC 3339 UTC, inclusive
    to: string; // RFC 3339 UTC, exclusive
  };
  billing_cycles: Array<{ from: string; to: string }>; // Date windows only
  unit: "stokens" | "bytes";
  measurement_method: string;
  total: string; // Exact integer as decimal string; adjustments may be negative
  buckets: Array<{
    from: string;
    to: string;
    total: string;
  }>;
  breakdown: {
    dimension: string; // Goa enum from the family-compatible facet catalog
    series: Array<{
      kind: "value" | "unset" | "remainder";
      key: string | null; // Canonical identity; non-null only for kind="value"
      label: string; // Display text, never chart identity
      total: string;
      values: string[]; // Aligned one-for-one with buckets
    }>;
  };
  queried_at: string; // Retrieval timestamp, not an ingestion watermark
};
```

- Daily resolution is the chosen baseline; no hourly granularity parameter. Emit chronological, dense UTC day buckets including zero-usage days, clipping the first/last bucket to `[from,to)`. A three-calendar-month request intersects at most 93 UTC days. Intraday selections give an aggregate for the selected partial day, not invented hourly detail.
- Every series has exactly `buckets.length` values, in bucket order, with zeros where absent. Empty ranges retain zero-filled buckets and have no breakdown series. Do not omit days or independently timestamp each series.
- Select top six facets once across the full request; include a remainder only when values were omitted. This is a bounded explorer response, not a paginated enumeration/export of every facet. `unset` is a real missing-attribute group; `remainder` aggregates omitted groups. Identity is `(kind, key)`, so labels such as “Other” and “Not set” cannot collide with these markers.
- Invariants: for each series, `sum(values) = series.total`; for each bucket index, `sum(series.values[index]) = buckets[index].total`; `sum(buckets.total) = sum(series.total) = total`. Compute from the same deduplicated result.
- The frontend adapter maps aligned arrays to chart rows; stable identities become chart data keys, not raw labels. Series totals directly populate the breakdown table or horizontal bars. Bucket totals supply the total line. No separate API chart/table representations.
- Keep quantities as decimal strings on the wire and use BigInt for weekly/monthly rollups and cumulative sums. Convert only plotted coordinates to Number; tooltips/tables retain exact quantities. Chart precision loss at large magnitudes must not alter displayed exact values.
- Colors, legend visibility and cumulative mode are client state. Roll weeks at UTC Monday and months at UTC month boundaries. Use bucket bounds for tooltips and click/drag drill-down. Signed adjustments require diverging stacks and a zero-inclusive axis; cumulative adjustment series may decrease.

### History and billing separation

- **Accepted clean cutover:** the old usage method was flawed and the new metering deployed recently. Losing historical completeness is explicitly acceptable; it is not a blocker, reconciliation requirement, or recovery workstream.
- Show only facts present in the new ledger and summaries derived from those facts. No legacy-history backfill/fallback, synthetic historical TUM, import replay, or rescanning. Empty ranges say “No meter readings recorded.” Internal summary publication markers establish query readiness, not completeness of historical ingestion; no public coverage-status API.
- Keep `getTokensUnderManagement`, `telemetry.queryTumDetails`, their repository helpers/tests and PAYG/finalization paths intact. Preserve the existing contract-position card/admin estimator separately. A legacy-query failure must not prevent meter usage from rendering.
- New explorer uses date-only cycle metadata from the new API, never the old `BillingCycle.tokens/days`, `billedDaysFromCycles`, `resolvePeriodFigures`, or overage weights. Meter cumulative tables show usage, **not allocated invoice overage**.

### Upstream contract boundary

- Already landed: time-oriented table, `chrepo.InsertReadings` writing it, and `WriteCorrelated` emitting only when `stored.Inserted`. Do not duplicate that work.
- Remaining upstream caveat: at the inspected revision, `MeterReadingCHWriter.enrichBillingUsers` still recomputes directory fields for each batch, and batch dedup selects a later producer variant. First-accepted/frozen attribution is the agreed contract, but the table does not enforce it. Record this as a producer/consumer follow-through dependency, not as permission for the dashboard to reconstruct a different winner or refresh directory joins.
- This visualization change does not migrate producer acceptance/enrichment, backfill production, enforce physical exactly-once inserts, or remove the deprecated ledger. Existing `FINAL` reads handle same-key copies; conflicting occurrence keys are an upstream invariant violation, not a query fallback case.

## Files to modify

- `server/design/usage/design.go`; new `server/internal/usage/meter_usage.go`; `server/internal/usage/impl.go` and service-constructor callsites in `server/cmd/gram/start.go`/test setup: additive API and repository injection.
- `server/internal/metering/chrepo/usage.go` and adjacent repository code/tests: replace historical raw/window aggregation with period-first ranking over summaries plus bounded live fragments, using existing Squirrel/query/scan conventions. Native view SQL owns snapshot construction unless evaluation selects targeted maintenance.
- `client/dashboard/src/pages/billing/Billing.tsx` and billing `tum-section.tsx`, `tum-queries.ts`, `tum-token-breakdown.tsx`, `token-usage-panel.tsx`, `tum-details-table.tsx`, `tum-usage-card.tsx`, `breakdown-options.ts`, `use-billing-period.ts`: separate billing position from generic meter usage. Use LSP renames/references for TUM-only components migrated to meter-generic names; retain admin-used helpers.
- `client/dashboard/src/components/stacked-time-series-panel.tsx` only where required for signed/large quantities; preserve its Costs caller behavior.
- `server/internal/demoseed/clickhouse.sql`, `spec.go`, and seed runner integration as needed; `seed/demo/PAGES.md`: preserve all nine meters and duplicate/adjustment fixtures, and build tenant-safe daily summaries so seeded history exercises the real serving path. Retain legacy telemetry fixtures for unrelated billing paths.
- `server/clickhouse/schema.sql` and both Atlas-generated migration flavors: add the chosen serving table/refreshable view, or staging tables only if targeted maintenance is selected. Keep the consolidated 16-column migration intact; never hand-edit generated migrations. No writer table cutover, legacy-history reconstruction, or deprecated-table removal.
- `server/cmd/gram/flags_clickhouse.go`, `deps.go`, `start.go`, `mise.toml`, and `.mise-tasks/zero/remap-ports.mts`: dedicated reader flags/client/lifecycle and explicit local configuration; derived reader ports follow their primary port instead of being randomized separately. Production deployment settings must be supplied by their owner before rollout.
- `server/internal/metering/ch_writer.go` and background registration/activity code: unchanged for native refreshes. Touch these only if measured full-refresh cost justifies externally scheduled dirty-day repair, after resolving durable markers and source visibility. Use SQLc/Atlas conventions if PostgreSQL state is selected.

## Reuse

- Meter IDs/units: `server/internal/metering/definition.go`.
- Provenance catalog: `server/internal/metering/attributes.go`.
- `server/internal/usage/billing_cycle.go`: `BillingCycles`/UTC anchor logic; `tum.go` org-read authorization and billing metadata lookup, not its telemetry totals.
- `BillingCyclePicker`, `TimeRangePicker`, `useBillingPeriod` date selection, `StackedTimeSeriesPanel`, palette, inline failures, and project-name lookup. Separate date-only typing without changing existing invoice/admin helpers.

## Steps

- [x] Rebased `meter-facets` onto `origin/main` at `07ffd9645d`; retained upstream table/writer changes.
- [x] Added reader configuration/pool and repository injection. Verified isolated SELECT-only routing, denied writes, explicit missing-config failure, and no primary fallback. Production replica provisioning remains a rollout prerequisite.
- [ ] Implement and benchmark summary-first reporting: deduplicated daily facet marginals/totals, atomic publication and dirty-day repair, replica-visible source splitting, period-first top-six ranking, then bounded daily series/remainder. Keep raw `FINAL` only for builds, unsettled days, and partial boundaries. Record EXPLAIN pruning, latency, rows/bytes read, and peak memory. Gate remains p95 <= 1s per cycle and <= 2s per maximum three-calendar-month request at five readers within existing server memory limits; exercise 20,000-user facets and representative full-window retention. The reported 29M rows/week motivates fixture size, not a proven benchmark.
- [x] Added the usage Goa method, embedded cycle-window metadata, family/facet catalog and explicit response contract. Regenerated with `mise run gen:goa-server` and `mise run gen:sdk` (the SDK task supplies its own skip-versioning/upload flags).
- [x] Retained independent billing position/estimate and migrated the explorer to storage readings with corrected units/facets.
- [x] Added bandwidth and six-scanner views, aligned arrays, exact weekly/monthly/cumulative arithmetic, separate adjustments, and existing time/legend controls.
- [x] Seeded all nine meters and verified source/API/browser reconciliation, local demo rendering, repeatable seeding, and tenant safety. Performance acceptance remains open in step 3.

### Step 3 execution order

1. Evaluate native refresh correctness and full-retained-history rebuild cost at representative volume. Define publication metadata, coherent request snapshots, and replica readiness. Select targeted maintenance only with evidence that the native rebuild is unsuitable.
2. Generate the chosen schemas through Atlas; implement publication, first-build catch-up, and the selected refresh/repair schedule. If targeted maintenance is necessary, establish durable dirty generations before cutover.
3. Replace the historical read path with the request algorithm above, preserving API/response semantics and the three-calendar-month limit.
4. Integrate demo summaries; prove publication/ranking/conservation edge cases and run representative replica benchmarks, including ingestion during builds. Close step 3 only after the original latency/memory gate is met.

## Verification

- Real ClickHouse regression cases: identical redeliveries across insert batches before merges; one contribution after `FINAL`; month-spanning queries with independent-partition FINAL; ordinary usage unaffected by separately selected positive/negative adjustments; missing adjustment attributes; net-zero adjustment periods with activity. Remove obsolete tests expecting later payloads to move an accepted reading's day/facets or win by `produced_at`.
- Prove `sum(daily) = period total = sum(facet totals including remainder)` exactly **within each reading kind**; same for direction/scanner series. Test unset values, same slug in different server-type namespaces, directory-group sets, quantities above JS safe-integer range, cross-org rejection and deleted-project inclusion.
- UTC boundaries: `[from,to)`, month-end anchor clamping, leap dates, custom partial-day ranges, Monday weekly rollups and click/drag clamping. Old closed periods refetch to show late facts; no mutation of previously accepted usage.
- Response/chart edge cases: dense zero days, an entirely empty range, partial-day clipping, equal-length aligned series, stable full-period top-six ranking, remainder conservation, real facet labels “Other”/“Not set”, exact cumulative/weekly/monthly arithmetic above JS safe-integer range, and signed adjustment stacks. Assert consumer-visible values rather than generated type/source text.
- Summary regression cases: a period winner absent from every daily top six; live facts changing the winner set; net-zero omitted adjustments still producing an activity-bearing remainder; independent facet totals conserved without cross-product duplication; incompatible measurements rejected.
- Publication regression cases: duplicate deliveries before/after midnight; empty published coverage; failed/repeated builds; late original facts and adjustments; inserts during refresh; builder/reader replica lag; no overlapping or omitted source intervals; consistent rank/total/series input during replacement. Add dirty-generation race cases only if targeted maintenance is selected.
- Run focused `mise run test:server ./internal/metering/chrepo/... ./internal/usage/...`; preserve existing invoice/allowance outputs. Run dashboard type-check; browser-smoke three families, separate reading kinds, legend/cumulative controls, signed adjustment buckets, exact tooltips, meter-only activity, missing history and isolated errors. Smoke Costs if its shared chart changes.
- Run local seed twice and demo-seed safety suite; verify Billing. Test the new API against a fixture database with no deprecated ledger to prove zero dependence; do not drop any live table. No production inserts/backfill during verification.
- Command/runtime verification: API queries reach the replica; writes still reach the existing writer; missing/invalid reader configuration fails explicitly; replica outage yields usage failure without querying primary; both pools close. Exercise delayed replica visibility on an actual replicated environment before rollout.
- Compare original and promoted schemas for insert throughput, batch latency, peak memory, and stored bytes per row using identical inputs/batch sizes/concurrency. Report one-time existing-row materialization separately from ongoing ingestion overhead.
- Benchmark completed-day-only and mixed-history/live requests separately, across low/high-cardinality facets, current cycle and maximum window, tenant skew, and five concurrent readers. Compare raw rows avoided and summary rows read; confirm period-first ranking does not recreate the day/value window state. Measure snapshot duration, summary storage, and ingestion throughput/latency while builds run. Keep fixture volume assumptions and Cloud/replica limits explicit.
- Local implementation evidence is recorded below. It does not establish production replica latency or replication lag; the proposed performance gate remains unmet locally.

## Unresolved questions

- Product decisions resolved: new production table only; immutable accepted usage; separate adjustments; visualization-only; additive API; existing APIs unchanged; read-replica-only; historical incompleteness accepted; maximum three calendar months per request. Production replica configuration, representative-load performance acceptance, delayed visibility on a replicated environment, and upstream frozen-enrichment follow-through remain integration dependencies.
- No new product decision is required: independent facet summaries fit the current API. Implementation prerequisites: measure native full-refresh suitability, prove coherent per-request snapshot reads and replica-visible publication metadata, and bound first-publication/catch-up work. Durable dirty-generation storage/source visibility is conditional on selecting targeted maintenance.
- Implementation skills: `clickhouse`, `golang`, `gram-management-api`, `gram-rbac`, `frontend`, `vercel-react-best-practices`, `page-toolbar`, `gram-demo-seed`, `gram-playwright-cli`; `gram-temporal`, `gram-pubsub`, and `postgresql` only if targeted maintenance changes those systems; `pitchfork` for local services. Referenced ClickHouse advisor skills are unavailable in this harness; official docs above were consulted instead.

## Local verification evidence

- ClickHouse 26.2.19.43, existing 1 GiB server memory limit. Synthetic fixture: 29 million hot-week rows plus one million historical storage readings across 13 monthly partitions; this is **not** three months retained at 29 million rows/week.
- Equivalent SQL at five readers: bounded execution (`max_threads=2`, `max_final_threads=2`, `max_block_size=1024`) completed 20 cycle and 20 three-month requests without memory errors. Cycle p95 2.96s; three-month p95 2.09s; largest reported query allocation 114,168,632 bytes. Proposed 1s/2s targets remain unmet. Unbounded-default runs hit the existing memory limit; no memory limit was raised.
- Three-month EXPLAIN snapshot: time pruning selected 22/64 parts, then organization/meter/time primary-key pruning selected 313/3581 remaining granules. Background merges can change part counts. Detailed local samples and EXPLAIN are in ignored `.playwright-cli/meter-usage-benchmark.json`.
- Isolated fixture database contained only the new ledger. SELECT-only reporting returned HTTP 200; revoking SELECT returned 500 while legacy billing remained 200; restoring SELECT restored 200. INSERT was denied. Running the seed with these reader credentials still wrote through the unchanged writer connection.
- Authenticated HTTP smoke covered all 41 allowed family/facet selections for each reading kind (82 requests), exact daily/series/period conservation, aligned zero-fill, remainder and unset markers. Three-month boundary checks covered month-end clamping, leap years, UTC normalization, and rejection one nanosecond beyond the limit.
- Browser checks: all three families, signed scanner adjustments, calendar selection, clipped weekly drilldown, legend visibility, cumulative charts, exact values above JavaScript's safe-integer limit, net-zero rollups, independent simulated failure boundaries, unchanged Costs chart, and local demo mode.
- Seed applied twice; 15 demo-safety tests passed. Final usage suite passed 265 tests at parallelism two after a prior full run encountered a test-database cleanup connection failure. Metering/repository and command packages passed separately. Dashboard type-check and 24 existing Billing tests passed; server lint passed with its existing deprecated-linter warning.
- Earlier reduced-catalog verification, before the final three-attribute removal: Atlas generated one additive 19-column migration with matching golang-migrate output. It applied locally without changing raw fact count/digest. Goa and SDK regenerated; 51 metering/repository tests and dashboard type-check passed. Live menus then contained 14 storage, 6 bandwidth, and 7 risk selections. Insert/storage measurements below also describe that 19-column schema, not the current 16-column target.
- Promoted-column insert A/B/B/A benchmark: identical synthetic inputs, all nine meters, 20,000-user catalog, 1,000 rows/batch, one concurrent writer, 200 measured batches/schema plus one warmup. Baseline: 3,704 rows/s, p50 259.38ms, p95 321.48ms. Nineteen promoted columns: 3,551 rows/s, p50 265.91ms, p95 372.95ms. Observed throughput -4.1%, p50 +2.5%, p95 +16.0%; local variance is material, not a production forecast. All 400 measured inserts succeeded.
- Compacted storage for those identical 201,000 rows/schema: baseline 27,963,447 compressed bytes (139.12 bytes/row); promoted 28,915,787 (143.86 bytes/row), +3.4%. Reliable insert peak memory was unavailable: native samples did not yield an unambiguous query peak, and this server has no `system.query_log`. Existing-part materialization was not included in this steady-state benchmark.
- A separate 34-million-row fixture spread over roughly three months completed 25/25 department reads at five readers (p95 477ms), but 20,000-user grouping failed 25/25 at the existing 1 GiB server limit. This is not three months at 29 million rows/week; the release performance gate remains open and motivates the rollup design.
- Final facet reduction: Atlas regenerated one additive migration for 16 promoted columns, with identical golang-migrate forward SQL and no forward drops. It applied locally without changing raw fact count/digest. Goa/SDK generation, 51 metering/repository tests, dashboard type-check, and server lint passed. Live menus contain 13 storage, 5 bandwidth, and 7 risk selections. Authenticated API smoke returned ID-only billing-user labels (200), and rejected `role_set` and `custom_domain` (400).
- Native refresh correctness prototype on ClickHouse 26.2.19.43: all nine meters and independent retained facets; 18,101 physical rows deduplicated to 18,001 accepted facts after a late insert. Repeated refreshes preserved exact results; an intentional failed refresh preserved the previous complete snapshot; recovery incorporated the late fact; sampled concurrent reads saw only complete old/new snapshots. Every generated facet's signed quantity and reading count reconciled to its raw family/day totals. This is correctness evidence, not representative rebuild performance or application integration.
- The oversized native fixture load was stopped after the reported nearly 30-minute run. Its 103 million rows cover only June 12–July 6, not a representative full three-month window. A bounded refresh using the production summary definition failed its 384 MiB query memory cap. The partial fixture is preserved with refresh and source merges stopped; no full-volume pass is claimed.
- A small SQL reproduction showed that seed-side deletes on the refresh-owned target did not take effect on this local build. Direct summary mutation was abandoned in favor of raw seeding followed by the native publisher. Further ClickHouse source investigation was stopped.
- Application integration: 14 focused meter-reporting tests passed, including native publication, missing/stale coverage, historical/live-tail conservation, and period-first ranking. The tagged demo-seed suite passed 14 tests, including two native-published reseeds with unchanged other-tenant facts and summary data. Removed assertions that pinned error wording or SQL source layout.
- Authenticated summary-backed API smoke: all 25 facets × two reading kinds passed over July 1–October 1 at five readers, with 92 aligned UTC buckets and exact ledger/daily/series/period conservation. Browser checks confirmed storage, bandwidth, and signed scanner-adjustment charts. These used synthetic demo data, not representative retention.
- Direct repository smoke on the same small fixture: 15/15 three-month billing-user requests at five readers, p95 192.4ms, at most 6,457 rows / 445,183 bytes read per request, and 72 returned daily cells. This does not satisfy the large-fixture or replica performance gate. Samples are in ignored `.playwright-cli/meter-summary-{http,native}-proof.json`.
- Local seed execution completed for both local and demo orgs once and for the local org a second time. Further attempts failed at the existing telemetry scoped-delete phase under the local 1 GiB limit, before meter reseeding. The failed attempts' retrying telemetry mutations were cancelled. No memory cap was raised and no unrelated telemetry repair was added.
- Both new Atlas migrations applied locally; generated native-summary forward DDL matches the golang-migrate flavor apart from comments. Server lint passed with only its existing deprecated-linter warning.
