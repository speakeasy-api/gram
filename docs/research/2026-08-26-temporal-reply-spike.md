# Temporal workflow as the enforcement reply channel: spike results

Date: 2026-08-26. Spike for AIS-402, evaluated against the Redis replica-inbox reply path on `vishal/ais-402-enforcement-reply-core` (see `docs/research/2026-08-18-request-reply-recipes.md` and `docs/research/2026-08-20-enforcement-reply-load-test.md` on that branch). Spike code: `server/cmd/tools/temporalreply` on this branch, throwaway.

## Verdict

The Temporal shape works and is pleasant to express: per-lane folding, duplicate-signal dedupe, and the deadline timer are a ~40-line workflow, verified correct in the spike. But it loses the comparison on every measured and structural axis:

- Reply-leg latency is 15x to 35x the Redis inbox at the same local concurrency, on Temporal's best-case backend (in-memory dev server persistence). p99 at 100 concurrent 5-lane scans: 335 ms vs 10.0 ms for Redis.
- One realistic scan costs 26 history events, 6 workflow tasks, and a dozen persistence writes, versus one pipelined `RPUSH`+`EXPIRE` per reply and a shared `LPOP` poll.
- It puts the Temporal cluster and the gram-worker fleet on an interactive single-digit-seconds gate. Worker deploys, task queue backlog, and namespace rate limits all become enforcement-latency events.
- Temporal's actual value, durable execution that survives process death, buys nothing here: the reply's only consumer is an in-memory waiter inside one HTTP request. If the replica dies, the workflow durably completes to nobody. The same restart caveat accepted for the replica inbox applies, minus every cost above.
- The clean-scan question (below) forces a dedicated completion topic anyway, so the design cannot even reuse the shared Finding topic to earn back some simplicity.

The recipes doc scored Temporal as "unproven for this use" from documentation alone. The spike upgrades that to measured: keep the Redis replica inbox.

## What was built

`server/cmd/tools/temporalreply/main.go`, self-contained against a `temporal server start-dev` instance (in-memory persistence, dev-server RPS limits raised out of the way):

- `EnforcementScanWorkflow(ScanInput{ScanID, Lanes, DeadlineMS})`: a signal-channel receive loop folds the first reply per requested lane (unrequested lanes and duplicates dropped), `AwaitWithTimeout` supplies the deadline, result is `Outcome{Replies, Complete, DeadlineHit}`.
- A signal sender in the gram `streams.HandlerFunc` shape, standing in for a Pub/Sub subscriber on a reply-events topic. It uses `SignalWithStartWorkflow` keyed on `enforce-scan-<scan id>`, which makes signal-before-start ordering safe and dedupes duplicate scan starts by workflow ID.
- A caller that starts the workflow (`ExecuteWorkflow`; attaching to an already-started ID is not an error) and blocks on `WorkflowRun.Get`.

Verified behaviors: every lane signal sent twice still completes with exactly one folded reply per lane; a scan missing one lane completes at the deadline with `DeadlineHit` and the partial fold; deadline overshoot beyond the timer was p50 12 ms, max 56 ms.

## Measurements

Same host class as the Redis load test (shared dev box; single-run observations). Two modes: `reply` mirrors the Redis harness (workflows pre-started and result waiters attached, then all lane signals released at once; RTT from release to caller unblock), `full` includes workflow start in the RTT. 1-lane and 5-lane, burst concurrency 1/10/100, 5 s deadline, zero simulated scan latency. Two full runs showed ~20-30% run-to-run variance; one run shown.

| In-flight scans | Lanes | reply p50 | reply p99 | full p50 | full p99 | start ack p50/p99 |
| --------------: | ----: | --------: | --------: | -------: | -------: | ----------------: |
|               1 |     1 |   13.2 ms |   13.2 ms |   8.8 ms |   8.8 ms |      2.0 / 2.0 ms |
|              10 |     1 |   23.5 ms |   39.5 ms |  44.1 ms |  45.7 ms |      3.8 / 5.9 ms |
|             100 |     1 |  114.0 ms |  199.1 ms | 199.9 ms | 262.5 ms |    23.1 / 83.0 ms |
|               1 |     5 |    6.9 ms |    6.9 ms |   6.7 ms |   6.7 ms |      1.3 / 1.3 ms |
|              10 |     5 |   32.5 ms |   36.1 ms |  44.1 ms |  51.1 ms |      3.7 / 4.1 ms |
|             100 |     5 |  244.9 ms |  335.0 ms | 263.2 ms | 352.3 ms |    38.6 / 73.2 ms |

Redis inbox reply-leg comparison points from the 2026-08-20 doc, same semantics (registration excluded, RTT from release):

| In-flight scans | Replies/scan | Redis p50 | Redis p99 | Temporal reply p50 | Ratio (p50) |
| --------------: | -----------: | --------: | --------: | -----------------: | ----------: |
|              10 |            1 |   1.42 ms |   1.43 ms |            23.5 ms |         17x |
|             100 |            1 |   3.17 ms |   5.43 ms |           114.0 ms |         36x |
|              10 |            5 |   2.09 ms |   2.17 ms |            32.5 ms |         16x |
|             100 |            5 |   9.17 ms |   9.98 ms |           244.9 ms |         27x |

Everything completed; no timeouts at any point, and even the worst tail (352 ms full-path p99 at 100 concurrent) fits a 5 s budget on this box. The gap is not "Temporal cannot do it at our tens-per-replica concurrency"; it is that the dev server is Temporal's floor (in-memory persistence, zero network distance, one idle worker), while the Redis numbers already include the production client topology. Production Temporal adds a real persistence store, network hops, a shared namespace, and a contended worker fleet on top of a 25x handicap; production Redis adds one network hop to a cache we already run in-VPC.

## What one scan costs in Temporal terms

Measured from actual histories (`GetWorkflowHistory`, all-event filter):

- Best case, burst: all lane signals land before the first workflow task completes and coalesce into it. 1-lane scan: 7 events, one workflow task. 5-lane: 11 events, still one workflow task (Started, 5x Signaled, TaskScheduled/Started/Completed, TimerStarted, Completed).
- Pre-started waiter (reply mode): 2 workflow tasks; 10 events for 1 lane, 14 for 5 lanes.
- Realistic case, scanners finishing at different times (5 signals 50 ms apart): 26 events and 6 workflow tasks. Each signal that arrives with no workflow task in flight appends its event and schedules a fresh task, so cost approaches 4 events and one task-queue dispatch per lane, plus start/timer/complete.
- Deadline path adds TimerFired and one more workflow task (11 events for the 1-of-2-lanes case).

Each of those event batches is a history-shard persistence transaction (start, one per signal append, one per workflow task completion), plus mutable-state updates and open/close visibility writes: order of 10+ database writes for a realistic 5-lane scan, with the whole history then retained for the namespace retention period. Every enforcement-scanned message becomes durable Temporal state. The Redis equivalent is one pipelined `RPUSH`+`EXPIRE` per reply, self-GCed by a 60 s TTL, and zero writes to any system of record.

Task-queue dispatches: one workflow-task dispatch per signal batch (1 best case, ~L+1 realistic, plus one for a deadline fire). These dispatches are the reason the worker fleet sits on the wait path: folding happens in workflow task execution on gram-worker pollers, so worker deploys, poller saturation from batch workloads on the same cluster, and matching-service latency all land inside the enforcement deadline. Production would also contend with namespace RPS limits (the dev server's had to be raised even for this spike; frontend namespace RPS defaults to 2400) shared with every other Gram workflow.

If the production cluster is Temporal Cloud (the server dials with a namespace-scoped mTLS client cert, which is the Cloud pattern), each start, signal, and timer is a billable action: roughly L+2 actions per scanned message, a linear per-message spend the Redis path does not have.

## Completion-notification path for the blocking caller

`WorkflowRun.Get` long-polls `GetWorkflowExecutionHistory` on the frontend with the close-event filter; the history service parks the poll and answers when the completion event is appended. Measured gap from the workflow's `WorkflowExecutionCompleted` event time to the caller unblocking (same-host clocks): p50 1 to 15 ms across sweeps, max 18.8 ms at 100 concurrent. So the notification hop itself is cheap; the cost lives in everything before it (signal append, workflow task dispatch, task execution, completion write). Note the long poll is one more open frontend request per in-flight scan for the whole wait, where the Redis design holds zero per-waiter connections.

## The clean-scan question

If the signal source is the shared batch Finding topic, a clean scan never signals: findings only exist when something was detected. The workflow's lane can then only resolve by burning the full deadline, and since clean is the overwhelmingly common case, nearly every scan would cost deadline seconds plus a timer fire, and the gate would return its failure-mode default despite the scanner having finished in milliseconds. Findings also cannot express a lane-level ERROR status, which the reply contract requires.

So the signal source must be an explicit per-lane completion event, present even when clean, meaning a dedicated reply/completion topic and subscription. That is exactly the reply leg the Redis design already has, so Temporal replaces only the Redis list with the workflow machinery costed above while keeping all the Pub/Sub plumbing. Routing completions through the batch Finding topic would additionally undo the deliberate noisy-neighbor isolation of enforcement from batch scanning, and the recipes doc's Pub/Sub caveat still binds either way: the request leg's tail latency dominates and no reply-leg choice fixes it.

## What Temporal genuinely wins, for the record

- `SignalWithStartWorkflow` keyed on scan ID removes the start/signal race and dedupes at-least-once scan starts for free.
- Duplicate and stray signals are trivially absorbed in workflow code; the fold logic is small and testable with the SDK test suite.
- A completed workflow is inspectable after the fact (the history is an audit record of which lane said what, when).

None of these need durable execution on the hot path. The audit value, if wanted, is available from the risk findings tables we already write; the dedupe and ordering properties the Redis design covers with the waiter map and scan-scoped URNs.

## Reproduction

```sh
mise exec -- go run ./server/cmd/tools/temporalreply -concurrency 1,10,100 -lanes 1,5 -deadline 5s
```

Starts its own `temporal server start-dev` (needs the `temporal` CLI on PATH; mise provides 1.8.2 / server 1.31.2), runs both sweep modes, prints per-scan history event counts for burst, pre-started, deadline, and spread-signal cases, then the duplicate-signal and missing-lane verifications. SDK 1.47.0, Go 1.26, shared dev box; single-run observations, not controlled benchmarks.
