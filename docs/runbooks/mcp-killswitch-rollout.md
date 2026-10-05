# MCP Killswitch Rollout

This runbook gates the authenticated MCP tool-call Killswitch rollout. Do not
enable customer prescriptions until every serving instance has the approved
hosted and private checkpoint build.

## Coverage boundary

This rollout covers `tools/call` for an authoritative, active organization user
and a canonical organization-owned MCP server on these surfaces:

- hosted MCP dispatch;
- private remote or tunnel proxy forwarding.

It does not cover anonymous sessions, API keys, assistants, chat-session end
users, inactive users, unattributed requests, legacy toolset-only routes, meta
or platform MCP, direct internal calls, or MCP methods other than `tools/call`.
Do not describe this rollout as controlling chat, hooks, model inference,
assistant work, or all AI activity.

## Server-side modes

Two locally evaluated PostHog flags form the backend gate. Target them by the
organization distinct ID; do not put concrete organization identifiers in this
repository or rollout tickets.

| Mode    | `mcp-killswitch-shadow` | `mcp-killswitch-enforce` | Serving behavior                                | Management behavior                                 |
| ------- | ----------------------- | ------------------------ | ----------------------------------------------- | --------------------------------------------------- |
| Off     | off                     | off                      | Skip derivation and evaluation                  | Create and edit unavailable; lift remains available |
| Shadow  | on                      | off                      | Evaluate and emit metrics, but always continue  | Create and edit unavailable; lift remains available |
| Enforce | either                  | on                       | Apply matched denial and registered fail policy | Create and edit available                           |

Management behavior in the table applies to fresh mutations. A completed
operation replay bypasses the current gate and returns its stored result in every
mode.

Enforce takes precedence if both flags are on. Missing or disabled flags resolve
to off; a local flag-resolution error rejects the call as an infrastructure
failure. A successfully cached PostHog result remains in effect until the local
poller refreshes it, so every mode change must be verified on every serving
instance. Flag evaluation stays local to the process; the serving path does not
perform a remote PostHog request.

## Fleet-readiness gate

Before enabling shadow for any production cohort:

1. Identify the approved build containing both hosted and private checkpoints.
2. Prove every server instance and tunnel/private forwarding instance runs that
   build. Drain old instances; a mixed fleet is not ready.
3. Confirm dashboards and monitors below are live and owned.
4. Confirm no active production prescriptions exist before a mixed-version
   deploy. If any exist, lift them before changing serving versions.
5. Direct `HandleToolsCall` census observations remain active in off mode, while
   routed hosted and private-proxy checkpoint derivation starts in shadow. Enable
   shadow for a controlled cohort, then confirm both
   `gram.mcp.killswitch.surface:hosted` and
   `gram.mcp.killswitch.surface:private_proxy` appear in
   `mcp.tool.call.killswitch_identity`.
6. Confirm `killswitch.evaluation.duration` emits
   `gram.outcome:matched`, `gram.outcome:unmatched`, and
   `gram.outcome:evaluator_failure` in a non-production or controlled test. Do
   not create synthetic customer prescriptions.

Record the build, instance inventory, evidence links, reviewer, and timestamp in
the internal rollout record. Repository tests cannot prove fleet uniformity.

## Shadow observation

Start with an internal or restricted cohort. Keep shadow active for at least one
normal traffic cycle and one peak period. Select quantitative thresholds from a
reviewed baseline; the values below are initial stop conditions, not permanent
SLOs.

Observe:

- p95 and p99 of `killswitch.evaluation.duration`; p99 must remain comfortably
  below the one-second private checkpoint timeout and two-second hosted timeout;
- `gram.outcome:evaluator_failure` ratio; stop on any sustained rate above 0.1% for
  five minutes or any correlated serving incident;
- matched and unmatched evaluator-invocation volume; the histogram count records
  evaluator invocations, not PostgreSQL query count;
- active-user plus canonical-server coverage by `surface`; investigate any
  increase in `unavailable`, `invalid_owner`, or unsupported classes;
- PostgreSQL query rate, latency, CPU, lock waits, and pool saturation compared
  with the pre-shadow baseline. Stop on a sustained 10% load increase or pool
  saturation unless the database owner approves a different bound.

Shadow suppresses transport denial, including fail-closed evaluator outcomes.
It does not suppress evaluator and coverage telemetry. Shadow evaluation is
synchronous by design so it measures the serving-path cost that enforce mode
will add; restrict cohort size and monitor end-to-end MCP request latency.

## Datadog monitors

Create these in Datadog; monitor configuration is not stored in this repository.
Scope every monitor by environment. Evaluator duration has no surface attribute;
only the identity-coverage counter can be split by surface. Tune only after a
reviewed baseline.

1. **Evaluator failure ratio**: count of
   `killswitch.evaluation.duration{gram.outcome:evaluator_failure}` divided by
   all outcomes over five minutes. Warn at 0.05%; alert at 0.1%.
2. **Evaluator latency**: p95 and p99 of `killswitch.evaluation.duration`. Warn
   at 250 ms; alert at 500 ms.
3. **Coverage regression**: ratio of
   `mcp.tool.call.killswitch_identity{gram.mcp.killswitch.identity_class:active_user,gram.mcp.killswitch.resource_class:canonical_server}`
   to all observations, split by `gram.mcp.killswitch.surface`. Alert on a
   reviewed baseline regression, not on an arbitrary global percentage.
4. **Coverage unavailable**: any sustained
   `gram.mcp.killswitch.identity_class:unavailable` or
   `gram.mcp.killswitch.resource_class:unavailable`, plus
   `gram.mcp.killswitch.resource_class:invalid_owner` above baseline.
5. **PostgreSQL load**: query latency, pool saturation, lock waits, CPU, and
   evaluator query volume. Correlate changes with shadow cohort expansion.

Metric dimensions are bounded server classes. Never add organization IDs, user
IDs, server IDs, notes, URLs, or error text as metric tags.

## Enforce progression

Enable `mcp-killswitch-enforce` only after the fleet gate and shadow criteria
pass. Progress one restricted cohort at a time. For each cohort:

1. Enable enforce while shadow remains on.
2. Create a controlled prescription through the audited management path.
3. Verify the next matching call is denied with the exact external note. No
   restart, cache expiry, or propagation delay is expected.
4. Verify a non-matching call continues.
5. Lift the prescription and verify the next call continues.
6. Exercise a bounded expiry and verify database time restores the next call.
7. Review all monitors before expanding the cohort.

There is no TTL allow/deny cache. Any future summary optimization must be
updated transactionally with activation or change and visible on the next call.

## Rollback criteria

Stop expansion and roll back the cohort to off when any of these occurs:

- a mixed serving fleet is detected;
- evaluator failures exceed the approved threshold;
- p99 approaches a checkpoint timeout;
- authoritative identity or canonical-resource coverage regresses;
- PostgreSQL load exceeds the approved bound;
- denial text or JSON-RPC behavior differs from acceptance evidence.

Before rolling binaries backward, stop cohort expansion, lift active
prescriptions, verify no active or scheduled prescriptions remain, switch the
cohort off, and then drain newer instances. Never leave active prescriptions
while an older instance can bypass evaluation. Use the evaluator incident
runbook for an active failure.
