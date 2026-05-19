# AI Integrations

This package owns organization-level AI provider configuration and usage sync state. Cursor is the first provider implemented here, but the data model and service naming are intentionally provider-oriented so future integrations can reuse the same configuration and polling primitives.

## Data Model

AI integrations are split across two Postgres tables:

- `ai_integration_configs` stores provider configuration: organization, provider, telemetry project, encrypted API key, enabled flag, and soft-delete metadata.
- `ai_integration_syncs` stores usage polling state: the completed inclusive watermark (`last_polled_at`) plus the short-lived lease fields (`lease_owner`, `lease_expires_at`).

Configuration and sync state are separate because provider credentials are user-managed settings, while polling metadata is operational state owned by background workers.

## Cursor Usage Polling

Cursor usage metrics come from Cursor's Admin API, not from hooks or OTEL. The background pipeline polls Cursor's hourly usage event endpoint, transforms each event into the shared `telemetry_logs` schema, and writes token/cost data with `gram.hook.source = "cursor"` and `gram.event.source = "polling"`.

The polling workflow is implemented in `internal/background/ai_integration_usage_polling.go`; the Cursor-specific API, mapping, dedupe, and persistence logic lives in `internal/background/activities/poll_cursor_usage_metrics.go`.

```text
Hourly Temporal Schedule
        |
        v
+-----------------------------------+
| AIIntegrationUsageSyncWorkflow    |
| coordinator                       |
+-----------------------------------+
        |
        | shared endTime = workflow.Now()
        | leaseOwner = workflowID/runID
        v
+-----------------------------------------------+
| ClaimAIIntegrationUsagePolls activity         |
|                                               |
| Postgres: ai_integration_syncs                |
| - select eligible configs                     |
| - FOR UPDATE SKIP LOCKED                      |
| - set lease_owner / lease_expires_at          |
+-----------------------------------------------+
Coordinator rolling pool, max N children:

+----------------------+     +----------------------+     +----------------------+
| config A             |     | config B             |     | config C             |
| claim config         |     | claim config         |     | claim config         |
| start child + timer  |     | start child + timer  |     | start child + timer  |
+----------+-----------+     +----------+-----------+     +----------+-----------+
           |                            |                            |
           v                            v                            v
+----------------------+     +----------------------+     +----------------------+
| AIIntegrationUsage   |     | AIIntegrationUsage   |     | AIIntegrationUsage   |
| PollWorkflow         |     | PollWorkflow         |     | PollWorkflow         |
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
| update watermark     |     | update watermark     |     | update watermark     |
+----------+-----------+     +----------+-----------+     +----------+-----------+
           |                            |                            |
           v                            v                            v
+----------------------+     +----------------------+     +----------------------+
| child complete       |     | child complete       |     | child complete       |
| OR timer fires       |     | OR timer fires       |     | OR timer fires       |
+----------+-----------+     +----------+-----------+     +----------+-----------+
           |                            |                            |
           v                            v                            v
+---------------------------------------------------+
| Coordinator                                       |
|                                                   |
| if timer fired:                                   |
|   cancel child with WaitForCancellation: true     |
|   wait until child/activity observes cancellation |
|                                                   |
| then:                                             |
|   ReleaseAIIntegrationUsagePollLease              |
|   start next claimed config                       |
+---------------------------------------------------+
```

## Important Invariants

Claiming must be short-lived and transactional. `ClaimUsagePolls` uses `FOR UPDATE SKIP LOCKED` so multiple workers can safely run without claiming the same config. It only claims enabled, non-deleted configs with API keys, a non-empty poll window, and no active lease.

Leases are recovery guards, not long-running locks. The coordinator claims one config only when a child workflow slot is available, starts that child immediately, and releases the lease only after the child completes or cancellation has been acknowledged.

Child cancellation must wait. The coordinator uses `WaitForCancellation: true` when cancelling a child that exceeds its runtime timer. This prevents the coordinator from releasing the lease while the child activity is still in a Cursor request, rate-limit sleep, ClickHouse write, or watermark update.

Cursor windows are non-overlapping. Cursor includes both request bounds, so each request starts at `last_polled_at + 1ms` and ends at the coordinator's shared `endTime`. The watermark is only advanced to `endTime` after the bulk ClickHouse write succeeds.

ClickHouse and Postgres are not updated atomically. The pipeline is at-least-once across the ClickHouse insert and Postgres watermark update. Each row includes `cursor.event_hash` so dashboard queries can dedupe polled Cursor rows before summing if needed.

Cost fields are intentionally separate. `gen_ai.usage.cost` currently uses `tokenUsage.totalCents / 100`. Cursor's charged amount is also stored as `cursor.charged_cents` and `cursor.charged_usd` so billing semantics can be adjusted later without losing data.

## Adding Providers

Keep shared config and lease behavior in this package. Provider-specific API calls and event mapping should stay behind background activities, with the provider value deciding which implementation runs.

When adding a provider:

1. Add the provider constant and validation.
2. Reuse `ai_integration_configs` for credentials and enablement.
3. Reuse `ai_integration_syncs` for watermarks and leases.
4. Add provider-specific polling inside the activity layer.
5. Emit telemetry with the shared `gen_ai.usage.*` attributes where possible.
6. Store provider-specific fields under a provider namespace, such as `cursor.*`.
