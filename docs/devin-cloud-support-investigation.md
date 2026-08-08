# Investigation: Native support for Devin (Cognition) Cloud agents

**Status:** Investigation only (DNO-395). No implementation in this document.
**Question:** Can Gram ingest Devin/Cognition telemetry — cost, usage, sessions, and
security signals — the way it ingests Claude Code today?
**Deadline for an answer:** 2026-07-15.

## TL;DR

Yes, Devin telemetry can be supported — but **not through the same mechanism as Claude
Code**, and it is **not zero-effort**.

- Gram's Claude Code integration is a **push** model: Claude Code emits OpenTelemetry
  (OTLP) logs/metrics carrying tokens + dollar cost, and Gram receives them at
  `/rpc/hooks.otel/v1/*`. **Devin Cloud has no documented OTEL emission and no outbound
  webhooks.**
- Devin's only telemetry surface for the **Cloud** agent (the ~95% of Bilt's workload)
  is its **REST API**, which must be **polled**. It exposes per-session and
  per-user/day usage, session status, and PR outcomes.
- **Cost is denominated in ACUs (Agent Compute Units), not tokens or dollars.** This
  does not map 1:1 onto Gram's existing token/cost model and needs a new usage
  dimension.
- Devin's **CLI hooks are Claude-Code-compatible** and map almost 1:1 onto Gram's
  existing canonical hook event model — but hooks only fire for **local/terminal**
  sessions, not Cloud, and their payloads carry **no cost, no tokens, and no
  session/user IDs**. So hooks help with security signals for CLI usage but cannot
  cover Cloud cost/usage.

**Net:** Supporting Devin Cloud requires **net-new pull-based ingestion infrastructure**
(an API poller + ACU usage model), not just a new adapter behind the existing endpoints.
This is feasible and reuses Gram's storage/query/attribution layers, but it is a
genuine build, and a few things still need confirmation from Cognition (see
[Open questions](#open-questions-to-confirm-with-cognition)).

---

## 1. How Gram ingests Claude Code today (the baseline)

Two pathways, both **push** from the agent to Gram:

### A. OTEL / OTLP push — carries cost + usage (the important one)
- Endpoints: `POST /rpc/hooks.otel/v1/logs` and `/rpc/hooks.otel/v1/metrics`
  (`server/design/hooks/design.go`, handler `server/internal/hooks/otel.go`).
- Claude Code, configured with `CLAUDE_CODE_ENABLE_TELEMETRY=1` + standard `OTEL_*`
  env vars, exports gzip'd OTLP JSON directly to Gram.
- Payloads carry `gen_ai.usage.{input,output,cache_read,cache_creation}_tokens`,
  `gen_ai.usage.cost` (USD), model, and `gen_ai.conversation.id`.
- Sessions are attributed (team vs personal, org, billing mode) and rows land in
  ClickHouse `telemetry_logs`, then roll up into `metrics_summaries`,
  `chat_token_summaries`, etc.

### B. Hook events — control-flow + behavioral/security signals
- Per-provider endpoints (`/rpc/hooks.claude`, `/rpc/hooks.cursor`, `/rpc/hooks.codex`)
  and a unified `/rpc/hooks.ingest`.
- Providers are normalized to a **canonical event model** in
  `server/internal/hookevents/` (`event.go`) via thin adapters in
  `server/internal/hookevents/adapters/{claude,cursor,codex}/`. A Cursor adapter is
  ~73 lines (`adapters/cursor/events.go`).
- Canonical event types already include `session_start`, `before/after_tool_use`,
  `permission_request`, `user_prompt_submit`, `stop`, `session_end`, etc.
- The ingest handler can return allow/deny decisions (prompt-injection, tool blocking,
  shadow-MCP access).

**Key point:** cost/usage for Claude Code comes from pathway **A (OTEL push)**. Pathway
B carries behavior/security, not reliable cost. Devin breaks pathway A.

---

## 2. What Devin (Cognition) actually exposes

Confidence is high on the API/hook findings (Devin's official docs); the genuine unknown
is native OTEL support. Full source list at the bottom.

### 2.1 REST API — the strongest surface (pull only)
- Base `https://api.devin.ai/`, scopes `v3/organizations/*` and `v3/enterprise/*`.
  Auth via service tokens (`cog_*`) or enterprise admin keys (`apk_user_*`).
- **Sessions (usage/cost live here):** list `GET /v3beta1/enterprise/sessions`, detail
  `GET .../sessions/{devin_id}`. Session objects carry **`acus_consumed`**, `status`,
  `user_id`, `org_id`, `created_at/updated_at`, `pull_requests` (state + url),
  `parent/child_session_ids`, tags, title, url. A v2 "insights" variant adds
  `session_analysis` (action items, issues w/ impact, timeline).
- **Consumption:** per-user daily ACU at
  `GET /v3/organizations/{org_id}/consumption/daily/users/{user_id}` →
  `total_acus` + `consumption_by_date[]` with `acus_by_product`
  (`devin`, `cascade`, `terminal`, `review`). Sibling endpoints exist for
  cycles / daily sessions / service users. Daily granularity (midnight PST).
- **Org usage metrics:** `GET /v3/enterprise/organizations/{org_id}/metrics/usage` →
  counts only (`sessions_count`, `searches_count`, `prs_created/merged_count`);
  **no ACU/token/cost here**.
- **Cost unit:** **ACUs only** — no token counts and no dollar figures anywhere in the
  API. (~1 ACU ≈ 15 min active work; ACU→$ depends on plan/overage.)

### 2.2 CLI hooks — Claude-Code-compatible, but local + thin
- Devin CLI reads hooks from `.devin/hooks.v1.json`, `.devin/config.json`, and even
  existing `.claude/settings.json`. Same JSON format as Claude Code.
- Events: `PreToolUse`, `PostToolUse`, `PermissionRequest`, `UserPromptSubmit`,
  `Stop`, `SessionStart`, `SessionEnd`, `PostCompaction`.
- No native HTTP sink — a `command` hook gets event JSON on stdin and can `curl`
  it out.
- **Payload limits:** tool name/input/response, prompt, session source, stop flag,
  compaction summary — **but no cost, no tokens, and no session/user IDs.**

### 2.3 Enterprise / admin
- **Audit logs:** `GET /v2/enterprise/audit-logs` (+ v3 variants), admin key required,
  cursor pagination. **But the documented schema guarantees only `audit_log_id` with
  `additionalProperties: true`** — event types are not enumerated (secrets access,
  logins, permission changes neither confirmed nor denied).
- **No SIEM integration, no audit/log streaming, no bulk/CSV/object-store export**
  documented. Everything is pull.

### 2.4 OTEL — no evidence of native support
- No Devin/Cognition docs describe OTEL/OTLP emission. Because the CLI is
  Claude-compatible for *hooks*, it's *plausible* it honors some `OTEL_*` env vars, but
  this is **undocumented — must be tested empirically**, not assumed.

### 2.5 Cloud vs Desktop/CLI
- Post-rebrand, "Devin" spans Cloud (autonomous, Cognition infra), Desktop (ex-Windsurf),
  CLI/Terminal, and Review. `acus_by_product` lets us isolate **Cloud** (`devin`)
  consumption. The REST API is the Cloud surface; hooks are the local/CLI surface.
- Session endpoints are still `v3beta1` → API-stability risk.

---

## 3. Gap analysis: Claude Code vs Devin Cloud

| Capability | Claude Code (today) | Devin Cloud | Mechanism change |
| --- | --- | --- | --- |
| Cost / usage | OTEL **push**, tokens + USD | REST **poll**, **ACUs** | New poller + ACU model |
| Sessions | Derived from OTEL conversation id | REST sessions endpoint (rich: status, PRs, analysis) | New poller |
| Security / behavior | Hook push (per-tool, permission) | CLI hooks (**local only**), + undocumented audit log | Reuse hook model (CLI); audit log TBD |
| Delivery model | Push (OTEL + hooks) | **Pull (polling)** — no OTEL, no outbound webhooks | Fundamental |
| Cost unit | tokens → USD | **ACU** (→ USD depends on plan) | New dimension |

---

## 4. Feasibility per requested signal

- **Cost & usage — feasible, ACU-based.** Poll consumption + session endpoints. Gives
  per-session and per-user/day ACU, and PR outcomes. Requires an ACU usage dimension
  alongside the existing token/USD model; ACU→$ conversion needs plan/rate info.
- **Sessions — feasible and rich.** The sessions API arguably exposes *more* than Claude
  Code (explicit status lifecycle, PR links, session analysis).
- **Security signals — partial.** CLI hooks give command/tool-execution + permission
  decisions and map cleanly onto Gram's canonical hook model — but **only for CLI/local
  sessions, not Cloud**, and without session/user IDs. Cloud security telemetry depends
  on the audit-log endpoint, whose event coverage is undocumented and must be validated
  against a live enterprise account.
- **Real-time / push — not available.** No OTEL, no outbound webhooks. Expect
  polling latency (consumption is daily granularity; sessions can be polled more often).

---

## 5. Recommended approach (if we proceed)

Build a **Devin API poller** as net-new ingestion, reusing Gram's existing storage,
attribution, and query layers:

1. **Poller service** authenticating with an enterprise admin key, periodically pulling
   `sessions` + `consumption` (and `audit-logs` if it proves useful). Normalize into
   Gram's telemetry model with `provider: "devin"` and a new **ACU usage** field.
2. **ACU as a first-class usage unit** in the telemetry schema/summaries, with optional
   ACU→USD conversion driven by plan config.
3. **Reuse account attribution** — map Devin `user_id` → Gram org member.
4. **CLI hooks (optional, later)** — a ~small `adapters/devin/` adapter behind
   `/rpc/hooks.ingest` for local Devin CLI sessions, mirroring the Cursor adapter. Adds
   command/permission security signals for terminal usage. Does **not** cover Cloud
   cost/usage.

Suggested phasing: **Phase 1** sessions + ACU consumption polling (covers the Cloud
cost/usage/session ask). **Phase 2** audit-log ingestion for Cloud security signals
(pending schema validation). **Phase 3** CLI hook adapter for local security signals.

---

## 6. Open questions (to confirm with Cognition)

These gate scope and should be raised with Cognition (David's action item) before
committing to a POC:

1. **Does Devin Cloud emit OTEL, or honor `OTEL_*` env vars?** If yes, a push path could
   dramatically simplify the integration. Currently undocumented — needs empirical test.
2. **Audit-log event catalog:** what event types does `audit-logs` actually return
   (secrets access, permission changes, logins, command execution)? Schema is
   `additionalProperties: true`. Validate against a real enterprise tenant.
3. **ACU → USD:** what conversion / rate data is available via API or contract, so we can
   present dollar cost like we do for Claude Code?
4. **`v3beta1` stability / GA timeline** for the session endpoints.
5. **Rate limits / polling frequency** allowed on sessions + consumption endpoints
   (affects freshness).
6. **Enterprise auth provisioning** — can Bilt issue an `apk_user_*` / service token
   scoped to analytics for Gram to poll?
7. **Any outbound webhook / streaming on the roadmap?** (Would remove the need to poll.)

---

## Sources

Codebase: `server/design/hooks/design.go`, `server/internal/hooks/otel.go`,
`server/internal/hooks/ingest_hooks.go`, `server/internal/hookevents/event.go`,
`server/internal/hookevents/adapters/{claude,cursor,codex}/`,
`docs/unified-observability-plan.md`.

Devin / Cognition docs:
- https://docs.devin.ai/cli/extensibility/hooks/overview
- https://docs.devin.ai/cli/extensibility/hooks/lifecycle-hooks
- https://docs.devin.ai/api-reference/overview
- https://docs.devin.ai/api-reference/v3/sessions/enterprise-sessions
- https://docs.devin.ai/api-reference/v2/sessions/list-enterprise-sessions-insights
- https://docs.devin.ai/api-reference/v3/consumption/organizations-consumption-daily-users
- https://docs.devin.ai/api-reference/v3/metrics/organizations-metrics-usage
- https://docs.devin.ai/api-reference/v2/audit-logs
- https://docs.devin.ai/llms.txt
- Claude Code OTEL baseline: https://code.claude.com/docs/en/agent-sdk/observability
