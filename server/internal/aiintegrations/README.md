# AI Integrations

This package owns organization-level AI provider configuration and usage sync state. Cursor is the first provider implemented here, but the data model and service naming are intentionally provider-oriented so future integrations can reuse the same configuration and polling primitives.

## Data Model

AI integrations are split across two Postgres tables:

- `ai_integration_configs` stores provider configuration: organization, provider, encrypted API key, enabled flag, soft-delete metadata, and the telemetry project used for usage rows.
- `ai_integration_syncs` stores usage polling state: the completed inclusive query cursor (`poll_watermark_at`), scheduler cursor (`next_poll_after`), and final failure metadata.

Configuration and sync state are separate because provider credentials are user-managed settings, while polling metadata is operational state owned by background workers.

Cursor setup is organization-level. Today each integration attaches to the organization's first-created project automatically. New or replaced API keys start with a one-hour-old query cursor and `next_poll_after = now()` so they are due on the next five-minute polling tick.

## Cursor Usage Polling

Cursor usage metrics come from Cursor's Admin API, not from hooks or OTEL. The background pipeline polls Cursor's hourly usage event endpoint, transforms each event into the shared `telemetry_logs` schema, and writes token/cost data with `gram.hook.source = "cursor"`, `gram.event.source = "api"`, and `gram.resource.urn = "cursor:usage:metrics"`.

The polling workflow is implemented in `internal/background/ai_integration_usage_polling.go`; the Cursor-specific API, mapping, dedupe, and persistence logic lives in `internal/background/activities/poll_cursor_usage_metrics.go`.

```text
Five-minute Temporal Schedule
        |
        v
+-----------------------------------+
| AIIntegrationUsageSyncWorkflow    |
| coordinator                       |
+-----------------------------------+
        |
        | shared endTime = workflow.Now()
        | candidate cutoff = endTime
        | bounded child capacity
        | stable child workflow ID per provider + org
        v
+-----------------------------------------------+
| GetAIIntegrationsCandidates activity          |
|                                               |
| Postgres: ai_integration_syncs                |
| - select next due LIMIT batch                 |
| - return config ID, organization ID, provider |
| - no claiming write or row lock               |
+-----------------------------------------------+
Coordinator starts one bounded batch, waits for it to complete, then fetches more:

+----------------------+     +----------------------+     +----------------------+
| config A             |     | config B             |     | config C             |
| start stable child   |     | start stable child   |     | start stable child   |
+----------+-----------+     +----------+-----------+     +----------+-----------+
           |                            |                            |
           v                            v                            v
+----------------------+     +----------------------+     +----------------------+
| AIIntegrationUsage   |     | AIIntegrationUsage   |     | AIIntegrationUsage   |
| SyncConfigWorkflow   |     | SyncConfigWorkflow   |     | SyncConfigWorkflow   |
+----------+-----------+     +----------+-----------+     +----------+-----------+
           |                            |                            |
           v                            v                            v
+----------------------+     +----------------------+     +----------------------+
| SyncAIIntegration    |     | SyncAIIntegration    |     | SyncAIIntegration    |
| Usage activity       |     | Usage activity       |     | Usage activity       |
|                      |     |                      |     |                      |
| Cursor pages         |     | Cursor pages         |     | Cursor pages         |
| 429 sleep + heartbeat|     | 429 sleep + heartbeat|     | 429 sleep + heartbeat|
| in-memory dedupe     |     | in-memory dedupe     |     | in-memory dedupe     |
| bulk ClickHouse write|     | bulk ClickHouse write|     | bulk ClickHouse write|
| record poll state    |     | record poll state    |     | record poll state    |
+----------+-----------+     +----------+-----------+     +----------+-----------+
           |                            |                            |
           v                            v                            v
+----------------------+     +----------------------+     +----------------------+
| child complete       |     | child complete       |     | child complete       |
| OR activity timeout  |     | OR activity timeout  |     | OR activity timeout  |
+----------+-----------+     +----------+-----------+     +----------+-----------+
           |                            |                            |
           v                            v                            v
+---------------------------------------------------+
| Coordinator                                       |
|                                                   |
| waits for the current child batch to complete     |
| fetches next due LIMIT batch after the batch ends |
+---------------------------------------------------+
```

## Important Invariants

Temporal workflow IDs are the per-org/provider mutex. Each child workflow uses `v1:ai-integration-usage-sync-config:{provider}:{organizationID}`. If another coordinator tries to start the same provider/org while a child is already open, Temporal rejects the duplicate start and the coordinator skips that config for the run.

The coordinator runs every five minutes, while each config is due when `next_poll_after <= runTime`. It starts a bounded batch of child workflows, waits for that batch to complete, then fetches the next due `LIMIT` batch. The stable workflow ID prevents another poll for the same provider/org from starting if a previous child workflow is still open.

Candidate listing is read-only. `ListUsagePollCandidates` returns enabled, non-deleted configs with API keys whose next hourly poll is due, ordered by `(next_poll_after, organization_id, provider)`, limited to the coordinator's child concurrency. Candidate rows include only the config ID, organization ID, and provider; `SyncAIIntegrationUsage` loads and decrypts the full config by ID. The coordinator does not use offset or keyset pagination because each started batch completes and moves out of the due window before the next fetch.

Polling concurrency is primarily enforced by the coordinator's bounded child batch size. `SyncAIIntegrationUsage` is still routed to the dedicated AI integration usage task queue, whose worker sets `MaxConcurrentActivityExecutionSize` as an additional guardrail.

Cursor windows are non-overlapping on success. Cursor includes both request bounds, so each request starts at `poll_watermark_at + 1ms` and ends at the coordinator's shared `endTime`. On success, `poll_watermark_at` advances to `endTime`, `next_poll_after` advances to `endTime + 1h`, and failure metadata is cleared. On the third failed `SyncAIIntegrationUsage` activity attempt, `poll_watermark_at` is left unchanged, `next_poll_after` advances to `endTime + 1h`, and the final error is recorded. A child workflow can wait behind the dedicated activity task queue; while it is still open, later coordinators skip the same provider/org via the stable workflow ID.

Child workflow failures are isolated. The coordinator logs a failed child and continues draining sibling and later candidates instead of failing the whole coordinator run.

ClickHouse and Postgres are not updated atomically. If ClickHouse insert succeeds but the success sync-state update fails before a later retry advances `poll_watermark_at`, the same window can be re-inserted. Each row includes `cursor.event_hash`; dashboard queries that sum polled Cursor rows should dedupe by `(gram_project_id, cursor.event_hash)` first. If the final activity attempt fails before inserting, the failure is logged and only `next_poll_after` advances so that provider/org does not block later work.

Cost fields are intentionally separate. `gen_ai.usage.cost` currently uses `tokenUsage.totalCents / 100`. Cursor's charged amount is also stored as `cursor.charged_cents` and `cursor.charged_usd` so billing semantics can be adjusted later without losing data.

## Adding Providers

Keep shared config and poll-state behavior in this package. Provider-specific API calls and event mapping should stay behind background activities, with the provider value deciding which implementation runs.

When adding a provider:

1. Add the provider constant and validation.
2. Reuse `ai_integration_configs` for credentials and enablement.
3. Reuse `ai_integration_syncs` for query cursors, scheduler state, and failure metadata.
4. Add provider-specific polling inside the activity layer.
5. Emit telemetry with the shared `gen_ai.usage.*` attributes where possible.
6. Store provider-specific fields under a provider namespace, such as `cursor.*`.
