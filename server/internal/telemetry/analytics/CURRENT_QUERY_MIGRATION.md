# Current telemetry query migration catalog

This document maps every exported `*repo.Queries` operation under `server/internal/telemetry/repo` to the proposed telemetry architecture. Private SQL builders are covered by the exported operation that owns them. `WithConn` is excluded because it changes the connection used by a repository and does not issue a query.

The important conclusion is that one aggregate DSL should not absorb every database operation. The current repository contains four different kinds of work:

| Kind | Production boundary | Examples |
| --- | --- | --- |
| Aggregate analytics | Closed semantic `Query` and physical planner | totals, time series, breakdowns, unique counts |
| Detail and search | Bounded, typed search specifications | log rows, trace rows, sessions, chats |
| Operational lookup | Dedicated repository method | deduplication, attribution lookup, staging work |
| Command | Dedicated writer | inserts, promotions, score writes, inventory updates |

Only the first category belongs in the aggregate query mechanism demonstrated by this spike. Detail searches can share the same mandatory `Scope`, `TimeRange`, dimension registry, and plan-selection infrastructure, but need cursor and row-projection semantics rather than `GROUP BY` measures. Operational lookups and commands should remain explicit methods.

## Target vocabulary used below

The examples in this document are target-state pseudocode. Only the `usage` subset exists in the compilable spike.

```go
query := telemetry.NewQuery(telemetry.DatasetToolCalls, scope, interval).
    AtGrain(telemetry.GrainDay).
    GroupBy(telemetry.DimensionTarget, telemetry.DimensionTool).
    Select(
        telemetry.MeasureEvents,
        telemetry.MeasureSuccesses,
        telemetry.MeasureFailures,
        telemetry.MeasureFailureRate,
    ).
    Where(
        telemetry.OneOf(telemetry.DimensionTargetType, targetTypes...),
        telemetry.OneOf(telemetry.DimensionUser, users...),
    ).
    OrderBy(telemetry.Descending(telemetry.MeasureEvents)).
    WithLimit(25)
```

The likely datasets are:

| Dataset | Row grain before aggregation | Likely physical sources |
| --- | --- | --- |
| `model_usage` | one canonical model-usage observation | usage facts, hourly/daily usage rollups |
| `tool_calls` | one canonical tool-call outcome | tool-call facts, tool-call rollups |
| `hook_events` | one hook event or normalized hook trace | hook facts, hook rollups |
| `skill_uses` | one skill invocation/version observation | skill facts, skill-version rollups |
| `chat_sessions` | one normalized chat/session | session facts and summaries |
| `employee_activity` | one attributed employee activity | attributed facts and employee rollups |
| `shadow_mcp_usage` | one attributed unproxied MCP call | tool-call facts filtered by target class |
| `chat_analysis` | one chat-analysis verdict | score store and verdict summaries |
| `skill_efficacy` | one scored skill session | efficacy scores and insight rollups |
| `data_flow_edges` | one normalized source-to-destination edge | precomputed edge facts |

The likely reusable dimensions include project, provider, model, source, hook source, account type, user, email, department, division, role, target type, target, tool, status, client, session, chat, skill, skill version, and verdict. Measures include events, requests, successes, failures, failure rate, unique users, unique tools, unique targets, latency, input/output/cache token categories, total tokens, cost, active sessions, and score statistics.

## Model usage, general metrics, and overview

```go
// GetTimeSeriesMetrics or the token portion of GetMetricsSummary.
telemetry.NewQuery(telemetry.DatasetModelUsage, scope, interval).
    AtGrain(bucketGrain).
    Select(
        telemetry.MeasureRequests,
        telemetry.MeasureInputTokens,
        telemetry.MeasureOutputTokens,
        telemetry.MeasureCacheReadInputTokens,
        telemetry.MeasureCacheCreationInputTokens,
        telemetry.MeasureTotalCost,
    )

// GetToolMetricsBreakdown.
telemetry.NewQuery(telemetry.DatasetToolCalls, scope, interval).
    GroupBy(telemetry.DimensionTool).
    Select(telemetry.MeasureEvents, telemetry.MeasureFailures, telemetry.MeasureLatency).
    OrderBy(telemetry.Descending(telemetry.MeasureEvents))
```

| Current operation | Target conversion |
| --- | --- |
| `GetMetricsSummary` | Composite repository call over `model_usage` and `tool_calls`; each subquery is semantic and may select a different rollup. |
| `GetTimeSeriesMetrics` | `model_usage` and/or `tool_calls` with the requested time grain. Split mixed-domain measures rather than rebuilding one raw-log query. |
| `GetToolMetricsBreakdown` | `tool_calls`, grouped by tool and optionally target, selecting events, failures, and latency. |
| `GetOverviewSummary` | Application-level composition of semantic summary queries; no SQL of its own. |
| `GetTopUsers` | `employee_activity` or `tool_calls`, grouped by user, ordered by activity. |
| `GetTopServers` | `tool_calls`, grouped by target/server, ordered by events. |
| `GetLLMClientBreakdown` | `model_usage` or `tool_calls`, grouped by client/hook source; `SessionMode` becomes an explicit dataset choice instead of changing SQL branches. |
| `GetActiveCounts` | Semantic unique-user and unique-target measures over the selected dataset. |

## Canonical model usage and tokens under management

Tokens under management should reuse canonical usage facts but have a separately governed measure definition and population policy. The billing definition must not be an arbitrary expression supplied by a caller.

```go
telemetry.NewQuery(telemetry.DatasetModelUsage, scope, billingWindow).
    AtGrain(telemetry.GrainDay).
    GroupBy(telemetry.DimensionModel).
    Select(telemetry.MeasureTokensUnderManagement).
    Where(telemetry.Exclude(telemetry.DimensionHookSource, gramHostedSources...))
```

| Current operation | Target conversion |
| --- | --- |
| `GetTokensUnderManagementByDay` | `model_usage`, day grain, governed `tokens_under_management` measure. |
| `GetTumWindowTotal` | Same dataset, filters, and governed measure without a time grain. |
| `GetTumBreakdownTotalsByDay` | Same day query selecting governed input, output, cache-creation, and total measures. |
| `GetTumBreakdownDimByDay` | Same measure grouped by one cataloged dimension. Canonical email and multi-valued role behavior belong in dimension definitions, not caller SQL. |
| `GetClaudeTurnUsageByChatIDs` | `model_usage` filtered by chat IDs and grouped by chat/turn; use a dedicated batch-result adapter if the caller requires a map keyed by chat. |
| `GetClaudeToolUsageByChatIDs` | `tool_calls` filtered by chat IDs and grouped by chat/turn/tool. |

## Tool calls, target attribution, and MCP activity

The current tool-usage family already shares a large normalized CTE. In the target design that normalization becomes the `tool_calls` fact writer, and all projections use the same semantic dataset.

```go
base := telemetry.NewQuery(telemetry.DatasetToolCalls, scope, interval).
    Where(
        telemetry.OneOf(telemetry.DimensionTargetType, targetTypes...),
        telemetry.OneOf(telemetry.DimensionTarget, targets...),
        telemetry.OneOf(telemetry.DimensionUser, users...),
        telemetry.OneOf(telemetry.DimensionStatus, statuses...),
    )

totals := base.Select(
    telemetry.MeasureEvents,
    telemetry.MeasureSuccesses,
    telemetry.MeasureFailures,
    telemetry.MeasureFailureRate,
    telemetry.MeasureUniqueTools,
    telemetry.MeasureUniqueUsers,
    telemetry.MeasureUniqueTargets,
)

targets := base.GroupBy(telemetry.DimensionTarget).
    Select(telemetry.MeasureEvents, telemetry.MeasureUniqueTools, telemetry.MeasureFailureRate)
```

| Current operation | Target conversion |
| --- | --- |
| `GetToolUsageSummary` | Composite application call that executes the semantic totals, target, user, time-series, and cross-breakdown queries below. |
| `GetToolUsageTotals` | `tool_calls` totals query. |
| `GetToolUsageTargets` | `tool_calls` grouped by target type/kind/ID/label. |
| `GetToolUsageUsers` | `tool_calls` grouped by normalized user identity. |
| `GetToolUsageTargetTimeSeries` | `tool_calls`, time grain plus target dimensions. |
| `GetToolUsageUserTimeSeries` | `tool_calls`, time grain plus user dimensions. |
| `GetToolUsageUsersByTarget` | `tool_calls` grouped by target and user. |
| `GetToolUsageTargetToolBreakdown` | `tool_calls` grouped by target and tool. |
| `GetMcpServerActivity` | `tool_calls` grouped by MCP target, selecting total, recent, and last-seen measures. |
| `GetToolUsageFilterOptions` | Dimension-value discovery over `tool_calls`; use cataloged dimensions and the same base scope/window. |
| `ListToolUsageTraces` | Typed `TraceSearchSpec` over tool-call/trace facts with cursor pagination; not an aggregate query. |
| `GetUnproxiedMcpServerUsageTimeSeries` | `tool_calls` filtered to unproxied/shadow targets, grouped by time and server. |
| `GetUnproxiedMcpServerToolUsage` | Same filter, grouped by server and tool with semantic pagination/order. |
| `GetUnproxiedMcpServerUserUsage` | Same filter, grouped by server and user. |
| `GetUnproxiedMcpServerClientUsage` | Same filter, grouped by server and client. |
| `ListShadowMCPInventoryUsage` | `shadow_mcp_usage` or filtered `tool_calls`, grouped by discovered server. |
| `ListShadowMCPInventoryUsers` | Same dataset grouped by discovered server and user. |

## Hooks and skills

Hooks and skills share several filters but have different product grains. They should be two datasets backed by facts written from native log/span records, rather than repeated `trace_summaries` interpretation.

```go
telemetry.NewQuery(telemetry.DatasetHookEvents, scope, interval).
    AtGrain(bucketGrain).
    GroupBy(telemetry.DimensionHookSource, telemetry.DimensionStatus).
    Select(telemetry.MeasureEvents, telemetry.MeasureUniqueUsers)

telemetry.NewQuery(telemetry.DatasetSkillUses, scope, interval).
    GroupBy(telemetry.DimensionSkill, telemetry.DimensionSkillVersion).
    Select(telemetry.MeasureEvents, telemetry.MeasureActiveSessions)
```

| Current operation | Target conversion |
| --- | --- |
| `GetHooksSummary` | `hook_events` grouped by server/source, selecting events, users, sessions, and outcomes. |
| `GetHooksSessionCount` | `hook_events` or `chat_sessions`, selecting unique sessions with hook filters. |
| `GetHooksUserSummary` | `hook_events` grouped by user. |
| `GetHooksBreakdown` | `hook_events` grouped by the requested cataloged hook dimension. |
| `GetHooksTimeSeries` | `hook_events` with a time grain. |
| `ListHooksTraces` | Typed `TraceSearchSpec` filtered to hook traces. |
| `ListRecentHookEventsForOnboarding` | Typed `HookEventSearchSpec`; this is recent-row retrieval, not aggregation. |
| `CountRecentHookEventsForOnboarding` | `hook_events` selecting the event-count measure with the same scope/window as the search. |
| `GetSkillsSummary` | `skill_uses` grouped by skill, selecting invocations, sessions, and users. |
| `GetSkillBreakdown` | `skill_uses` grouped by the requested cataloged dimension. |
| `GetSkillTimeSeries` | `skill_uses` with a time grain. |
| `QuerySkillVersionMetricsTable` | `skill_uses` grouped by skill version and requested cataloged dimensions. |
| `QuerySkillVersionMetricsTimeseries` | Same dataset with a time grain. |

## Chats and sessions

Row-oriented chat/session pages need a bounded search specification. Aggregates over those entities use semantic datasets.

```go
search := telemetry.NewSessionSearch(scope, interval).
    Where(telemetry.OneOf(telemetry.DimensionUser, users...)).
    OrderBy(telemetry.DescendingTime()).
    After(cursor).
    WithLimit(50)

summary := telemetry.NewQuery(telemetry.DatasetChatSessions, scope, interval).
    GroupBy(telemetry.DimensionUser).
    Select(telemetry.MeasureSessions, telemetry.MeasureTotalTokens)
```

| Current operation | Target conversion |
| --- | --- |
| `ListChats` | Typed `ChatSearchSpec` over chat/session summaries. |
| `GetChatMetricsByIDs` | Dedicated authorized batch lookup over chat/session facts, or semantic query filtered by bounded chat IDs. |
| `ListSessions` | Typed `SessionSearchSpec` with cataloged filters and cursor ordering. |
| `GetChatSessionFactsByChatIDs` | Dedicated authorized batch lookup; it serves enrichment rather than interactive analytics. |
| `GetChatMetricsSummaryByIDs` | Dedicated batch aggregate over bounded chat IDs, implemented through the `chat_sessions` dataset where practical. |

## Users, employee analytics, dimensions, and data flow

```go
telemetry.NewQuery(telemetry.DatasetEmployeeActivity, scope, interval).
    GroupBy(telemetry.DimensionDepartment, telemetry.DimensionUser).
    Select(telemetry.MeasureEvents, telemetry.MeasureTotalTokens, telemetry.MeasureUniqueTools)
```

| Current operation | Target conversion |
| --- | --- |
| `SearchUsers` | Typed employee/user search over an employee-activity rollup. |
| `SearchEmployeeAgentUsage` | Same employee search specification with agent-usage measures. |
| `GetUserMetricsSummary` | `employee_activity` filtered by user, selecting usage and tool-call measures. |
| `GetEmployeeDataFlowGraph` | `data_flow_edges` grouped by source/destination/type; do not reconstruct graph edges from raw logs at read time. |
| `ListFilterOptions` | Catalog-driven dimension-value discovery against the selected dataset and window. |
| `ListAttributeKeys` | Catalog metadata plus an explicit dynamic-attribute registry; not a raw query escape hatch. |
| `ListEmaillessIdentities` | Dedicated data-quality lookup. It is operational and should not enter the analytics DSL. |

## Existing generic attribute analytics

The current generic attribute APIs are the closest predecessor to the semantic catalog. Migration should preserve their public dimension IDs while moving SQL expressions and compatible sources into dataset definitions.

| Current operation | Target conversion |
| --- | --- |
| `QueryAttributeMetricsTable` | Compatibility adapter from the existing request into a cataloged dataset query with dimensions and measures. Reject paths not registered for that dataset. |
| `QueryAttributeMetricsTimeseries` | Same adapter with a time grain. |

The adapter should be temporary. New product code should construct semantic query values directly.

## Skill efficacy and chat analysis

| Current operation | Target conversion |
| --- | --- |
| `QuerySkillInsights` | `skill_efficacy`, grouped by skill/version/time and selecting score, success, and sample-count measures. |
| `ListSkillEfficacyScoreSessions` | Typed scored-session search; row retrieval rather than an aggregate query. |
| `GetChatAnalysisVerdictsByChatIDs` | Dedicated authorized batch lookup by bounded chat IDs. |
| `ListChatAnalysisVerdicts` | Typed verdict search with cataloged verdict/user/time filters. |

## Trace and log detail

Detail endpoints intentionally retain a search-oriented repository. They should still share authenticated scope construction, bounded time ranges, registered filters, plan selection, and parameterization.

```go
telemetry.NewLogSearch(scope, interval).
    Where(telemetry.Equals(telemetry.DimensionTraceID, traceID)).
    SearchText(searchText).
    After(cursor).
    WithLimit(100)

telemetry.NewTraceSearch(scope, interval).
    Where(telemetry.OneOf(telemetry.DimensionStatus, statuses...)).
    After(cursor).
    WithLimit(50)
```

| Current operation | Target conversion |
| --- | --- |
| `ListTelemetryLogs` | Typed `LogSearchSpec`; raw/native log detail is a legitimate raw-table read. |
| `ListToolTraces` | Typed `TraceSearchSpec` backed by trace summaries/facts. |

## Shadow MCP inventory state

Inventory URL records are mutable product state, not time-series analytics. They should remain behind a dedicated repository even if ClickHouse continues to store them.

| Current operation | Target conversion |
| --- | --- |
| `GetShadowMCPInventoryURL` | Dedicated inventory lookup. |
| `ListExistingShadowMCPInventoryURLs` | Dedicated bounded existence lookup. |
| `ListShadowMCPInventoryURLsByCanonicalURLs` | Dedicated inventory lookup. |
| `ListShadowMCPInventoryURLsBySlugHash` | Dedicated inventory lookup. |
| `ListShadowMCPInventoryURLs` | Typed inventory search/read model. |

## Ingestion, staging, attribution, and diagnostics

These operations are deliberately outside the query DSL. Their narrow method names are useful governance because they describe the exact mutation or operational lookup being performed.

| Current operation | Target conversion |
| --- | --- |
| `InsertTelemetryLog`, `InsertTelemetryLogs`, `InsertTelemetryLogsSync` | Native-signal writer commands with batching and acknowledged async-insert policy. |
| `InsertTelemetryLogsStaging` | Staging writer command. |
| `InsertPromotedTelemetryLogs` | Promotion command that writes validated native rows. |
| `DeleteStagedTelemetryLogs` | Explicit staging cleanup command. |
| `ListStagedTelemetryLogs` | Operational promotion work lookup. |
| `ListStagedTelemetryProjects` | Operational promotion work lookup. |
| `ListExistingTelemetryLogIDs` | Operational ingestion deduplication lookup. |
| `ListIngestedEventHashes` | Operational event deduplication lookup. |
| `LookupMCPProvenanceByToolCallID` | Operational attribution lookup used by the fact writer. |
| `ReplaceIdentityMap` | Identity-map writer command. |
| `ListLiteLLMTrafficDiagnostics` | Explicit diagnostic query, isolated from customer-facing analytics. |
| `InsertSkillSessionVersions` | Skill-session/version fact writer command. |
| `InsertSkillEfficacyScores` | Score writer command. |
| `ListExistingSkillEfficacyScoreIDs` | Operational score-deduplication lookup. |
| `InsertChatAnalysisScores` | Chat-analysis writer command. |
| `ListExistingChatAnalysisScoreIDs` | Operational score-deduplication lookup. |
| `UpsertShadowMCPInventoryURLs` | Inventory state command. |
| `UpdateShadowMCPInventoryURLNameOverride` | Inventory state command. |

## Migration order

The safest migration sequence follows dependencies rather than file order:

1. Introduce native logs, spans, and metric points while preserving current writes.
2. Materialize canonical `model_usage`, `tool_calls`, `hook_events`, `skill_uses`, and `chat_sessions` facts in parallel with current views.
3. Add semantic datasets and parity tests for one query family at a time.
4. Move summaries and breakdowns first because they benefit most from governed rollups.
5. Add typed trace/log/session search specifications without forcing them into aggregate semantics.
6. Leave ingestion, staging, stateful inventory, diagnostics, and bounded lookups as explicit repository methods.
7. Remove old query implementations only after shadow comparisons establish row and measure parity.

This separation is also the future-sprawl rule: a new dashboard aggregation must register a dataset, dimensions, measures, and compatible plans. A new detail page must use a typed search specification. Operational commands and lookups remain narrow methods and do not gain a generic SQL escape hatch.
