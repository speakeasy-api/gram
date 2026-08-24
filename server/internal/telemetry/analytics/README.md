# Telemetry analytics architecture spike

This package is a narrow, compilable spike for the two Draft telemetry RFCs. It demonstrates the intended boundaries without changing the authoritative ClickHouse schema, generating migrations, wiring Pub/Sub, or moving an existing endpoint.

The complete example has two parts:

- [`server/clickhouse/spikes/telemetry_v2.sql`](../../../clickhouse/spikes/telemetry_v2.sql) contains executable DDL for the three native signal tables, canonical usage facts, and a refreshable daily usage rollup.
- This package contains a closed semantic catalog, immutable `Query`, mandatory tenant and time constructors, physical-plan selection, and parameterized ClickHouse compilation.

Two additional walkthroughs show how the design is consumed and migrated:

- [`example/main.go`](example/main.go) is a compilable production-style caller covering every supported grain, dimension, measure, filter operator, order direction, physical plan, default, and rejection path.
- [`CURRENT_QUERY_MIGRATION.md`](CURRENT_QUERY_MIGRATION.md) inventories every exported telemetry repository operation and maps it to a semantic query, typed detail search, operational lookup, or command.

## Shape

```text
authenticated caller
       │
       ▼
NewScope + NewTimeRange
       │
       ▼
NewUsageQuery
  dimensions: provider, model, source
  measures: requests, token categories, cost
       │
       ▼
Planner.Compile
       │
       ├── fresh, complete, day-aligned ──► telemetry_usage_daily
       │
       └── within fact retention ─────────► telemetry_usage_facts FINAL
```

Callers name product semantics rather than tables or SQL expressions:

```go
scope, err := analytics.NewScope(organizationID, projectIDs...)
if err != nil {
    return err
}
interval, err := analytics.NewTimeRange(from, to)
if err != nil {
    return err
}

query := analytics.NewUsageQuery(scope, interval).
    AtGrain(analytics.GrainDay).
    GroupBy(analytics.DimensionProvider, analytics.DimensionModel).
    Select(
        analytics.MeasureRequests,
        analytics.MeasureTotalTokens,
        analytics.MeasureTotalCost,
    ).
    Where(analytics.OneOf(analytics.DimensionSource, sources...)).
    OrderBy(analytics.Descending(analytics.MeasureTotalCost)).
    WithLimit(10)

compiled, err := planner.Compile(query)
```

`Query` has no table, expression, join, function, or setting field. The compiler accepts only cataloged dimensions, measures, filters, grains, sorts, and limits. Filter values are Squirrel parameters. A valid query always contains a non-empty authenticated project set and a half-open UTC interval.

## What the DDL demonstrates

The native tables have product-specific grains and stable identities. Frequently filtered fields use native ClickHouse types; variable attributes use `JSON`; source payloads remain compressed escape hatches. The `ORDER BY` prefix follows every supported query's organization, project, and time filters. Daily partitions are bounded by the 90-day TTL and must still be validated against production part counts before becoming authoritative schema.

`telemetry_usage_facts` is one row per logical usage observation, not one row per raw signal. `ReplacingMergeTree(fact_version)` models late attribution and replay without `ALTER UPDATE`. Analytical reads use `FINAL` because background replacement is eventual. Replacement identity is the complete `ORDER BY` tuple, so event timestamps are immutable identity metadata in this example; correcting a timestamp would require a different key design or an explicit retraction protocol.

`telemetry_usage_daily` is a refreshable materialized view over deduplicated facts. It replaces its complete 90-day target every five minutes in this spike. That is safe for corrected facts, but it is not the RFC's final 730-day partition-refresh controller. A production implementation must benchmark full refreshes and either stage and replace complete dirty partitions or prove that full-target refresh is cheap enough.

The spike uses `Decimal(20, 12)` for `total_cost` to make the fixed-precision boundary concrete. This is an explicit assumption, not a billing decision. Billing must confirm source precision before migration DDL is approved.

## Requirement coverage

| RFC requirement | Spike artifact | Status |
| --- | --- | --- |
| Separate native log, span, and metric grains | `telemetry_v2.sql` | Demonstrated, not migrated |
| Stable identities and replacement versions | Native tables and `telemetry_usage_facts` | Demonstrated |
| Typed fields plus dynamic attributes | Native table DDL | Demonstrated |
| Canonical usage fact | Fact DDL and usage semantic catalog | Demonstrated for one domain |
| Simple deduplicated rollup | Refreshable daily MV | Demonstrated for 90 days |
| Mandatory tenant and time scope | `NewScope`, `NewTimeRange`, `NewUsageQuery` | Implemented and tested |
| Closed dimensions and measures | `catalog.go` | Provider, model, source and seven measures |
| Rollup/fact physical planning | `Planner.Compile` | Implemented and tested |
| Parameterized SQL | `compiler.go` | Implemented and tested |
| Pub/Sub, writers, and replay | None | Intentionally skipped |
| Existing endpoint migration and shadow comparison | None | Intentionally skipped |
| Tool-call facts and other datasets | None | Intentionally skipped |
| Raw-query registry and `glint` enforcement | None | Intentionally skipped |
| Production schema and migrations | None | Intentionally skipped while RFCs remain Draft |

## ClickHouse provenance

The schema follows `schema-pk-plan-before-creation`, `schema-pk-prioritize-filters`, `schema-pk-filter-on-orderby`, `schema-types-native-types`, `schema-types-lowcardinality`, `schema-types-avoid-nullable`, `schema-json-when-to-use`, `schema-partition-lifecycle`, and `insert-mutation-avoid-update`.

The refreshable rollup follows `query-mv-refreshable` and `decision-real-time-preaggregation`. Avoiding an incremental additive view over replacement facts is derived from ClickHouse's inserted-block MV semantics and the RFC's at-least-once delivery model. Production writers would still need batching or acknowledged async inserts per `insert-batch-size` and `insert-async-small-batches`.
