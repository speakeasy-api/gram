# Demo org page checklist

Acceptance contract for the demo seed. Every dashboard page a demo user can
reach must render populated (no empty state, no error boundary) from the data
in `postgres.sql` + `clickhouse.sql`. A page is DONE when its `verify.md`
check passes.

Status: `[x]` seeded + verified · `[~]` seeded, not yet verified · `[ ]` not seeded.

## Seeded

| Page                                                           | Backing data                                                                                                                                                      | Status |
| -------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------ |
| Agent sessions list                                            | PG `chats` + org `rbac` feature (without it ShouldEnforce=false hides everything)                                                                                 | `[x]`  |
| Chat detail sheet (transcript + per-turn cost + tool payloads) | PG `chat_messages` (`message_id`=prompt id, `tool_call_id`=call_demo_i_k) + CH api_request/tool_result rows; demo-org impersonation lift in chat.load             | `[~]`  |
| Risk events / findings                                         | PG `risk_results` (~125, 13 rule types across 6 policies) mirrored 1:1 into CH `risk_findings`; 8 enabled `risk_policies`                                         | `[x]`  |
| Watchdog (risk signals)                                        | CH `risk_findings` only — needs `chat_source`/`team`/`user_email` for its App/Team/top-user groupings, and a policy-score spread for its severities               | `[~]`  |
| Dismissed findings / exclusions                                | PG `risk_exclusions` ×3 + suppressed `risk_results`; CH `excluded_reason` in {rule, manual, automated}                                                            | `[~]`  |
| Policy Center                                                  | Verify 9 policy rows (including Quarantine action) and 1 active `session_quarantines` row on the Quarantines tab                                                  | `[~]`  |
| Detection rules (custom CEL)                                   | PG `risk_custom_detection_rules` ×3, `detection_expr` only; compile-checked by TestSeedCELCompiles; all three now carry findings                                  | `[~]`  |
| Risk overview (CH mirror ready)                                | PG today; CH `risk_findings` mirror seeded for the flag flip                                                                                                      | `[x]`  |
| Cost dashboard, all pivots                                     | CH `attribute_metrics_summaries` via provenance rows carrying `user.attributes.*`, roles/groups, hostname, skill/agent/mcp attribution                            | `[~]`  |
| Costs Efficiency dataset                                       | CH `chat_analysis:work_units:score` rows                                                                                                                          | `[~]`  |
| Sessions list (telemetry.listSessions)                         | CH `chat_session_summaries` (via MV)                                                                                                                              | `[x]`  |
| Project overview metric cards                                  | CH `metrics_summaries` (via MV)                                                                                                                                   | `[x]`  |
| Tool logs / traces                                             | CH `trace_summaries`; page enterprise-gated for 'demo' account type (README change 7)                                                                             | `[~]`  |
| Insights (MCP & Tools)                                         | CH `trace_summaries`: unique per-surface trace ids + `gram.toolset.slug` (direct branch) + `gram.event.source=hook` rows (hook branch) + Skill hook rows          | `[~]`  |
| Team page                                                      | PG `organization_user_relationships` + `users.workos_id` + role assignments (global_roles admin/member, skipped if absent)                                        | `[~]`  |
| Employee enrollment                                            | PG memberships + `user_accounts` + `device_owners` + `device_agent_syncs`; roster served via impersonation carve-out                                              | `[~]`  |
| Org home (activity, facepiles, challenges)                     | PG `audit_logs` (13 rows, including `session_quarantine:open`) + CH `authz_challenges` (13 rows incl. api_key bucket)                                             | `[~]`  |
| Access challenges                                              | CH `authz_challenges` (member user_ids pass the suppression filter)                                                                                               | `[~]`  |
| Budgets / spend controls                                       | PG `spend_rules` ×2 + `spend_rule_events` ×4, calibrated to CH usage (breach+warning per rule); usage MV already fed by existing rows                             | `[~]`  |
| Toolsets / MCP / Sources / Deployments / Playground            | PG deployment stack: asset + completed deployment + 8 `http_tool_definitions` (urns match telemetry, doc slug `acme`) + 3 toolsets (+versions)                    | `[~]`  |
| Prompts                                                        | PG `prompt_templates` ×2                                                                                                                                          | `[~]`  |
| Skills                                                         | PG `skills` ×3 + `skill_versions` + 1 open edit suggestion with diff                                                                                              | `[~]`  |
| Shadow MCP                                                     | CH `shadow_mcp_inventory_urls` ×15 + `hooks:` telemetry rows                                                                                                      | `[~]`  |
| Directory dimensions                                           | PG `directory_users`/`directory_groups`/memberships mirroring the CH `user.attributes.*` profiles                                                                 | `[~]`  |
| MCP connections (server tab, org MCP Sessions, employee page)  | PG `user_session_issuers` ×1 on the Acme Partner Gateway server + `user_session_clients` ×5 (one per credential kind, plus a pre-column row) + `user_sessions` ×5 | `[x]`  |

## Local only (RunLocalFixtures, never the demo org)

These come from `server/internal/demoseed/local.go` after the seed, so they are
present in a developer's org and deliberately absent from the shared demo org.

| Page               | Backing data                                                       |
| ------------------ | ------------------------------------------------------------------ |
| Environments       | One `Default` environment                                          |
| Playground MCP App | Functions deployment + UI resource, zipped from `demoseed/mcpapp/` |
| API keys           | The well-known `seed-key`                                          |
| Catalog            | The global `Gram Recommended` registry row                         |

## Not seeded (deliberate)

| Page                              | Why                                                          |
| --------------------------------- | ------------------------------------------------------------ |
| Plugins / Assistants              | Auto-provision on first visit; empty state is intentional    |
| Integrations / Triggers           | Acceptable empty states                                      |
| Billing / Device agent / Settings | Render fine without seed data                                |
| ChatGPT/Work usage split          | Later phase (`chatgpt:usage:metrics` rows)                   |
| Logs page content                 | Enterprise-gated for the demo account type (README change 7) |

## Rules when extending

1. New page = new row here + seed data + a `verify.md` check, in the same
   commit.
2. All ids derive from the fixed constants in `postgres.sql` (org
   `org_gram_demo_workspace`, projects `dec0de00-…0001`/`…0002`); chat ids are
   `md5('gram-demo-chat-' || n)` in BOTH stores; the owner index formula
   `1 + (n % 6)` must stay identical in both files.
3. Timestamps are always relative to `now()`, trailing ≤ 12 days, so every
   chat clears the MV date cutoffs, the daily prod rerun keeps data fresh, and
   MVs populate on INSERT — never hand-backfill an MV target.
4. Every new CH insert target (or new MV) gets a matching scoped DELETE in
   `clickhouse.sql`, or re-runs double the numbers.
5. Cost/session MVs are provenance-first: usage rows must be Claude OTEL
   api_request (+`prompt.id`), `cursor:usage:*`, codex OTEL, or agent
   PostToolUse hook shapes — generic `chat:completion` rows are ignored.
6. Give every row surface its own trace-id namespace; shared trace ids merge
   into one unclassifiable trace in `trace_summaries`.
