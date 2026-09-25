# Demo org page checklist

Acceptance contract for the demo seed. Every dashboard page a demo user can
reach must render populated (no empty state, no error boundary) from the data
in `postgres.sql` + `clickhouse.sql`. A page is DONE when its `verify.md`
check passes.

Status: `[x]` seeded + verified · `[~]` seeded, not yet verified · `[ ]` not seeded.

## Seeded

| Page                                                           | Backing data                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | Status |
| -------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------ |
| Agent sessions list                                            | PG `chats` + org `rbac` feature (without it ShouldEnforce=false hides everything)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | `[x]`  |
| Agent management setup                                         | Ten synthetic identities are browsable through Fleet; management still requires active membership, so the Agents setup page keeps its demo-disabled explanation                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | `[~]`  |
| Chat detail sheet (transcript + per-turn cost + tool payloads) | PG `chat_messages` (`message_id`=prompt id, `tool_call_id`=call_demo_i_k) + CH api_request/tool_result rows; demo-org impersonation lift in chat.load                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | `[~]`  |
| Risk events / findings                                         | PG `risk_results` (~125, 13 rule types across 6 policies) mirrored 1:1 into CH `risk_findings`; 8 enabled `risk_policies`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | `[x]`  |
| Watchdog (risk signals)                                        | CH `risk_findings` only — needs `chat_source`/`team`/`user_email` for its App/Team/top-user groupings, and a policy-score spread for its severities                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | `[~]`  |
| Dismissed findings / exclusions                                | PG `risk_exclusions` ×3 + suppressed `risk_results`; CH `excluded_reason` in {rule, manual, automated}                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | `[~]`  |
| Policy Center                                                  | Verify 9 policy rows (including Quarantine action) and 1 active `session_quarantines` row on the Quarantines tab                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | `[~]`  |
| Detection rules (custom CEL)                                   | PG `risk_custom_detection_rules` ×3, `detection_expr` only; compile-checked by TestSeedCELCompiles; all three now carry findings                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | `[~]`  |
| Risk overview (CH mirror ready)                                | PG today; CH `risk_findings` mirror seeded for the flag flip                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | `[x]`  |
| Cost dashboard, all pivots                                     | CH `attribute_metrics_summaries` via provenance rows carrying `user.attributes.*`, roles/groups, hostname, skill/agent/mcp attribution                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | `[~]`  |
| Costs Efficiency dataset                                       | CH `chat_analysis:work_units:score` rows                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | `[~]`  |
| Billing usage explorer                                         | CH raw `billing_meter_readings_by_time`: usage-only dashboard/API data across all nine meters, 864 ordinary facts over 12 days, 9 intentional duplicate physical deliveries, and project/agent/directory/server/scanner facets; inserts incrementally populate UTC-day `billing_meter_daily_summaries`, where all 873 physical deliveries count                                                                                                                                                                                                                                                                                                                                                | `[x]`  |
| Billing spend by product                                       | The enterprise demo intentionally hides the spend section: the API returns `unsupported_plan`, empty products, and a `"0"` total, while ordinary usage stays visible. Existing meter fixtures also support local PAYG checks for storage, per-scanner risk, and egress-only costs.                                                                                                                                                                                                                                                                                                                                                                                                             | `[~]`  |
| Admin billing spend by product                                 | Existing ordinary `billing_meter_daily_summaries` fixtures produce storage, per-scanner risk, and egress-only estimates for the enterprise demo without changing its tier or requiring a Stripe subscription. The admin API prices every account type at the same current PAYG list prices for usage comparison, not invoicing.                                                                                                                                                                                                                                                                                                                                                                | `[x]`  |
| Sessions list (telemetry.listSessions)                         | CH `chat_session_summaries` (via MV)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | `[x]`  |
| Project overview metric cards                                  | CH `metrics_summaries` (via MV)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | `[x]`  |
| External OAuth settings                                        | Route `/mcp/acme-oauth-discovery/authentication`; PG `external_oauth_server_metadata` attached to the dedicated Acme OAuth Discovery toolset; Gram-hosted metadata with an `example.com` issuer drives the provider-hosted recommendation without live discovery                                                                                                                                                                                                                                                                                                                                                                                                                               | `[~]`  |
| Tool logs / traces                                             | CH `trace_summaries`; page enterprise-gated for 'demo' account type (README change 7)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | `[~]`  |
| Insights (MCP & Tools)                                         | CH `trace_summaries`: unique per-surface trace ids + `gram.toolset.slug` (direct branch) + `gram.event.source=hook` rows (hook branch) + Skill hook rows                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | `[~]`  |
| Team page                                                      | PG `organization_user_relationships` + `users.workos_id` + role assignments (global_roles admin/member, skipped if absent)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | `[~]`  |
| Identities (roster + per-person pages)                         | PG memberships + `user_accounts` (3 personal, across two providers) + `mdm_devices` ×7 under one `device_integration_configs` row + `device_agent_syncs`; CH telemetry keyed by email (agent-metrics view) and by `user.id` (raw logs), plus `ai_scan_receipts` ×35 and `ai_detections` ×52 across six directory users and all three categories (harness, assistant, local_model) for per-person Shadow AI — one account-less address and one agent id so both kinds render; request rows carry `gen_ai.response.id` or the Chat requests tile reads 0; `gram.account_type` splits one person's chats and tool calls across team and personal so the Usage tab's account filter has both sides | `[~]`  |
| Device coverage widgets (org Device agent page)                | PG `mdm_devices` ×7 spanning the coverage buckets — agent active, agent stale, no agent, unresolved email — plus `device_agent_syncs` for the four with a reporting agent                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | `[~]`  |
| Org home (activity, facepiles, challenges)                     | PG `audit_logs` (27 rows: 12 general, 1 trial change, 1 quarantine, 13 Killswitch lifecycle) + CH `authz_challenges` (13 rows incl. api_key bucket)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | `[~]`  |
| Audit logs                                                     | PG `audit_logs`: 13 canonical Killswitch actions (activate ×10, change, deactivate, expire), plus the existing general/trial-change/quarantine history                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | `[~]`  |
| Killswitches (identity Access tab and Fleet)                   | PG `killswitch_prescriptions` ×10 current aggregates: Active ×7, Scheduled ×1, Lifted ×1, Expired ×1; six user restrictions and four agent restrictions, with selected/all scopes and two overlapping principals                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | `[~]`  |
| Killswitch record and version history (user and agent targets) | PG `killswitch_prescription_versions` ×12 + complete resource snapshots; changed A/B/C → A, lifted successor, expiry marker, notes, and matching Audit events                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | `[~]`  |
| Access challenges                                              | CH `authz_challenges` (member user_ids pass the suppression filter)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | `[~]`  |
| Budgets / spend controls                                       | PG `spend_rules` ×2 + `spend_rule_events` ×4, calibrated to CH usage (breach+warning per rule); usage MV already fed by existing rows                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | `[~]`  |
| Toolsets / MCP / Sources / Deployments / Playground            | PG deployment stack: asset + completed deployment + 8 `http_tool_definitions` (urns match telemetry, doc slug `acme`) + 4 toolsets (+versions)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | `[~]`  |
| Prompts                                                        | PG `prompt_templates` ×2                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | `[~]`  |
| Skills                                                         | PG `skills` ×3 + `skill_versions` + 1 open edit suggestion with diff                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | `[~]`  |
| Shadow MCP                                                     | CH `shadow_mcp_inventory_urls` ×15 + `hooks:` telemetry rows                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | `[~]`  |
| Directory dimensions                                           | PG `directory_users`/`directory_groups`/memberships mirroring the CH `user.attributes.*` profiles                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | `[~]`  |
| Gateway overview Activity + MCP listing gateway marker         | CH `telemetry_logs` rows stamped `gram.meta_mcp_server.id` = det_uuid(`gram-demo-metamcp-1`): `meta_discovery` rows (list_servers → describe_server → describe_tools, the Gateway tool usage chart) + member `tool_call` rows keyed by `gram.mcp_server.id` for the four members, unique `gwdisc`/`gwcall` trace namespaces; `gwgone` dispatches to GitHub (membership soft-deleted 3 days ago, PG `meta_mcp_server_members.deleted_at`) that stay out of Calls by member; `gwhook` hook rows whose `gram.mcp.server_url` is the gateway endpoint URL, classified as the gateway on Tool Logs / Insights                                                                                       | `[~]`  |
| MCP connections (server tab, org MCP Sessions, identity page)  | PG project `user_session_issuers` ×4 plus organization `user_session_issuers` ×1 (used by the GitHub server), `user_session_clients` ×5 (one per credential kind, plus a pre-column row), and `user_sessions` ×5                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | `[x]`  |
| Workload sessions (project MCP Sessions, revoke dialogs)       | PG `workload_issuers` ×2 (reserved-domain URLs; `Acme CI` refuses wildcard admission because its subjects encode a branch ref, `Acme Agent Platform` permits it) + `workload_identity_admissions` ×4 (project and organization tier; the payments workload is admitted at both; one `wildcard` rule standing for the agent fleet) + `workload_agent_assignments` ×3 (active and suspended managed agent, plus the wildcard fleet assignment) + inert `workload:` `user_sessions` ×2 on the gateway issuer                                                                                                                                                                                      | `[~]`  |
| Workload Identities (trust policy page)                        | The same `workload_issuers` / `workload_identity_admissions` / `workload_agent_assignments` rows, read through `workloadIdentities.list`: two issuers with opposing wildcard permission, all four admissions at both tiers, three exact and one wildcard, and the agent each resolves to. Demo visitors hold every scope through `authz.DemoScopeGrants`, so the page renders populated here; a real organization needs `workload:read`, which only reaches organizations provisioned after AIM-291 (see AIM-360).                                                                                                                                                                             | `[~]`  |
| Organization setup board                                       | PG `organization_setup_tasks` overrides for member-owned In Progress, email-owned Awaiting Support, Done, and Hidden; catalog defaults supply To Do and blocked states                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | `[~]`  |
| Network Access                                                 | Deliberately no `network_ingress` entitlement, ingress row, or credential; every reseed removes active and tombstoned private-ingress state before asserting absence, so the temporary PostHog rollout cannot expose retained setup or server-mode controls to the demo organization                                                                                                                                                                                                                                                                                                                                                                                                           | `[~]`  |
| Explore (analytics.query over agent_events)                    | CH `agent_events`: 144 sessions over the trailing 12 days for the six users, dealt across claude-code and codex, 2-5 turns each with prompt/api_request/api_response rows and 0-3 tool calls (tool_decision + tool_call_result on Claude, results alone on Codex); Codex rows state tokens but no cost                                                                                                                                                                                                                                                                                                                                                                                         | `[x]`  |

Onboarding selection: the seed persists 13 explicit task rows, 10 visible, with a customized Security preset. Distribute servers is included and Anthropic admin controls is deferred. In Admin organization Features, verify "security - customized", apply a preset to a draft, then discard it; saved task status and assignment must remain unchanged.

The ordinary MCP connection inventory totals 11 sessions: five on Acme Partner
Gateway and six across Linear, Slack, and Acme Agent Gateway. The eight managed-agent credential sessions below are checked separately from those human connections.

### Gateway instructions

The seeded gateway leaves `instructions` NULL. Settings must display the built-in
instructions as the editor's actual value, not a placeholder, with Save disabled
until edited and no append/replace selector. Saved custom text replaces the
built-in text; saving an empty editor restores the built-in instructions. This
behavior is covered by editor tests; browser verification remains pending.

### Managed agents

The Identities roster also reads these ten registered-agent fixtures
in an ordinary local session. Agent names open the shared identity overview at
`/:orgSlug/projects/:projectSlug/identities/agent%3A<AGENT_ID>/overview`, with an
"Edit Agent Identity" link to the agent-management screen. Owners without
`org:read` go directly to Agent Identity management instead. Identity is the first
project navigation group; organization membership appears under Team → Members. The roster toggle
selects All, Humans, or Agents; unmatched telemetry identifiers without an email are Unknown, not Agent.
Fleet permits a narrow read-only synthetic inventory projection in the shared demo; selected-agent and credential APIs still require membership.

Registered-agent profiles read the stored agent policy on Access and Overview;
saving permissions in Agent Identity invalidates both views. Verify restricted
resource and tool selectors remain visible. Activity separates human changes to
the agent from actions attributed to the agent; Connections uses the dedicated
agent sessions API, with its credential authorization gate. Accounts & devices
shows agent API keys. Usage, cost, and risk aggregation currently lack a registered
agent identifier and must show unavailable, never human-owner activity or zeros.

Verify management in the local rewritten seed with an ordinary human session.
`agent-management` enables inventory; `agent-identity-credentials` enables API key
management. PG `agents` ×10 spans four departments and both project and
organization scope. Eight expired credential sessions and three expired inert
agent keys provide historical authentication evidence; two sessions and one key
are soft-deleted. No fixture can authenticate or refresh. The two active release
agents retain their exact Linear grants and inert upstream attachments. The
organization API Keys list excludes agent-subject keys; agent key tabs show their
expired status. Shared SQL clears visitor-created keys before inserting these
three inert examples. Local-only usable keys remain in `RunLocalFixtures`.
Reseeding clears only the target organization's agent grants and restores the two
scoped policies; human grants are preserved. Follow check 17 in `verify.md` for
management, and the Fleet contract below for read-only demo browsing.

### Audit session target resolution

Project Activity Timeline, organization Recent activity, and View all / Audit
logs must distinguish the human revoker from the affected session owner. The
current local audit history includes user- and agent-owned session revocations.
For these rows, `subject_id` is the session ID, not a user or agent ID; resolve
`metadata.subject_urn` when present, otherwise the raw `subject_display_name`
URN. Users link to their identity page. Readable agents show their authorized
name, muted bot icon, and dotted link to agent management. Deleted, unreadable,
or failed-to-load agents remain raw `agent:<AGENT_ID>` text without a link or
cached name. Direct agent subjects and agent actors follow the same rule.

These are expectations for existing local history, not additional deterministic
seed rows. No reseed is required to inspect that history; reseeding may remove
locally generated revoke events. Keep raw metadata and snapshot diffs available
in the audit feed. Shared-demo agent access remains restricted as above.

### Exact remote-session attachments

PG `remote_sessions` ×1 and `principal_remote_session_bindings` ×2 show one
fictional user-owned upstream account shared by **Release assistant** and
**Release notes assistant** (both active, same human owner). Linear MCP session
6 is the requesting human session; its issuer is explicitly linked to the
upstream client. Both bindings retain the same exact session ID, not a copy of
its credential. The original three agent lifecycle fixtures remain unchanged.
The account has invalid ciphertext, no refresh token, auto-refresh disabled,
and reserved `.invalid` issuer metadata. It is display-only, not a live OAuth
integration. All IDs reuse `Spec.NameSeed` and retarget with the tenant.
Reseeding deletes bindings before sessions, issuers, agents and projects.
Browser verification: `[~]` (not yet verified); see check 18 in `verify.md`.

## Local only (RunLocalFixtures, never the demo org)

These come from `server/internal/demoseed/local.go` after the seed, so they are
present in a developer's org and deliberately absent from the shared demo org.

| Page               | Backing data                                                                                |
| ------------------ | ------------------------------------------------------------------------------------------- |
| Environments       | One `Default` environment                                                                   |
| Playground MCP App | Functions deployment + UI resource, zipped from `demoseed/mcpapp/`                          |
| API keys           | The well-known `seed-key`, with its project binding shown separately from permission scopes |
| Catalog            | The global `Gram Recommended` registry row                                                  |

## Not seeded (deliberate)

| Page                     | Why                                                          |
| ------------------------ | ------------------------------------------------------------ |
| Plugins                  | Auto-provision on first visit; empty state is intentional    |
| Integrations / Triggers  | Acceptable empty states                                      |
| Settings                 | Render fine without seed data                                |
| ChatGPT/Work usage split | Later phase (`chatgpt:usage:metrics` rows)                   |
| Logs page content        | Enterprise-gated for the demo account type (README change 7) |

## Rules when extending

1. New page = new row here + seed data + a `verify.md` check, in the same
   commit.
2. All ids derive from the fixed constants in `postgres.sql` (org
   `org_gram_demo_workspace`, projects `dec0de00-…0001`/`…0002`); chat ids are
   `md5('gram-demo-chat-' || n)` in BOTH stores; the owner index formula
   `1 + (n % 6)` must stay identical in both files.
3. Timestamps are always relative to `now()`, trailing ≤ 12 days, so every
   chat clears the MV date cutoffs, the daily prod rerun keeps data fresh, and
   MVs populate on INSERT — never hand-backfill an MV target. Billing meter
   facts follow the same rule: clear both the tenant's raw ledger and daily
   summary target, wait for delete visibility, then let the fresh raw inserts
   incrementally repopulate UTC-day summaries.
4. Every new CH insert target (or new MV) gets a matching scoped DELETE in
   `clickhouse.sql`, or re-runs double the numbers.
5. Cost/session MVs are provenance-first: usage rows must be Claude OTEL
   api_request (+`prompt.id`), `cursor:usage:*`, codex OTEL, or agent
   PostToolUse hook shapes — generic `chat:completion` rows are ignored.
6. Give every row surface its own trace-id namespace; shared trace ids merge
   into one unclassifiable trace in `trace_summaries`.

Anthropic inference hooks: Agent Sessions includes “Claude inference conversation” with a user prompt, assistant reply, and follow-up. The source is Claude Chat; Raw view retains the original content blocks.

Anthropic inference hooks on AI Integrations shows **Finish setup** with a pending URL and no signing secret. The seeded configuration never permits real inference deliveries.

Claude Tag: Agent Sessions includes “Claude Tag in #demo-releases”. Open it to see the demo-releases channel, human message, and assistant reply. Raw view reveals the wake envelope and Slack reply tool call.

- `[~]` Trial end-date changes: org-scoped audit example records a shortened trial with previous/new dates; browser verification pending.

The Collaborator role includes project-selected `plugin:write` for plugin references and publishing, separately from its unchanged skill authoring grants.

The global registry may be empty until staff manually add servers through the
admin UI. JSON records are local/test contract fixtures, not production setup.
Tenant reseeds do not populate or reset the global catalog. Existing Pulse-backed
demo installations remain intact. Global catalog administration is staff-only
and intentionally absent from the demo customer's pages.

### Identity chaining preparation (management API) — `[x]`

The project has one reserved-example remote issuer advertising both ID-JAG and
JWT-bearer, plus a separate resource registration with explicitly recorded
JWT-bearer grants and `documents:read` scope. It contains no secret, binding,
remote session, or claim of usable human access.

Browser-verified on 2026-09-23 against the actual local demo seed at
`8e4fa893a862edc5219adf973a80724133c1ed25`, after two successful `mise run seed`
runs. The issuer Overview renders its JWT-bearer capability; Clients lists
`demo-resource-client`, whose Overview renders `documents:read` and auth method
`none`. Its Sessions tab correctly shows no active sessions, and the project
MCP Sessions search finds no connection for it. These intentional empty session
states are part of the inert fixture contract, not missing seed data.

The client Overview does not render effective grant types, and the issuer
Overview does not render the ID-JAG profile. Separate management API checks
verified those fields without conflating issuer capability with client grant
evidence. Client deletion preflight and a read-only local database check
confirmed zero bindings/sessions and no client secret. No dedicated preparation
dashboard, provider acceptance, or usable human access is demonstrated.

[Browser evidence and supplementary checks](https://github.com/speakeasy-api/gram/pull/6438#issuecomment-5798502630).

### Fleet and agent MCP restrictions — `[~]`

Fleet is in Observability, gated as a whole by the dashboard PostHog flag
`gram-fleet`. Disabled, loading, missing and error states mount no Fleet data
queries. `agent-management` independently gates the registered inventory and
`assistants` gates assistant inventory. Local dashboard flags are enabled;
`server/flags.csv` enables agent management for the local demo organization.
Production PostHog must define `gram-fleet` and enable the relevant flags for the
demo organization group. This PR does not change remote PostHog configuration.

Fleet shows observed activity in the last 24 hours. Core fixtures are 2–20 minutes
old at reseed, so they survive nearly the full daily interval; there are no future
timestamps or scheduler changes. Reseed before local verification. Registration
edits do not count. Credential stamps are organization-wide, best-effort and
coalesced (roughly five minutes for sessions and one for keys), with no freshness
guarantee. Normal viewers need credential-management permission to receive them;
ordinary demo visitors receive only synthetic registration data and aggregate
timestamps with read-only permissions. No demo credential, policy or mutation
endpoint is opened.

| Registered identity         | Owner / department             | Scope        | Evidence / lifecycle                                                   |
| --------------------------- | ------------------------------ | ------------ | ---------------------------------------------------------------------- |
| Release assistant           | Amara / Support Engineering    | Organization | Session 2m ago; active, selected + all-server restrictions             |
| Support triage              | Jonas / Support Engineering    | Organization | Session 12m ago; suspended 6m ago; all-server restriction              |
| Retired documentation bot   | Priya / Platform Engineering   | Project      | Session 15m ago; revoked 8m ago; credential revoked with it            |
| Release notes assistant     | Amara / Support Engineering    | Organization | API key only, 3m ago; active                                           |
| Billing reconciliation      | Hana / Billing Operations      | Project      | Session 5m ago beats key 25m ago; active                               |
| Deploy verifier             | Mateo / Platform Engineering   | Project      | Session 10m ago; credential revoked 4m ago; registration active        |
| Incident evidence collector | Priya / Platform Engineering   | Organization | Session 7m ago; active                                                 |
| Engineering digest          | Lucas / Engineering Leadership | Project      | Session 20m ago; active                                                |
| Legacy invoice exporter     | Hana / Billing Operations      | Organization | Last use 4d ago, suspended 3d ago; omitted but restriction recoverable |
| Nightly report agent        | Lucas / Engineering Leadership | Organization | Never used; omitted                                                    |

Three assistants have five explicit `assistant_threads`: four recent conversations
and one 48-hour historical conversation. Support handoff assistant (creator Jonas),
Deployment review assistant (creator Priya), and Billing review assistant (creator
Hana, paused after the latest capture) tell support, engineering and billing
stories with four-message transcripts. Capture users differ from creators.
Assistants appear through explicit assistantId evidence on session pages loaded
during the visit; search retains that evidence. The initial 50 captures include
all three assistants, asserted by postflight.

Eight dedicated captures include the five linked conversations plus an unverified
external contractor, an external id equal to a directory id (still unverified),
and an unattributed capture. A synthetic email-address finding in Deployment review has
one matching PG/CH finding; it is not an enforcement receipt. Existing bulk
history remains unchanged. Captures never bind to registered agents through
names, owners, creators, or external ids. Directory roles remain Owner, Creator,
and Captured user.

Restrictions are local-admin verification only; shared demo visitors cannot
read or mutate them. Release assistant's selected/all restrictions overlap;
Support triage and the old Legacy invoice exporter retain independent restrictions.
Release removes one restriction, not suspension or other restrictions, and never
restarts a process. Covered effect is MCP tools/call only. Agent recovery remains
available through `/killswitch/:id` and the `/killswitch` index when Fleet is off
or project:read is absent. That fallback also links to people’s restrictions.

Verified in the local shared demo: all three sources, department grouping,
explicit attribution, the flagged transcript, read-only inspector panels, and
phone focus restoration. The local administrator view also exposes all four
agent restrictions, including recovery for the older identity. Deployed-preview
markers are recorded separately; no remote authenticated automation or remote
seed execution is claimed.
