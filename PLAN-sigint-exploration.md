# Signal Intelligence exploration plan

Agreed first epic, 2026-10-18, incorporating the independent Opus review.
Initial stack base: freshly fetched `origin/main` (`12dd47d8a7`). The completed
stack is rebased against `origin/main` before handoff.

## Agreed scope

- Fast, trustworthy time-series exploration shared by the dashboard and Platform MCP.
- Minute, hour and UTC calendar-month aggregation; arbitrary reading-attribute grouping and filtering.
- Exact, primary-key-qualified canonical `FINAL` reads first, measured on ClickHouse 26.6.
- Session presence, breadth, concentration, co-occurrence, prior-window comparison and evidence.
- Intelligent resolution planning must distinguish fewer output points from less physical query work.
- Sigint is experimental: use the unversioned `sigint-reading` identity tag and avoid speculative compatibility, backfills or feature-version machinery.

## Measurement and provenance contract

- Every metric names its sensor(s), counting unit, hit predicate, population and denominator. A shared signal ID does not establish equivalent measurement across sensors.
- Keep streaming (zero run UUID) and explicitly selected manual runs separate.
- Count original utterances; retain generation/replay lineage for inspection. Carry message generation and sequence, identify exact transcript re-persistence at capture, and distinguish synthetic compaction summaries. Do not deduplicate genuinely repeated utterances by content alone. This gates message counts and time-local findings.
- Distinguish evaluation outcomes, matching messages, signal assignments and distinct conversations. A sigint session is a `conversation_id`; producer session IDs in other datasets need an explicit mapping.
- Multi-label hits use an explicit probability threshold; exclusive hits use the selected signal. Ordered hits use an explicit level/distribution predicate, not an unjustified average of ordinal indices.
- Report answered, not-applicable, draft and failed companions. Rates initially describe answered observations. Missing evaluation coverage stays unknown until eligible-message and activation-history accounting exists.
- Capture inspectable evaluation-definition snapshots keyed by the existing hash, plus activation history needed for a future census. Audit before/after snapshots already exist; assess reuse rather than claiming history is absent.
- Derive definition/model/input-strategy mix from contributing readings. Mark incomplete edge buckets and unsettled comparisons; an unobserved bucket is not automatically a supported zero.

## Message-time attributes

Capture an immutable creation-event snapshot, preserved by evaluation retries and outcomes:

| Key              | Meaning                                                               | Examples                                       |
| ---------------- | --------------------------------------------------------------------- | ---------------------------------------------- |
| `model_family`   | Model vendor identified by the model name                             | `anthropic`, `openai`                          |
| `model_provider` | Serving provider reported by ingestion, independently of model family | `openrouter`, `bedrock`, `anthropic`, `openai` |
| `model_name`     | Model identifier recorded on the message                              | `anthropic/claude-opus-5`, `gpt-6.1-sol`       |
| `department`     | Message actor's organization-scoped directory department at capture   | `engineering`, `marketing`                     |

Omit unknown or blank values; never write an `<unset>` sentinel. Preserve the model identifier and department spelling. Normalize known provider aliases. Do not infer a serving provider from a model family, or a department from billing allocation. Historical imports snapshot information available at ingestion, not an invented historical directory state. Source-message model labels are distinct from the sensor's classifier model/provider columns.

These are the first populated labels, not a closed query vocabulary. Future arbitrary labels remain queryable within explicit budgets. Fingerprints/catalogs are benchmark candidates, not prerequisites.

Producer contract: an explicit `ModelProvider` field distinguishes serving facts from the existing account/metering `Provider`. Completion responses and known first-party imports supply it. Hook account metadata alone leaves it unknown.

## Primary-key-first query architecture

**Workload:** interactive product analytics, late attempts/imports, bounded evidence lookups; two-year request-time retention. Latency/scale targets await measurement.

**Derived design, high confidence on correctness:**

- Canonical source: `sigint_readings_v2`, `ReplacingMergeTree(evaluated_at)`; latest complete attempt wins, including a failure replacing an answer.
- Required source parameters inside the `FINAL` query: authorized organization/project, trigger/run, sensor selection, half-open event-time range. Keep mutable filters after replacement and avoid mutable-column `PREWHERE`.
- Sparse primary key ends at `event_occurred_at`; `id` only extends the sorting key. Reading-ID candidates alone do not prune the primary index. Evidence needs sensor/time coordinates or a measured additional access path.
- Partitioning and retention use immutable `evaluation_requested_at`; activity buckets use `event_occurred_at`. Add request-time partition bounds only when provable, such as an exact manual-run timestamp.
- Explicitly validate `do_not_merge_across_partitions_select_final = 1`: attempts share request time and partition, but the 26.6 automatic inference does not follow from this primary key. Test the invariant and diagnose violations.
- Read only required columns, including probability subcolumns. Compute headline, outcome companions and definition mix together where practical.
- Server planner chooses output step from span, groups and point budget; chooses a compatible physical source independently; enforces scan/memory/runtime limits. Explicit fine resolution is honored or rejected with an actionable reason.
- Select top groups over the full window, then return complete series. Replace silent 1,000-row truncation with point budgeting and completeness metadata.
- Canonical reads are the correctness oracle. If they miss agreed budgets, benchmark one hourly acceleration tier before additional tiers, with exact boundary handling and correct distinct-session reaggregation.
- Correction-aware aggregates must remove vanished groups and follow request-time expiry. Additive insert-triggered MVs cannot retract superseded attempts. Compare request-cohort replacement with bounded bucket rebuilds before choosing maintenance machinery.

Rules applied: `schema-pk-prioritize-filters`, `schema-pk-filter-on-orderby`, `schema-partition-lifecycle`, `decision-late-arriving-upserts`, `decision-real-time-preaggregation`. Official foundations: [ReplacingMergeTree](https://clickhouse.com/docs/en/guides/replacing-merge-tree), [primary keys](https://clickhouse.com/docs/best-practices/choosing-a-primary-key), [partitioning](https://clickhouse.com/docs/partitions), [incremental MVs](https://clickhouse.com/docs/materialized-view/incremental-materialized-view).

## Dashboard and Platform MCP

- Extend `server/internal/telemetry/analytics`, its catalog, compiler and runner with semantic metrics, required source parameters, structured dynamic attribute references, minute/auto steps and source planning.
- Decouple dataset retention, permitted span and physical resolution from the existing event/metric distinction and `agent_events` assumptions.
- Return unit, population, hit predicate, numerator/denominator, outcome companions, effective step, time axis, partial/completeness state, definition mix, query time and available freshness information.
- Both surfaces use the same compiled semantics. Live `FINAL` has no historical as-of snapshot: persist values and evidence context for reproducible findings; do not promise replay from an `inserted_at` cutoff.
- Extend `describe_analytics_catalog`, `list_analytics_dimension_values`, `run_analytics_query` and shipped workflows together; add an outcome-oriented session/evidence finder where needed.
- Align `signals_intelligence` entitlement and rollout behavior across discovery and execution. Aggregates use project-read authorization; content drilldown enforces underlying chat access.
- Dashboard supplies useful defaults, long-range month presets, attribute controls and evidence links. Seed linked conversation readings, multiple messages/minutes, known/unknown labels and separate manual runs as their consuming layers land.
- Session findings: exact distinct conversations with hits; share among evaluated conversations; top-session concentration; separately named same-message and same-session co-occurrence with joint-answer denominators; exploratory prior-window changes with comparability markers.

## PR stack and acceptance gates

Create each branch before implementing its concern; migrations get dedicated prerequisite PRs. Each layer includes its focused tests and applicable seed updates.

| Order | Layer                                              | Acceptance gate                                                                                                                                      |
| ----- | -------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1     | `sigint-exploration/message-attributes`            | This PLAN; capture family/provider/model/department on creation; omit unknowns; tenant-safe immutable snapshots; demo labels; evaluator preservation |
| 2     | Replay/lineage schema, then capture and evaluation | Separate schema PR if needed; generation/sequence/original-target lineage; compaction/edit examples cannot manufacture recent activity               |
| 3     | Definition/activation provenance                   | Reuse audit evidence where valid; inspectable evaluated definitions; capture activation facts needed for later census                                |
| 4     | Canonical query and benchmark harness              | Synthetic correctness oracle, representative 26.6 plans/latency/scan costs; agree workload and performance budgets                                   |
| 5     | Shared metric engine                               | Required source scope; predicates/ratios/companions; arbitrary attributes; minute/hour/month/auto; complete series and bounded costs                 |
| 6     | Dashboard and Platform exploration                 | Same semantics, entitlement and permissions; seeded usable charts, discovery and drilldown; MCP skills updated                                       |
| 7     | Cross-session findings                             | Presence, breadth, concentration, both co-occurrence units and guarded comparisons with evidence                                                     |

Layer 1 MCP assessment: creation-event enrichment is internal; no member/admin action or discoverable sigint analytics capability exists yet. Existing analytics tools remain truthful. Query/discovery and evidence parity land with layers 5–7, for authorized project members on both surfaces.

Follow-on scope: measured acceleration/catalogs, saved detectors/notifications and text query syntax. Establish a typed metric representation now. PromQL-inspired semantics are acceptable groundwork; literal compatibility remains undecided. Do not rename event ratios as Prometheus `rate`/`increase`.

## Validation

- Attribute layer: creation paths, null/blank/unknown values, routed vs first-party models, directory changes, missing/deleted/ambiguous identities, cross-tenant isolation, conflict/redelivery behavior and reading/outcome preservation.
- Measurement examples: answer-to-failure replacement, duplicate/out-of-order delivery, multi-label overlap, unknown coverage, ordered levels, replayed generations and message/session co-occurrence.
- Synthetic benchmark: 1k/10k/100k readings per sensor/day, 3/10/50 sensors, two-year windows, varied attribute cardinality, retries with answer flips, historical imports, long sessions and continuous ingestion.
- Queries: 6h/minute, 7d/hour, 90d/day, 24mo/month; threshold/share; exact distinct sessions; arbitrary labels; co-occurrence; bounded session evidence; manual-run partition pruning; concurrent chart requests.
- Record cold/warm p50/p95, rows/bytes/granules, memory and parts. Pin 26.6 settings; use `EXPLAIN indexes = 1` with query-condition cache and streaming skip-index evaluation disabled, plus `EXPLAIN ESTIMATE` verified against actual reads.
- Test `FINAL` settings and compare every result with latest-attempt oracle. Verify month/edge boundaries, zero versus unknown, complete tail buckets, and weighted/distinct reaggregation.
- The ClickHouse 26.6.8.7 matrix covers 1k/10k/100k readings per sensor/day, 3/10/50 sensors, 5–1,000 attribute values, 100–10,000 messages per conversation, and 4/8 concurrent readers. It includes two-year histories, corrections, historical imports, manual runs, evidence, co-occurrence and concurrent duplicate/new ingestion. All returned results matched latest-attempt oracles. The largest two-year query is explicitly refused by the scan budget. Full measurements and reproduction commands live in `server/cmd/sigint-bench/README.md`.
- Relevant implementation skills: Go, PostgreSQL, Pub/Sub, ClickHouse and architecture guidance, demo seed, management API, frontend, Platform MCP, RBAC and PR-stack workflows as each layer touches them.

## Delivered layers

Stack: https://github.com/speakeasy-api/gram/stack/7683

| PR                                                       | Concern                                                                                                   |
| -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| [#7681](https://github.com/speakeasy-api/gram/pull/7681) | Immutable message-time attributes and this plan                                                           |
| [#7682](https://github.com/speakeasy-api/gram/pull/7682) | Dedicated provenance-storage migration prerequisite                                                       |
| [#7684](https://github.com/speakeasy-api/gram/pull/7684) | Original/copy/summary/unknown capture lineage and evaluation exclusions                                   |
| [#7685](https://github.com/speakeasy-api/gram/pull/7685) | Immutable evaluated definitions and activation audit history                                              |
| [#7688](https://github.com/speakeasy-api/gram/pull/7688) | Canonical compiler, shared runner, exact metrics and complete-series budgeting                            |
| [#7691](https://github.com/speakeasy-api/gram/pull/7691) | Dedicated measured source-time index migration                                                            |
| [#7693](https://github.com/speakeasy-api/gram/pull/7693) | Analytics RPC, Platform MCP discovery/query/definition parity and shipped workflow                        |
| [#7696](https://github.com/speakeasy-api/gram/pull/7696) | Explore dashboard, count/share charts, partial markers, linked intraday fixtures and exports              |
| [#7700](https://github.com/speakeasy-api/gram/pull/7700) | Cross-session breadth/concentration, both co-occurrence units, guarded comparisons and permitted evidence |
| [#7707](https://github.com/speakeasy-api/gram/pull/7707) | Expanded correctness/performance matrix and measured workload envelope                                    |

## First-epic decisions and evidence

- New API vocabulary uses plain strings, with advertised values and shared-engine validation.
- Multi-label thresholds are explicit; the dashboard starts at 0.5 and names the rule. Exclusive measurements use the selected answer. Ordered measurements use cumulative probability at/above the selected level.
- Physical reads are canonical at every interval. Initial per-read limits are five seconds, 25 million rows, four billion read bytes and one billion memory bytes. These are engineering limits, not a production latency SLO or a freshness guarantee.
- At 219M retained synthetic readings, six-hour charts measured 88.8 ms warm p95; eight concurrent readers with new ingestion measured 220.8 ms. The largest two-year request exceeds the scan budget and returns an actionable refusal. Output coarsening alone does not reduce canonical work.
- Comparisons retain both windows' values. Comparability requires at least 30 measured answers and five evaluated conversations in each window, one identical known definition, and identical known classifier/model/provider/compiler/input settings. Windows ending within five minutes of query time are marked unsettled. Unknown census coverage remains explicit even when these gates pass.
- RPC and both analytics MCP audiences require the same Signals Intelligence entitlement and exact project access, independently of Explore. Supporting references use batched chat grants plus the existing owner/runtime/external-user/support boundary; opening content checks access again.
- Demo/local seeds include 4,968 original messages in 828 linked conversations across 400 days, dense recent intraday activity, known/missing attributes, failed/draft/not-applicable outcomes and separate manual runs. Repeated local seeding and tenant-isolation checks passed.
- Analytics, chat, Platform MCP and shipped-plugin package checks passed together (3,248 tests), with server lint and dashboard checks. Browser QA covered minute/month and count/share views, definition inspection, conversation drilldown, loading/empty/error recovery, dark mode and 400px layout. Reviewed synthetic screenshots are attached to the UI PRs.

## Follow-on decisions

- What deployment-specific workload and latency/freshness SLO should govern acceleration beyond the demonstrated canonical envelope? Correction-aware hourly acceleration and attribute catalogs remain follow-on work.
- Should a future text syntax target literal PromQL compatibility? Saved detectors, notifications and approximate aggregates remain outside this epic; first-slice distinct counts are exact.
