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

## Server contents

Role audiences also distribute eligible servers within each plugin's project.
Granting effective `mcp:connect` access adds servers to existing matching plugins;
assigning a role audience includes its existing access. Grant revocation or
audience removal removes affected servers unless another assigned role still
supplies them. Manual and automatic additions are the same membership, without
separate ownership. Unrelated contents remain unchanged. This content path never
creates a plugin or changes server permissions.

Administrator removal is an ordinary content removal. Unchanged grant/audience
replay and publication do not restore it; a new applicable grant or new role
audience can add the server again. Bounded setup/backfill and server-eligibility
delivery skip previously removed memberships rather than replaying additions.
Deletion history is not a permanent exclusion from explicit changes. Setup also
populates existing role-audience plugins, not just the plugin it creates/reuses.

Content changes use the existing transactional publication request. A queued
request is not proof of publication or installed-client refresh; an unconfigured
marketplace cannot publish. Marketplace-connected clients follow their existing
update behavior. Local ZIP installations require downloading and replacing the
package and following the client's reload/new-session instructions. There is no
hot-reload or immediate installed-content removal guarantee.

Memberships are deduplicated by backend identity within each plugin, including
legacy toolset/MCP-wrapper representations. Cross-plugin installed-client
deduplication is not guaranteed: generated client keys use membership display names,
and supported clients may show the same server through multiple plugins. Runtime
authorization remains authoritative regardless of stale published/client content.

### Cross-plugin client check

Verified on 2026-10-02 with **Claude Code 2.1.287 on macOS**, using a local
marketplace and normal `claude plugin marketplace add` / `claude plugin install`
commands in an isolated `CLAUDE_CONFIG_DIR` and empty workspace. Two synthetic
plugins, `engineering` and `on-call`, each declared the same `shared-server` key
and identical HTTP endpoint in `.mcp.json`, matching Gram's Claude package shape.
The endpoint was a loopback MCP test server, not a deployed Gram server.

Observed:

- `claude plugin list --json` showed both plugins installed and enabled, each
  containing the shared server configuration.
- With both installed, `claude mcp list` showed **one** connected entry:
  `plugin:engineering:shared-server`. The test server received one initialization
  and one `tools/list` request during that health check.
- After uninstalling `engineering`, a fresh `claude mcp list` showed
  `plugin:on-call:shared-server` connected. Initialization and tool discovery
  succeeded again through the remaining plugin.

Thus this client/version deduplicated the identical configurations in its MCP
listing; removing the displayed plugin did not prevent discovery through the
remaining plugin. This does not establish which plugin wins in other install
orders or a universal deduplication rule. Tool invocation, interactive UI,
authenticated Gram endpoints, differing names/headers, remote marketplace refresh,
Cursor, and Claude Desktop/Cowork were not verified by this check. Do not infer
those behaviors from package generation or this CLI result.

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

## Temporary staff rollout

The staff feature mutator changes `automatic-role-distribution` and enqueues one
organization-bootstrap outbox event in the same transaction. Repeated ON does
nothing. OFF intentionally skips setup and bootstrap events without modifying
plugins or assignments. A later OFF→ON explicitly starts one new bounded pass
over all active global/local roles. Matching plugins and assignments are reused;
there is no completion ledger excluding previously processed roles. It does not
create a scanner or new retry engine.

The organization advisory lock is shared by staff feature mutations, setup
attempts, and bootstrap expansion. It serializes OFF with in-flight setup and
also protects ON when no active feature row exists. Each page selects at most
100 roles after the role-URN cursor; setup events and the next-page event commit
together. No new schema or retry state is required. New-organization defaults
and demo seed enablement remain those established by the role-creation change.
