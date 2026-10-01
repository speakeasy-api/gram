# Role distribution setup

One-time, best-effort setup creates or reuses an actual plugin for an application
role. It is not role synchronization or ongoing reconciliation.

Setup normalizes the role display name with `conv.ToSlug` (`Sales Team` becomes
`sales-team`) and matches an active plugin by that exact slug in the oldest active
project. Reuse preserves its display name, contents, and existing audiences. If
none matches, setup creates a plugin with the role display name and normalized
slug. Invalid slugs fail through the existing best-effort error path; there is no
display-name fallback, generated slug, or collision suffix.

Existing creation/recreation and organization bootstrap SQL publishes `role_distribution.setup_requested_v1` through the transactional
outbox. Ordinary updates and repeated directory syncs publish no setup event.
The Platform MCP uses its existing role-creation transaction; no separate
provisioning tool or customer-facing rollout control is needed.

A streams consumer processes committed role URNs. Global-role and
organization-bootstrap events expand using stable cursors in pages of 100;
new setup events and any next-page event commit with the page. These symmetric
post-commit passes cover concurrent organization/global-role creation. There is
no feature Temporal workflow, timer, recovery sweep, or retry scheduler.

## Delivery and failures

- The shared outbox drains immediately while backlogged and sleeps five seconds
  when idle. Its one-minute watchdog restarts bounded runs; startup/restart gaps
  can approach one minute. These are mechanisms, not a delivery latency SLA.
- The existing relay normally dead-letters failed publication after ten attempts,
  with full-jitter exponential backoff capped at ten minutes. Database failures
  and crash recovery use existing relay semantics, not a strict invocation cap.
- The setup subscription uses ten best-effort delivery attempts and 10–600 second
  backoff, with the existing Pub/Sub dead-letter topology. Forwarding requires
  the deployment's standard Pub/Sub service-agent permissions.
- Handler errors are logged with the event target and returned for transport
  retry. After transport dead-lettering there is no application recovery loop.
  Delivery counts and exhaustion are best-effort, not an exactly-once guarantee.

No attempt counters or persistent completion ledger are maintained here.
Setup atomically commits plugin changes, assignments, and publication enqueue.
Duplicate delivery reuses matching plugins and assignments. Disabled organizations
and flags intentionally skip rather than fail. There is no automatic reset loop.

New organizations enable `automatic-role-distribution`; existing organizations
are not enabled by this change. Apply the schema before running revised code.
