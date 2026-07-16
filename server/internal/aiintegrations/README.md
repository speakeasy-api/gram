# AI Integrations

This package owns organization-level AI provider configuration and usage sync state. Cursor is the first provider implemented here, but the data model and service naming are intentionally provider-oriented so future integrations can reuse the same configuration and polling primitives.

## Data Model

AI integrations are split across two Postgres tables:

- `ai_integration_configs` stores provider configuration: organization, provider, encrypted API key, enabled flag, soft-delete metadata, and the telemetry project used for usage rows.
- `ai_integration_syncs` stores sync scheduling state: the completed inclusive query cursor (`poll_watermark_at`), scheduler cursor (`next_poll_after`), and final failure metadata.

A config can run several independent sync pipelines, so `ai_integration_syncs` rows are unique per `(ai_integration_config_id, schedule)`. The `schedule` column names the pipeline with provider-style values: each provider's primary sync shares the provider's name (`cursor`, `anthropic_compliance`), and secondary pipelines get their own names (`anthropic_analytics` for the Admin Analytics usage/cost ingest). The `kind` column records how the schedule checkpoints progress: `cursor` rows resume from `last_cursor_id`, `time` rows resume from `poll_watermark_at`. Upserting a config eagerly creates every schedule its provider runs.

Configuration and sync state are separate because provider credentials are user-managed settings, while polling metadata is operational state owned by background workers.

Cursor setup is organization-level. Today each integration attaches to the organization's first-created project automatically. New or replaced API keys start with a one-hour-old query cursor and `next_poll_after = now()` so they are due immediately.

Replacing an API key creates a new config generation: the old active `ai_integration_configs` row is soft-deleted, and a new active row is inserted with its own sync row. Settings-only updates, such as toggling `enabled` without supplying a new key, update the active row in place. Imported telemetry is not deleted when keys are replaced or integrations are deleted; each imported row carries `gram.ai_integration.config_id` so historical usage can be traced back to the config generation that imported it.

## Cursor Usage Polling

Cursor usage metrics come from Cursor's Admin API, not from hooks or OTEL. The background pipeline polls Cursor's usage event endpoint, transforms each event into the shared `telemetry_logs` schema, and writes token/cost data with `gram.event.source = "api"` and `gram.resource.urn = "cursor:usage:metrics"`. Cursor-specific metadata is stored under `cursor.*` attributes.

The polling workflows are implemented in `internal/background/ai_integration_usage_poller.go`. The background activity entrypoint lives in `internal/background/activities/poll_cursor_usage_metrics.go`, while Cursor API paging, event mapping, user hydration, and telemetry writes live in this package.

```text
Five-minute Temporal Schedule
        |
        v
+-----------------------------------+
| AIUsagePollerCoordinatorWorkflow  |
+-----------------------------------+
        |
        | candidate cutoff = workflow.Now()
        | bounded child capacity
        | stable child workflow ID per org + provider
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
| AIUsagePollerWorkflow|     | AIUsagePollerWorkflow|     | AIUsagePollerWorkflow|
+----------+-----------+     +----------+-----------+     +----------+-----------+
           |                            |                            |
           v                            v                            v
+----------------------+     +----------------------+     +----------------------+
| SyncAIIntegration    |     | SyncAIIntegration    |     | SyncAIIntegration    |
| Usage activity       |     | Usage activity       |     | Usage activity       |
|                      |     |                      |     |                      |
| per-activity endTime |     | per-activity endTime |     | per-activity endTime |
| Cursor pages         |     | Cursor pages         |     | Cursor pages         |
| 429 sleep + heartbeat|     | 429 sleep + heartbeat|     | 429 sleep + heartbeat|
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

Temporal workflow IDs are the per-org/provider mutex for scheduled polling. Each scheduled child workflow uses `v1:ai-usage-poller:{organizationID}:{provider}`. If another coordinator tries to start the same provider/org while a child is already open, Temporal rejects the duplicate start and the coordinator skips that config for the run.

New enabled config generations also start a best-effort immediate child workflow after the upsert transaction commits. That workflow uses `v1:ai-usage-poller:config:{configID}`, so a newly replaced key is not blocked by an older in-flight scheduled poll for the same org/provider. If the immediate start fails, the config remains due and the five-minute coordinator is the fallback.

The coordinator runs every five minutes, while each config is due when `next_poll_after <= runTime`. It starts a bounded batch of child workflows, waits for that batch to complete, then fetches the next due `LIMIT` batch. The stable workflow ID prevents another poll for the same provider/org from starting if a previous child workflow is still open.

Candidate listing is read-only. `ListUsagePollCandidates` returns enabled, non-deleted configs with API keys whose next hourly poll is due, ordered by `(next_poll_after, organization_id, provider)`, limited to the coordinator's child concurrency. Candidate rows include only the config ID, organization ID, and provider; `SyncAIIntegrationUsage` loads and decrypts the full config by ID. The coordinator does not use offset or keyset pagination because each started batch completes and moves out of the due window before the next fetch.

Polling concurrency is primarily enforced by the coordinator's bounded child batch size. `SyncAIIntegrationUsage` is still routed to the dedicated AI integration usage task queue, whose worker sets `MaxConcurrentActivityExecutionSize` as an additional guardrail.

Cursor windows are non-overlapping on success. Cursor includes both request bounds, so each request starts at `poll_watermark_at + 1ms` and ends at the activity's `endTime`. On success, `poll_watermark_at` advances to `endTime`, `next_poll_after` advances to `endTime + 1h`, and failure metadata is cleared. On the third failed `SyncAIIntegrationUsage` activity attempt, `poll_watermark_at` is left unchanged, `next_poll_after` advances to that attempt's `endTime + 1h`, and the final error is recorded. A child workflow can wait behind the dedicated activity task queue; while it is still open, later coordinators skip the same provider/org via the stable workflow ID.

Child workflow failures are isolated. The coordinator waits for the current child batch to finish and continues fetching later candidates instead of failing the whole coordinator run.

ClickHouse and Postgres are not updated atomically. If ClickHouse insert succeeds but the success sync-state update fails before a retry advances `poll_watermark_at`, the same window can be re-inserted. Ingestion does not enforce uniqueness; each row includes `cursor.event_hash` so consumers that need exact-once sums can dedupe by `(gram_project_id, cursor.event_hash)`. If the final activity attempt fails before inserting, the failure is recorded and only `next_poll_after` advances so that provider/org does not block later work.

Cost fields are intentionally separate. `gen_ai.usage.cost` currently uses `tokenUsage.totalCents / 100`. Cursor's charged amount is also stored as `cursor.charged_cents` so billing semantics can be adjusted later without losing data.

The API returns poll status derived from sync state, not from a separate column. A config is `pending` before its first success or failure, `success` when `last_poll_success_at` is set without a later failure, and `failed` when failure metadata is present. The dashboard shows this status in the integration card, including the persisted error message for failed polls.

## Adding Providers

Keep shared config and poll-state behavior in this package. Provider-specific API calls and event mapping should stay behind background activities, with the provider value deciding which implementation runs.

When adding a provider:

1. Add the provider constant and validation.
2. Reuse `ai_integration_configs` for credentials and enablement.
3. Reuse `ai_integration_syncs` for query cursors, scheduler state, and failure metadata. Declare the provider's schedules in `syncSchedulesFor` so upserts start every pipeline the provider runs.
4. Add provider-specific polling inside the activity layer.
5. Emit telemetry with the shared `gen_ai.usage.*` attributes where possible.
6. Store provider-specific fields under a provider namespace, such as `cursor.*`.
