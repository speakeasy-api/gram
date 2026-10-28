# End-to-end OpenClaw hooks monitor

DNO-1425. Proposed scope; this PR does not deploy a schedule or send alerts.

## Success condition

A fresh real OpenClaw turn, using the observability plugin and a dedicated agent
key, must appear in Agent Sessions under that agent and contain both the exact
synthetic user message and the expected harmless tool invocation. An installer
PASS, accepted ingest response, or pre-existing session is insufficient.

The current dashboard calls `useListChats` with `agentId` in
`client/dashboard/src/pages/identities/useIdentityQueries.ts`. The corresponding
`listChats` and paginated `loadChat` contracts are in
`server/design/chat/design.go`. Use these authorized read paths for assertions.
`agents.listSessions` is credential/session management, not the captured
conversation list; querying it would test the wrong subsystem.

## Smallest useful probe

1. Acquire a non-overlapping run lease. Create an ephemeral OpenClaw profile and
   unique synthetic run marker. Record monotonic start time and a wall-clock query
   window, allowing small clock skew.
2. Install the pinned plugin through the supported installer path. Use a dedicated
   synthetic agent and least-privilege credential. Clear conflicting key aliases
   and cached sign-ins so a developer credential cannot make a broken agent path
   pass. Never print installer codes or keys.
3. Start a real supported model turn with the marker in its user message and ask
   it to invoke an isolated echo tool with that marker. The tool returns only
   synthetic data and has no network or destructive capabilities.
4. Observe the runtime's actual tool invocation. A model response claiming it ran
   a tool is not evidence. Fail separately if no invocation occurs.
5. Poll captured chats through an independent read credential, filtered by exact
   agent and the run window. Match the unique marker, then load the conversation
   with pagination until the matching user message and expected tool call are
   found in the same run. Verify captured attribution against the expected agent
   identifier; the displayed name alone is not a stable identity assertion.
6. Emit a small structured result: outcome, failure stage, elapsed time, pinned
   versions, and a private session link when available. Remove the ephemeral
   profile and release the lease on every exit path.

The read credential must be separate from the hooks credential. Otherwise adding
read permission to make the assertion work could hide the exact regression that
hooks-only agent credentials are intended to detect.

## Proposed bounds and failure classes

Start with a five-minute total deadline, a two-minute model-turn timeout, and
bounded polling with backoff capped at ten seconds. These are initial tuning
values, not measured service guarantees. Cap pages, response sizes, tool calls,
tokens, and concurrency; record a timeout as failure instead of polling forever.

| Stage       | Meaning                                                                  |
| ----------- | ------------------------------------------------------------------------ |
| Setup       | Installer, dependency, plugin loading, or credential setup failed        |
| Runtime     | Model authentication, turn execution, or required tool invocation failed |
| Ingest      | Runtime ran but no correlated captured session arrived before deadline   |
| Attribution | Session exists but is associated with the wrong or missing agent         |
| Transcript  | Session lacks the matching user message or expected tool call            |
| Readback    | Observer authorization or query failed; capture health is unknown        |

Preserve failure stage rather than mapping every failure to an ingestion outage.
Do not dump transcripts, environment variables, or raw authorization errors into
alerts. Internal diagnostic artifacts need restricted access and retention.

## Schedule and alert rollout

First ship a locally runnable probe with machine-readable output and demonstrate
one passing run plus deliberately disabled hooks, missing user-message capture,
wrong attribution, and denied readback. Then choose the runner, secret store,
model budget, schedule, responsible team, and existing on-call destination.

A proposed hourly cadence is about 720 model turns per 30-day month per
configuration. Actual spend is that count times the bounded token/tool cost;
measure a baseline before enabling it. Run one supported configuration initially,
then add OS/harness variants only where they catch distinct failures.

Alert after two consecutive failed completed runs, with a recovery notification
after a successful run. Also detect a missing run heartbeat: a dead scheduler
cannot report its own probe failure. Deduplicate incidents by configuration and
stage. Thresholds and ownership require an explicit operational decision before
deployment.

## Dependencies and limits

PR #8088 describes the hooks self-check, but directs review to its successor
#8108. Its description also names #8109 and publication of a compatible hooks
binary as dependencies. Pin the actual released artifacts used by the monitor;
an unreleased local build can validate implementation but cannot certify the
published install path.

The probe must invoke a real OpenClaw runtime. Directly posting fabricated hook
events can test ingestion in isolation, but it cannot cover missing unzip,
installation failures, shim failures, or harness-specific capture regressions.
The initial scope produces a reviewable probe and runbook; production scheduling,
credentials, model billing, and alert routing are a separate rollout.
