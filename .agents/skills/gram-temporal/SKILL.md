---
name: gram-temporal
description: Use when adding or changing background work in gram — a Temporal workflow, activity, schedule, signal, or ContinueAsNew loop; anything reacting to chat messages, tool calls, MCP requests, telemetry rows, or DB writes; anything that fans out per project/org/user, polls, sweeps, or runs on a timer; when estimating or reviewing Temporal Cloud cost. Triggers: "workflow", "activity", "schedule", "cron", "poll", "sweep", "coordinator", "SignalWithStart", "ThrottledSignaler", "MessageObserver", "per message", "per tool call", "hot path", "registerSchedules".
metadata:
  relevant_files:
    - "server/internal/background/**/*.go"
    - "server/internal/outbox/*.go"
    - "server/internal/chat/message_store.go"
    - "server/internal/gateway/*.go"
    - "server/internal/mcp/**/*.go"
    - "server/internal/scanners/publish.go"
    - "server/internal/streams/*.go"
    - "server/cmd/gram/streams.go"
    - "server/cmd/gram/worker.go"
---

# Temporal in gram: what it costs and when not to use it

Temporal Cloud bills per **action**: every workflow start (ContinueAsNew and child starts included), every activity attempt (retries count), every timer, every signal. Workflow tasks are free. A no-op activity costs the same as five minutes of work. List price is $25–50 per million actions, so cost is set by **how many things you schedule**, not how much work they do.

Core rule: **Temporal is for durable, multi-step orchestration started once per user or system action. Anything that scales with messages, tool calls, MCP requests, rows, or entities goes through the transactional outbox or a Pub/Sub publisher and a `gram streams` handler. Anything on a timer costs `ticks × activities × namespaces` and must state that number in its PR.**

`server/internal/scanners/publish.go` (per-tool-call scanning over Pub/Sub) is the shape to copy. The offenders listed below grew the bill ~6× in two months; migrate them, do not copy them.

## Quick reference

| Work shape                                                                                             | Do this                                                                                                                                                                                                                      | Not this                                                                   |
| ------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| React to a DB write (chat message, telemetry, config change)                                           | `outbox.Publish` in the same tx → topic → `streams.BatchHandlerFunc` in `server/cmd/gram/streams.go`                                                                                                                         | `chat.MessageObserver` → `ThrottledSignaler` → `SignalWithStart` per write |
| Per tool call / per MCP request (scan, score, enrich)                                                  | A check with no network or LLM call runs inline in the handler with nothing async at all. Otherwise publish from the request path as `scanners/publish.go` does and handle in a batch handler                                | any Temporal start, signal, or activity in the request path                |
| Heavy per-event analysis (LLM judge, risk scan)                                                        | Pub/Sub batch handler; if a step must be durable, one workflow per **batch** per project (≥ 5 min or ≥ 100 events)                                                                                                           | one workflow run per event or per 30 s window                              |
| Fan-out over every project / org / user                                                                | One SQL activity returning only rows that changed (`updated_at > processed_at`, version column), then act on those; one activity per changed row only while that count stays in the tens, otherwise loop inside one activity | `ExecuteActivity` inside `for _, p := range projects` on a timer           |
| Periodic sweep                                                                                         | `ScheduleIntervalSpec{Every: time.Hour}` as a safety net behind a write-path event; ≥ 5 min only when the sweep is the sole mechanism; ≤ 3 activities per tick                                                               | schedules under 5 minutes; a faster schedule to meet a latency target      |
| Drain / relay loop                                                                                     | Wait on a signal, or `workflow.Sleep(≥ 60s)` when idle; exit after N iterations and let the watchdog schedule restart it                                                                                                     | `workflow.Sleep(5*time.Second)` forever                                    |
| Multi-step durable job per user action (publish to GitHub, register a custom domain, lifecycle emails) | Temporal workflow, `ExecuteWorkflow` once                                                                                                                                                                                    | —                                                                          |

## Cost line (required in every PR touching `server/internal/background/`)

```
Temporal actions/month ≈ starts + activities (+retries) + timers + signals + child starts,
per namespace (prod and dev; PR previews run in the dev namespace and each registers its own copy of every schedule).
Scales with: fixed | projects | orgs | messages | tool calls.
```

`Temporal actions/month: 0 (outbox → batch handler, no Temporal)` is a valid and common answer. Rules of thumb, per namespace:

- A 10 s schedule is 260k starts/month before it does any work. 1 min: 43k. 1 h: 720.
- One activity per project per 10 s tick at 110 projects: ~29M actions/month.
- A loop with one activity and a 5 s timer: ~1M actions/month, forever, even idle.
- A per-project coordinator signalled on every chat write through a 30 s `ThrottledSignaler`, measured in prod: **1.0 action per message** (median 4 messages per run, 14% of runs fetch nothing). At 250k messages/day that is 7.5M actions/month per coordinator. Do not estimate 0.05 per message "amortised over the batch": the throttle fires on the leading edge and is per pod, so batches stay small.

Anything over 1M actions/month needs a sentence saying why.

## Patterns

### Default: outbox event → Pub/Sub batch handler

```go
// Inside the service transaction that writes the row. The outbox row commits
// with the write, so this is transactional: no dual-write problem.
// Proto is any message declaring a (gcp.pubsub.v1.topic) option; copy the
// analysis/analyzer pair in infra/proto/gram/risk/v1/custom_rules_analysis.proto
// and custom_rules_analyzer.proto for a new event.
if _, err := outbox.Publish(ctx, tx, orgID, outbox.Message{Proto: event}); err != nil {
    return fmt.Errorf("enqueue event: %w", err)
}
```

Register the consumer with `mustReceiveBatchWithResult(...)` in `server/cmd/gram/streams.go` with `gcp.BatchReceiveSettings{MaxMessages: 1000, MaxBytes: 10 * constants.MiB, MaxLatency: time.Second}`. **REQUIRED SUB-SKILL:** `gram-pubsub`. Do the work inline in the handler when it is under a second. Dashboard/analytics results go to ClickHouse through a `*CHWriter` (`server/internal/metering/ch_writer.go`); authoritative state stays in Postgres. If a step must be durable, start one workflow per batch, never per event.

`chat.MessageObserver` (`server/internal/chat/message_store.go`) fires after commit with only the project id. Do not attach new work to it; publish from inside the write transaction.

### If a per-project coordinator is unavoidable, debounce inside the workflow

A per-project coordinator is unavoidable only when the reacting work cannot be expressed as one SQL query plus one activity across every affected tenant (it needs per-project workflow state or per-project retries). A digest or notification that one query can assemble for all tenants is a global sweep: a flat ≥ 5 min schedule with one query-and-send activity, no per-project signalling.

`ThrottledSignaler` lives in every API and worker pod, so the signal rate is `replicas × (leading + trailing per window)`. Two changes get from the measured 1.0 action per event to ≤ 0.1. First, dedupe at the write site: signal only on the transition from "nothing pending" to "something pending" (a `WHERE processed_at IS NULL` guard or partial unique index), not on every event. Second, put the delay in the workflow: on the first signal `workflow.Sleep(5 * time.Minute)`, then fetch everything pending in one activity, wrapped with `background.Debounce` (`debounced.go`) so signals arriving mid-run coalesce into one follow-up run. The `SignalWithStart` call lives in a `streams` batch handler or a scheduled sweep, never in the request path.

### Sweeps

One activity returns only the rows that changed (SQL), fan out over that. Cadence ≥ 1 hour when the sweep is a safety net behind a write-path event, ≥ 5 minutes when it is the sole mechanism. A minutes-scale latency target on per-entity events is met with the debounced signal pattern above, never with a shorter interval.

### Schedules

- Scope the ID by task queue: `fmt.Sprintf("v1:%s:%s", name, temporalEnv.Queue())`. PR previews share the dev namespace; an unscoped ID gets re-pointed by each preview's `Update` and deleted by the preview sweeper, orphaning the loop it tracked.
- `Create`, then on `ErrScheduleAlreadyRunning` do nothing unless the spec changed. Never `Update` a shared schedule from a preview worker.
- Every `ContinueAsNew` loop must exit on its own (max iterations or wall-clock) so a lost schedule cannot leave an immortal workflow behind.

### Existing offenders (migrate when you touch them)

`PluginGeneratorRolloutWorkflow` (10 s schedule × activity per project → hourly, SQL change detection, outbox event on config change). `RiskAnalysisCoordinatorWorkflow`, `ChatAnalysisCoordinatorWorkflow`, `SkillEfficacyCoordinatorWorkflow` (`SignalWithStart` per chat write, three times → one coordinator with in-workflow debounce, or a batch handler). `PublishOutboxWorkflow` (5 s idle timer, unscoped ID, immortal loop → 60 s idle, scoped ID, bounded lifetime). `SpendRuleEvaluationWorkflow`, `SessionQuarantineReassertWorkflow` (30 s schedules → 5 min).

## Rationalizations seen in real designs

| Excuse                                                                 | Reality                                                                                      |
| ---------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| "Gram already solves this trigger point this way three times"          | Those three are the incident. Reuse the outbox, not the coordinator.                         |
| "The activity is cheap / short-circuits when unchanged"                | Billing counts the start, not the work.                                                      |
| "Signal-on-commit is not transactional, so poll"                       | `outbox.Publish` inside the transaction is transactional. Poll only as an hourly safety net. |
| "Zero idle cost, it only runs when signalled, the throttle batches it" | Per-run cost is the problem, and the measured median batch is 4 messages.                    |

## Red flags in a diff

- `time.Second` inside a `ScheduleIntervalSpec`
- `ExecuteActivity` inside a `for ... range` over projects, orgs, or users
- `SignalWithStart`, `ExecuteWorkflow`, or `NewThrottledSignaler` in a request path, a `MessageObserver`, or a tool-call handler (from a `streams` batch handler or a scheduled sweep is fine)
- `workflow.Sleep` under 30 s inside `for {}`
- A schedule ID that does not include the task queue
- A PR touching `server/internal/background/` with no `Temporal actions/month` line
