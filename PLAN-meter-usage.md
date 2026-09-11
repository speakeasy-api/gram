# Billing usage from meter readings

## Context

- Production now writes `billing_meter_readings_by_time` (user-confirmed). It is the sole source for the new usage explorer. `billing_meter_readings` is deprecated and will be removed separately: no reads, unions, fallback, new seed rows, or removal of that table in this change.
- TUM is exactly `MeterAgentSessionStorage` (`stokens`); MCP ingress/egress are bytes; all six risk scanners meter scanned content in `stokens`.
- All nine usage meters follow the first-accepted immutable-fact contract: quantity, occurrence time and reporting attribution are frozen; adjustments are separate facts. The new table still uses unversioned `ReplacingMergeTree` to converge duplicate deliveries, so readers must use `FINAL` before aggregation.
- Upstream inspected at `aac036405e2ed87edac0517117413004cb1781bf`. Schema and writer cutover already landed; do not recreate them. Local branch `meter-facets` is not stacked; rebase onto current `origin/main` after planning mode is lifted, preserving this untracked plan. This revision was prepared from upstream sources without changing the checkout.

## Approach

- One meter-backed usage explorer for TUM, MCP bandwidth, and risk content scans; keep units separate.
- Read bounded organization/meter/time windows from `billing_meter_readings_by_time FINAL` on the read replica with `do_not_merge_across_partitions_select_final = 1`. No telemetry-derived meter figures and no deprecated-table dependency.
- Preserve cycle/custom-period selection and daily/weekly/monthly/cumulative chart controls.
- Confirmed scope: **usage visualization only**. Preserve invoice estimates, pricing, allowance/overage calculations and finalized records in a separate, clearly labeled billing section. Never normalize meter totals to legacy billed totals.
- API constraint confirmed: add a **new Goa method under `server/design/usage/design.go`**. Do not replace/remove existing methods. Migrate only the dashboard usage explorer to it.
- All new meter-reporting reads **must use the ClickHouse read replica**, never the existing writer connection.

### ClickHouse decision

- Deployed layout: `ENGINE = ReplacingMergeTree` (no version argument), `PARTITION BY toYYYYMM(occurred_at)`, `PRIMARY KEY (organization_id, meter_id, occurred_at)`, `ORDER BY (organization_id, meter_id, occurred_at, project_id, id)`.
- Frozen occurrence time/full sorting key keep every retry in the same month and replacement group. Organization + exact meter IDs + `[from,to)` now support primary-key and partition pruning. Keep time predicates directly in the query; remove the old moved-timestamp/global-dedup query strategy.
- Use `FINAL` and `SETTINGS do_not_merge_across_partitions_select_final = 1` for every reporting aggregation. Neither unversioned replacement nor background merges enforce first acceptance of conflicting payloads; the accepted upstream representation is the producer contract, not something the read API reconstructs.
- User-approved performance follow-up: promote the 19 retained reporting attributes from the frozen Map into scalar materialized columns. Use `String` for unbounded identities/labels and `LowCardinality(String)` for categorical attributes. Keep the source Map and accepted facts unchanged. Atlas alone generates and manages migrations from `schema.sql`—never hand-edit generated migration files. Materialize existing parts as a separate operational action, not as hand-authored migration SQL.
- Daily-rollup design supersedes further full-range raw-query tuning; see below. Do not use an insert-triggered summing MV: identical physical retries reach it before replacement merges.
- Sources: [deployed schema](https://github.com/speakeasy-api/gram/blob/aac036405e2ed87edac0517117413004cb1781bf/server/clickhouse/schema.sql), [writer/read contract](https://github.com/speakeasy-api/gram/blob/aac036405e2ed87edac0517117413004cb1781bf/server/internal/metering/chrepo/queries.go), [ReplacingMergeTree / FINAL](https://clickhouse.com/docs/guides/replacing-merge-tree).

### Proposed daily rollups and live tail

- **Design, not implemented.** Keep the raw ledger authoritative. Build complete UTC-day snapshots from `FINAL`, then publish by atomic `REPLACE PARTITION` into a daily `MergeTree` summary table. Matching staging/summary schemas and every schema migration are Atlas-managed. Runtime partition publication is an application operation, never a hand-edited migration.
- Summary grain: `(UTC day, organization, family, reading kind, facet, series kind, series key, unit, measurement method)`, with an exact `Int128` sum and deterministic label. Retain every facet value, not daily top six: a value outside each day's top six can still rank in the full period. Preserve compatibility metadata and net-zero adjustment groups with activity.
- Partition by UTC day; order by organization, family, reading kind, facet, series kind, series key. A rebuild replaces the complete day across tenants/facets, not an additive delta. Include a publication marker in the same partition, including empty days, so missing data cannot masquerade as a valid zero.
- Build yesterday after 01:00 UTC, allowing the expected one-hour retry window after midnight. Until successful publication, yesterday remains part of the live tail alongside today. Publication, not the clock alone, moves the raw/summary boundary. A failed build leaves the old complete snapshot intact.
- API reads remain replica-only. Resolve a contiguous published-day boundary on that reader; use summaries before it and raw `FINAL` at/after it, with no overlap. Recheck required publication markers in the summary result; missing replica-visible coverage must be an explicit failure, never a silent zero or writer fallback.
- Preserve arbitrary `[from,to)` semantics: incomplete first/last historic days must use raw aggregation because daily sums cannot answer an intraday slice. Use summaries only for fully included closed days. Combine summary and raw aggregates, choose the top six across the entire requested period, and then construct dense daily buckets/remainder.
- One-hour duplicate lateness is not a bound on first delivery or adjustment arrival. Keep late-day repair: successful ingest must durably mark affected closed dates dirty before acknowledgment; a worker rebuilds those dates from `FINAL`. Capture a dirty generation before building and acknowledge only that generation after publication. A concurrent late insert leaves a newer dirty generation pending. Do not assume a best-effort post-ACK notification is sufficient.
- Use one hourly schedule with one batched activity for daily publication and dirty-date repair; serialize partition publication and process dates inside that activity, not one workflow per row/org. Fixed Temporal cost per 30-day month/namespace: approximately 720 starts + 720 activity attempts = 1,440 actions before retries. Raw ingestion performs no Temporal calls.
- The hourly repair gives historical views eventual freshness; old snapshots remain readable while rebuilding. Builder source visibility and durable dirty-marker storage must be resolved together so a marker can never be cleared before its raw insert is visible. This is the remaining implementation boundary, not a claim that two ClickHouse tables replicate atomically.
- Verify before cutover: retries before/after midnight, failed and repeated publication, late original facts, separate late adjustments, insert-during-build races, empty published days, replica lag, partial-day slices, global top-six correctness, and exact conservation. Benchmark full three-month retention at representative ingestion volume and five concurrent readers, including 20,000-user facets. Also measure build duration and ingestion impact while a rebuild runs.
- Reference: ClickHouse documents [`REPLACE PARTITION` as atomic](https://clickhouse.com/docs/sql-reference/statements/alter/partition#replace-partition). Local proof is not Cloud/replicated deployment proof.

### Read-replica wiring

- Upstream command layer still has only `clickHouseFlags` and `newClickhouseClient` (`server/cmd/gram/flags_clickhouse.go`, `deps.go`); the dedicated reader remains work for this change. Re-ground `start.go` wiring after rebase rather than relying on old line numbers.
- Add `clickhouse-read-*` / `CLICKHOUSE_READ_*` host, native port, database, username, password and TLS-verification configuration for the API command. Build a distinct traced connection pool and inject **only that pool** into the new meter read repository. Reuse the existing dial/ping/TLS/close machinery through explicit connection options; leave writer callers unchanged.
- Require explicit reader endpoint configuration for the API command; no silent writer fallback on missing config or replica failure. Reader configuration must not become mandatory for unrelated writer/migration commands. Clean up both clients on startup failure and shutdown.
- Add explicit local reader values to `mise.toml`, pointing at local ClickHouse and following the PostgreSQL read-replica convention. Production reader endpoint/credentials must be configured before deployment; a table cutover does not establish replica-client configuration.
- Use a SELECT-only reader principal on `billing_meter_readings_by_time`, provisioned through infrastructure ownership conventions, not application DDL. Keep connection errors and secrets out of responses/logs. Replica lag is eventual consistency: query time is retrieval time, not proof of ingestion completeness; no read-your-write promise or fallback to primary.
- Benchmark on the read replica. Verify selected endpoint with distinct reader/writer configurations, permission-limited credentials and server query evidence. A single local ClickHouse instance proves routing/configuration, **not actual replica lag behavior**.

### Meter views and facets

| View                    | Meter selection / unit                           | Default stacks   | Facets                                                                                                                                                      |
| ----------------------- | ------------------------------------------------ | ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Tokens under management | `gram.agent_session.storage` / s-tokens          | Total            | Project, model, provider, billing mode, assistant; billing user, division, department, job title, employee type, cost center, role set, directory group set |
| MCP bandwidth           | `gram.mcp.bandwidth.ingress` + `.egress` / bytes | Ingress / egress | Project, MCP server (type + ID identity, slug label), server type, custom domain                                                                            |
| Risk content scans      | Exact six `MeterRisk*` definitions / s-tokens    | Scanner          | Project, policy, judge model/provider, tool name                                                                                                            |

- Risk means **volume scanned**, not detection count or unique content: content scanned by several scanners legitimately contributes to each. No scan-count headline inferred from signed readings.
- Scanner series: Gitleaks, Presidio, prompt injection, prompt policy, custom rules, CLI destructive. Model/provider here identify the **scanner judge**, not the scanned agent; non-LLM scanners normally leave these unset.
- Bandwidth is application-visible body bytes, not headers/framing or upstream fanout. Its producer emits no user/directory facets; risk emits message-user identity but no billing-user enrichment. Storage has no emitted message-type dimension. Show only family-supported facets.
- TUM is canonical stored-message workload, not provider token consumption. Remove input/output/cache token-type stacks and old cache/inference-exclusion copy. Never add risk s-tokens to TUM just because the units match.
- Attribute absence is explicit `(unset)`; do not infer billing user from message actor or carry current telemetry identity into the ledger. High-cardinality message/request/conversation IDs remain provenance, not default chart facets.
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
- Billing-user roles/groups are multi-valued: expose intact sorted sets as **Role set / Directory group set**, not exploded overlapping stacks. This preserves additive totals without inventing a cost allocation rule.
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
- Show only readings present in the new ledger. No backfill, legacy-history fallback, synthetic historical TUM, import replay, or rescanning. Empty ranges say “No meter readings recorded.” A brief explanation that usage reflects the new metering system is sufficient; no coverage-status API or completeness tracking.
- Keep `getTokensUnderManagement`, `telemetry.queryTumDetails`, their repository helpers/tests and PAYG/finalization paths intact. Preserve the existing contract-position card/admin estimator separately. A legacy-query failure must not prevent meter usage from rendering.
- New explorer uses date-only cycle metadata from the new API, never the old `BillingCycle.tokens/days`, `billedDaysFromCycles`, `resolvePeriodFigures`, or overage weights. Meter cumulative tables show usage, **not allocated invoice overage**.

### Upstream contract boundary

- Already landed: time-oriented table, `chrepo.InsertReadings` writing it, and `WriteCorrelated` emitting only when `stored.Inserted`. Do not duplicate that work.
- Remaining upstream caveat: at the inspected revision, `MeterReadingCHWriter.enrichBillingUsers` still recomputes directory fields for each batch, and batch dedup selects a later producer variant. First-accepted/frozen attribution is the agreed contract, but the table does not enforce it. Record this as a producer/consumer follow-through dependency, not as permission for the dashboard to reconstruct a different winner or refresh directory joins.
- This visualization change does not migrate producer acceptance/enrichment, backfill production, enforce physical exactly-once inserts, or remove the deprecated ledger. Existing `FINAL` reads handle same-key copies; conflicting occurrence keys are an upstream invariant violation, not a query fallback case.

## Files to modify

- `server/design/usage/design.go`; new `server/internal/usage/meter_usage.go`; `server/internal/usage/impl.go` and service-constructor callsites in `server/cmd/gram/start.go`/test setup: additive API and repository injection.
- New `server/internal/metering/chrepo/usage.go` and focused real-ClickHouse regression tests; reuse neighboring Squirrel/query/scan conventions.
- `client/dashboard/src/pages/billing/Billing.tsx` and billing `tum-section.tsx`, `tum-queries.ts`, `tum-token-breakdown.tsx`, `token-usage-panel.tsx`, `tum-details-table.tsx`, `tum-usage-card.tsx`, `breakdown-options.ts`, `use-billing-period.ts`: separate billing position from generic meter usage. Use LSP renames/references for TUM-only components migrated to meter-generic names; retain admin-used helpers.
- `client/dashboard/src/components/stacked-time-series-panel.tsx` only where required for signed/large quantities; preserve its Costs caller behavior.
- `server/internal/demoseed/clickhouse.sql`, `spec.go` if needed, and `seed/demo/PAGES.md`: seed `billing_meter_readings_by_time` for all **nine** meters, varied facets, separate signed adjustments and duplicate-delivery fixtures; scoped cleanup/postflight on the new table. Retain legacy telemetry fixtures for unrelated billing paths.
- Generated Goa/OpenAPI/SDK outputs via generators; changeset. No new base-table migration, writer cutover, production backfill or deprecated-table removal.
- `server/cmd/gram/flags_clickhouse.go`, `deps.go`, `start.go`, `mise.toml`, and `.mise-tasks/zero/remap-ports.mts`: dedicated reader flags/client/lifecycle and explicit local configuration; derived reader ports follow their primary port instead of being randomized separately. Production deployment settings must be supplied by their owner before rollout.

## Reuse

- Meter IDs/units: `server/internal/metering/definition.go`.
- Provenance catalog: `server/internal/metering/attributes.go`.
- `server/internal/usage/billing_cycle.go`: `BillingCycles`/UTC anchor logic; `tum.go` org-read authorization and billing metadata lookup, not its telemetry totals.
- `BillingCyclePicker`, `TimeRangePicker`, `useBillingPeriod` date selection, `StackedTimeSeriesPanel`, palette, inline failures, and project-name lookup. Separate date-only typing without changing existing invoice/admin helpers.

## Steps

- [x] Rebased `meter-facets` onto `origin/main` at `07ffd9645d`; retained upstream table/writer changes.
- [x] Added reader configuration/pool and repository injection. Verified isolated SELECT-only routing, denied writes, explicit missing-config failure, and no primary fallback. Production replica provisioning remains a rollout prerequisite.
- [ ] Implement and benchmark time-pruned `FINAL` queries on the new table, including month-spanning ranges, large-tenant skew, duplicate copies and separate adjustments; record EXPLAIN partition/granule pruning, latency, rows/bytes read and peak memory. Proposed gate: p95 <= 1s per cycle and <= 2s for the maximum three-month window at five readers within server memory limits. The reported 29M rows/week motivates realistic volume; it is not a measured benchmark of this new table.
- [x] Added the usage Goa method, embedded cycle-window metadata, family/facet catalog and explicit response contract. Regenerated with `mise run gen:goa-server` and `mise run gen:sdk` (the SDK task supplies its own skip-versioning/upload flags).
- [x] Retained independent billing position/estimate and migrated the explorer to storage readings with corrected units/facets.
- [x] Added bandwidth and six-scanner views, aligned arrays, exact weekly/monthly/cumulative arithmetic, separate adjustments, and existing time/legend controls.
- [x] Seeded all nine meters and verified source/API/browser reconciliation, local demo rendering, repeatable seeding, and tenant safety. Performance acceptance remains open in step 3.

## Verification

- Real ClickHouse regression cases: identical redeliveries across insert batches before merges; one contribution after `FINAL`; month-spanning queries with independent-partition FINAL; ordinary usage unaffected by separately selected positive/negative adjustments; missing adjustment attributes; net-zero adjustment periods with activity. Remove obsolete tests expecting later payloads to move an accepted reading's day/facets or win by `produced_at`.
- Prove `sum(daily) = period total = sum(facet totals including remainder)` exactly **within each reading kind**; same for direction/scanner series. Test unset values, same slug in different server-type namespaces, role/group sets, quantities above JS safe-integer range, cross-org rejection and deleted-project inclusion.
- UTC boundaries: `[from,to)`, month-end anchor clamping, leap dates, custom partial-day ranges, Monday weekly rollups and click/drag clamping. Old closed periods refetch to show late facts; no mutation of previously accepted usage.
- Response/chart edge cases: dense zero days, an entirely empty range, partial-day clipping, equal-length aligned series, stable full-period top-six ranking, remainder conservation, real facet labels “Other”/“Not set”, exact cumulative/weekly/monthly arithmetic above JS safe-integer range, and signed adjustment stacks. Assert consumer-visible values rather than generated type/source text.
- Run focused `mise run test:server ./internal/metering/chrepo/... ./internal/usage/...`; preserve existing invoice/allowance outputs. Run dashboard type-check; browser-smoke three families, separate reading kinds, legend/cumulative controls, signed adjustment buckets, exact tooltips, meter-only activity, missing history and isolated errors. Smoke Costs if its shared chart changes.
- Run local seed twice and demo-seed safety suite; verify Billing. Test the new API against a fixture database with no deprecated ledger to prove zero dependence; do not drop any live table. No production inserts/backfill during verification.
- Command/runtime verification: API queries reach the replica; writes still reach the existing writer; missing/invalid reader configuration fails explicitly; replica outage yields usage failure without querying primary; both pools close. Exercise delayed replica visibility on an actual replicated environment before rollout.
- Compare original and promoted schemas for insert throughput, batch latency, peak memory, and stored bytes per row using identical inputs/batch sizes/concurrency. Report one-time existing-row materialization separately from ongoing ingestion overhead.
- Local implementation evidence is recorded below. It does not establish production replica latency or replication lag; the proposed performance gate remains unmet locally.

## Unresolved questions

- Product decisions resolved: new production table only; immutable accepted usage; separate adjustments; visualization-only; additive API; existing APIs unchanged; read-replica-only; historical incompleteness accepted; maximum three calendar months per request. Production replica configuration, representative-load performance acceptance, delayed visibility on a replicated environment, and upstream frozen-enrichment follow-through remain integration dependencies.
- Implementation skills: `clickhouse`, `golang`, `gram-management-api`, `gram-rbac`, `frontend`, `vercel-react-best-practices`, `page-toolbar`, `gram-demo-seed`, `gram-playwright-cli`; `pitchfork` when running local services. The ClickHouse skill's referenced best-practices/advisor skills are unavailable in this harness; official docs above were consulted instead.

## Local verification evidence

- ClickHouse 26.2.19.43, existing 1 GiB server memory limit. Synthetic fixture: 29 million hot-week rows plus one million historical storage readings across 13 monthly partitions; this is **not** three months retained at 29 million rows/week.
- Equivalent SQL at five readers: bounded execution (`max_threads=2`, `max_final_threads=2`, `max_block_size=1024`) completed 20 cycle and 20 three-month requests without memory errors. Cycle p95 2.96s; three-month p95 2.09s; largest reported query allocation 114,168,632 bytes. Proposed 1s/2s targets remain unmet. Unbounded-default runs hit the existing memory limit; no memory limit was raised.
- Three-month EXPLAIN snapshot: time pruning selected 22/64 parts, then organization/meter/time primary-key pruning selected 313/3581 remaining granules. Background merges can change part counts. Detailed local samples and EXPLAIN are in ignored `.playwright-cli/meter-usage-benchmark.json`.
- Isolated fixture database contained only the new ledger. SELECT-only reporting returned HTTP 200; revoking SELECT returned 500 while legacy billing remained 200; restoring SELECT restored 200. INSERT was denied. Running the seed with these reader credentials still wrote through the unchanged writer connection.
- Authenticated HTTP smoke covered all 41 allowed family/facet selections for each reading kind (82 requests), exact daily/series/period conservation, aligned zero-fill, remainder and unset markers. Three-month boundary checks covered month-end clamping, leap years, UTC normalization, and rejection one nanosecond beyond the limit.
- Browser checks: all three families, signed scanner adjustments, calendar selection, clipped weekly drilldown, legend visibility, cumulative charts, exact values above JavaScript's safe-integer limit, net-zero rollups, independent simulated failure boundaries, unchanged Costs chart, and local demo mode.
- Seed applied twice; 15 demo-safety tests passed. Final usage suite passed 265 tests at parallelism two after a prior full run encountered a test-database cleanup connection failure. Metering/repository and command packages passed separately. Dashboard type-check and 24 existing Billing tests passed; server lint passed with its existing deprecated-linter warning.
- Reduced-catalog verification: the initial add/remove migrations were discarded and Atlas regenerated one additive migration for the 19 retained columns, with matching golang-migrate output. The consolidated migration applied locally; raw fact count and digest remained unchanged. Goa and SDK regenerated. All 51 metering/repository tests and dashboard type-check passed. Live dashboard menus contain 14 storage, 6 bandwidth, and 7 risk selections, including totals.
- Promoted-column insert A/B/B/A benchmark: identical synthetic inputs, all nine meters, 20,000-user catalog, 1,000 rows/batch, one concurrent writer, 200 measured batches/schema plus one warmup. Baseline: 3,704 rows/s, p50 259.38ms, p95 321.48ms. Nineteen promoted columns: 3,551 rows/s, p50 265.91ms, p95 372.95ms. Observed throughput -4.1%, p50 +2.5%, p95 +16.0%; local variance is material, not a production forecast. All 400 measured inserts succeeded.
- Compacted storage for those identical 201,000 rows/schema: baseline 27,963,447 compressed bytes (139.12 bytes/row); promoted 28,915,787 (143.86 bytes/row), +3.4%. Reliable insert peak memory was unavailable: native samples did not yield an unambiguous query peak, and this server has no `system.query_log`. Existing-part materialization was not included in this steady-state benchmark.
- A separate 34-million-row fixture spread over roughly three months completed 25/25 department reads at five readers (p95 477ms), but 20,000-user grouping failed 25/25 at the existing 1 GiB server limit. This is not three months at 29 million rows/week; the release performance gate remains open and motivates the rollup design.
