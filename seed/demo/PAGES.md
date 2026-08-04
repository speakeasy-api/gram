# Demo workspace page checklist

Acceptance contract for the demo seed. Every dashboard page a demo user can
reach must render populated (no empty state, no error boundary) from the data
in `postgres.sql` + `clickhouse.sql`. A page is DONE when its `verify.md`
check passes.

Status: `[x]` seeded + verified · `[~]` seeded, not yet verified · `[ ]` not seeded.

## Phase 1 (current)

| Page                                   | Backing data                                                                                                                                            | Status |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ------ |
| Agent sessions list                    | PG `chats` (needs unrestricted `chat:read`)                                                                                                             | `[~]`  |
| Chat detail sheet (transcript)         | PG `chat_messages`, `risk_results`                                                                                                                      | `[~]`  |
| Risk events / findings                 | PG `risk_results` + `risk_policies` (enabled)                                                                                                           | `[~]`  |
| Cost dashboard (by user/model/agent)   | CH `attribute_metrics_summaries` (MV admits ONLY provenance rows: Claude OTEL `api_request`/`tool_result`, `cursor:usage:*`, agent `PostToolUse` hooks) | `[~]`  |
| Sessions list (telemetry.listSessions) | CH `chat_session_summaries` (via MV)                                                                                                                    | `[~]`  |
| Project overview metric cards          | CH `metrics_summaries` (via MV)                                                                                                                         | `[~]`  |
| Tool logs / traces                     | CH `trace_summaries` (via MV from `tools:` rows)                                                                                                        | `[~]`  |

## Later phases (not seeded yet)

| Page                                                | Backing data                                                                                                            | Status |
| --------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- | ------ |
| Per-turn cost float in chat detail                  | CH `claude_code.api_request` rows with `prompt.id`                                                                      | `[ ]`  |
| Tool payload sizes in chat detail                   | CH `claude_code.tool_result` rows keyed by `tool_use_id`                                                                | `[ ]`  |
| Spend control rules/usage                           | PG spend rules + CH `spend_rule_usage_summaries`; urn needs `cursor:`/`codex:` prefix to pass `is_generic_usage_row`    | `[ ]`  |
| ChatGPT/Work agent usage split                      | CH rows with urn `chatgpt:usage:metrics`, hook `chatgpt`/`chatgpt-work`                                                 | `[ ]`  |
| Team members / facepile                             | PG `users` + `organization_user_relationships` (demo org has NO memberships by design — check page under impersonation) | `[ ]`  |
| Directory dimensions (department/title/cost center) | PG `directory_users`/`directory_groups` + `user.attributes.*` on CH rows                                                | `[ ]`  |
| Toolsets / MCP pages                                | PG `toolsets`, `mcp_servers`, deployments                                                                               | `[ ]`  |
| Shadow MCP inventory                                | CH `shadow_mcp_inventory_urls`                                                                                          | `[ ]`  |
| Access challenges                                   | CH `authz_challenges`                                                                                                   | `[ ]`  |
| Risk overview (CH mirror)                           | CH `risk_findings`                                                                                                      | `[ ]`  |
| Skills pages                                        | PG `skills`, `skill_versions`, suggestions                                                                              | `[ ]`  |

## Rules when extending

1. New page = new row here + seed data + a `verify.md` check, in the same
   commit.
2. All ids derive from the fixed constants in `postgres.sql` (org
   `org_gram_demo_workspace`, projects `dec0de00-…0001`/`…0002`); chat ids are
   `md5('gram-demo-chat-' || n)` in BOTH stores.
3. Timestamps are always relative to `now()`, trailing ≤ 12 days, so every
   chat clears the MV date cutoffs, the daily prod rerun keeps data fresh, and
   MVs populate on INSERT — never hand-backfill an MV target.
4. Every new CH insert target (or new MV) gets a matching scoped DELETE in
   `clickhouse.sql`, or re-runs double the numbers.
