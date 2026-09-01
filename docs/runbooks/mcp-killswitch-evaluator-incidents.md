# MCP Killswitch Evaluator Incidents

Use this runbook when authenticated MCP `tools/call` traffic is denied because
the authoritative Killswitch checkpoint cannot evaluate PostgreSQL state, or
when rollout telemetry indicates unsafe evaluator behavior. The registered M2
policy fails closed in enforce mode.

## Immediate response

1. Declare an incident and assign an incident commander and database owner.
2. Stop rollout cohort expansion. Do not create or edit prescriptions.
3. Check `killswitch.evaluation.duration{outcome:evaluator_failure}`, p95/p99
   latency, PostgreSQL health, pool saturation, lock waits, and deploy changes.
4. Compare hosted and private proxy outcomes. A one-surface failure can indicate
   a mixed or unhealthy serving fleet.
5. Determine whether lifecycle APIs can still reach PostgreSQL. Do not assume
   that management access works merely because it bypasses evaluation.

Do not copy bearer tokens, external notes, user identifiers, organization
identifiers, server identifiers, or customer request bodies into logs, tickets,
dashboards, or comments.

## Safe mitigation order

When lifecycle access is healthy and PostgreSQL is not saturated:

1. Enumerate active and scheduled prescriptions through the restricted platform
   management path.
2. Lift or deactivate them with unique operation IDs. Deactivation remains
   available while activation and change are rollout-gated.
3. Verify each operation is audited and no active or scheduled prescriptions
   remain.
4. Switch affected cohorts to rollout off and verify every serving instance
   received the mode and calls continue without an evaluator query.
5. Repair the evaluator or database, restore shadow, observe, then re-enter the
   normal rollout gate.

When PostgreSQL latency, pool saturation, or lock load is itself the incident,
switch affected cohorts off first and prove the local flag converged across the
fleet. After load stabilizes, enumerate and deactivate prescriptions in bounded,
paginated batches. If flag convergence cannot be proved, prefer deactivation
before any binary rollback.

When lifecycle access is also unavailable, use the approved out-of-band
configuration path to switch affected cohorts off from the locally cached
feature state. Confirm the new local state reached every serving instance. If
that path cannot converge, perform an explicitly approved full-fleet rollback.
A partial binary rollback is unsafe while prescriptions might remain active.

## Break-glass rules

- Break-glass credentials are restricted to approved incident responders.
- Prefer deactivation. Activation or change during an evaluator incident needs
  explicit incident-commander approval and an audited reason.
- Use a new operation ID for each intended mutation; never retry with altered
  input under an existing operation ID.
- Confirm organization authorization and tenant binding. Never infer authority
  from email, API-key ownership, creator fields, or cached attribution.
- Record only prescription IDs and internal evidence in the restricted incident
  system. Keep concrete customer identifiers out of repository artifacts.

Break-glass lifecycle operations still depend on PostgreSQL. They do not solve a
total database outage.

## Recovery checks

Before leaving off mode:

1. PostgreSQL query latency, lock waits, CPU, and pool saturation are healthy.
2. Every serving instance runs the approved checkpoint build.
3. Hosted and private proxy coverage metrics are present.
4. In a controlled environment, unmatched, matched, and evaluator-failure
   outcomes are observable and bounded.
5. Shadow traffic runs through a peak period without exceeding approved latency,
   failure, coverage, or database-load thresholds.
6. A controlled activation denies the next matching call, lift restores the
   next call, and expiry follows database time.
7. The incident commander and service owner approve cohort enforcement.

## Rollback validation

After mitigation or binary rollback:

- no mixed serving version remains;
- no active or scheduled prescription can be silently bypassed;
- off mode performs no evaluator query;
- ordinary MCP traffic is restored;
- audit records exist for every break-glass mutation;
- monitors have returned to baseline;
- unsupported identities, resources, and methods remain outside the published
  coverage boundary.

Do not add a TTL negative cache as an incident fix. A future summary or cache
must be transactionally updated with prescription activation and changes so the
next call observes authoritative state.
