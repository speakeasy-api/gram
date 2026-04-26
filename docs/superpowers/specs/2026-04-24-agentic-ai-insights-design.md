# Agentic AI Insights — Design

**Date:** 2026-04-24
**Status:** Draft, pending user review
**Scope:** Backend service + frontend review UI + new observability MCP tools

## Context

The current AI Insights feature (`client/dashboard/src/components/insights-sidebar.tsx`) is a reactive chat: the user opens a sidebar, asks a question, and the model uses read-only observability MCP tools (`gram_search_logs`, `gram_list_tools`, etc.) to answer. It does not remember anything across sessions, cannot take actions, and does not spontaneously do deeper multi-step investigations.

Making it "agentic" means closing three loops:

1. **B — Deeper analysis on demand.** Encourage multi-step investigation (hypothesize → verify → report) with light structure.
2. **C1+C2 — Action-taking via proposals.** Let the agent propose edits to tool variations and toolset composition; never write directly. The user reviews a diff and clicks Apply.
3. **D — Workspace memory.** Persist facts, playbooks, and glossary entries across sessions; inject a relevant slice into the system prompt on each new session.

The unifying architectural idea: **add one new backend service (`insights`) with two durable tables (`insights_proposals`, `insights_memories`), expose its write-side endpoints as new tools on the existing observability MCP toolset, and add a small review UI in the dashboard.**

Ease of implementation and verification is the primary constraint. Every change should be small, pattern-consistent with existing services (variations, toolsets), and testable with the standard `testenv` harness.

## Architecture at a glance

```
┌─ Dashboard (React) ───────────────────────────────────────┐
│  InsightsSidebar ── Chat ── GramElementsProvider          │
│        │                                                  │
│        ├── ProposalsPanel (new, inline in sidebar)        │
│        └── MemoryPill     (new, inline in sidebar)        │
└───────────────┬───────────────────────────────────────────┘
                │ Management API (Goa, /rpc/insights.*)
┌───────────────▼───────────────────────────────────────────┐
│ server/internal/insights/                                 │
│   impl.go          — service methods                      │
│   queries.sql      — SQLc queries                         │
│   repo/            — generated repo                       │
└───────────────┬───────────────────────────────────────────┘
                │ Wrapped by the new built-in MCP…
┌───────────────▼───────────────────────────────────────────┐
│ NEW built-in MCP: /mcp/ai-insights (first-party Gram)     │
│   Tools (six):                                            │
│     insights_propose_variation                            │
│     insights_propose_toolset_change                       │
│     insights_remember / insights_forget                   │
│     insights_recall_memory                                │
│     insights_record_finding                               │
│                                                           │
│ Existing observability MCP (speakeasy-team-gram, hosted)  │
│   Read-only: gram_search_logs, gram_list_tools, …         │
│   Stays where it is — unchanged.                          │
└───────────────────────────────────────────────────────────┘
```

**Key architectural decision (revised 2026-04-24):** the new agentic write tools ship as a **first-party built-in MCP server** at `/mcp/ai-insights` rather than being added to the existing observability toolset. The observability MCP is a customer toolset hosted by the speakeasy-team workspace; mixing platform-write actions into a customer toolset creates a tenancy story that's correct mechanically but conceptually muddled. A built-in MCP gives us cleaner read/write separation, default availability without customer-side dependency, native Gram-platform RBAC, and a clean home for future agentic capabilities.

The Insights sidebar configures **both** MCPs — observability for reads, ai-insights for writes — using multi-MCP support in `@gram-ai/elements`. `apply` and `rollback` are intentionally **not** exposed as MCP tools; those are human-only actions reachable from the dashboard UI.

## Data model

Two new tables in `server/database/schema.sql`:

### `insights_proposals`

| Column                                                                 | Type             | Notes                                                                                                                           |
| ---------------------------------------------------------------------- | ---------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `id`                                                                   | `uuid` PK        |                                                                                                                                 |
| `project_id`                                                           | `uuid` FK        | Scope everything per project.                                                                                                   |
| `kind`                                                                 | `text`           | `tool_variation` or `toolset_change`.                                                                                           |
| `target_ref`                                                           | `text`           | Tool name (for variations) or toolset slug (for toolsets).                                                                      |
| `current_value`                                                        | `jsonb`          | Snapshot of the resource state at proposal time. Used for staleness detection on apply, **and as the rollback target.**         |
| `proposed_value`                                                       | `jsonb`          | The proposed new state.                                                                                                         |
| `applied_value`                                                        | `jsonb` nullable | The value actually written at apply time (normally equals `proposed_value`; differs if a future "edit then apply" UX is added). |
| `reasoning`                                                            | `text`           | The agent's justification. Shown in the review card.                                                                            |
| `source_chat_id`                                                       | `uuid` nullable  | Chat that produced the proposal.                                                                                                |
| `status`                                                               | `text`           | `pending`, `applied`, `dismissed`, `superseded`, `rolled_back`.                                                                 |
| `created_at`, `applied_at`, `dismissed_at`, `rolled_back_at`           | `timestamptz`    |                                                                                                                                 |
| `applied_by_user_id`, `dismissed_by_user_id`, `rolled_back_by_user_id` | `uuid` nullable  | The human who took the action.                                                                                                  |

**Indexes:** `(project_id, status, created_at DESC)` for listing pending proposals.

**Staleness:** On `applyProposal`, we compare the stored `current_value` against the live value. If they differ, we set `status='superseded'` and return a conflict to the UI so the user decides whether to override.

### `insights_memories`

| Column                                     | Type            | Notes                                                                                                          |
| ------------------------------------------ | --------------- | -------------------------------------------------------------------------------------------------------------- |
| `id`                                       | `uuid` PK       |                                                                                                                |
| `project_id`                               | `uuid` FK       |                                                                                                                |
| `kind`                                     | `text`          | `fact`, `playbook`, `glossary`, `finding`. `finding` is used by the investigation scratchpad and auto-expires. |
| `content`                                  | `text`          | Short, one-line-ish. Enforced max 2000 chars.                                                                  |
| `tags`                                     | `text[]`        | Agent-chosen tags used for recall.                                                                             |
| `source_chat_id`                           | `uuid` nullable |                                                                                                                |
| `created_at`, `last_used_at`, `expires_at` | `timestamptz`   | `expires_at` nullable; used for findings.                                                                      |
| `usefulness_score`                         | `int` default 0 | Incremented when the memory is recalled into a system prompt. Cheap signal for dedup/pruning.                  |

**Indexes:** `(project_id, kind, last_used_at DESC)`, GIN on `tags`.

## Backend service

New service at `server/internal/insights/` with design at `server/design/insights/design.go`. Methods, all under `/rpc/insights.*`:

| Method                  | Purpose                                                                                                                                                                                                                                                            |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `proposeToolVariation`  | Agent submits a proposed variation edit. Validates tool exists, snapshots current variation value, inserts row with `status=pending`.                                                                                                                              |
| `proposeToolsetChange`  | Same, but target is a toolset + a change payload (`add_tools`, `remove_tools`, `rename`).                                                                                                                                                                          |
| `listProposals`         | UI: list pending proposals with diffs precomputed for render.                                                                                                                                                                                                      |
| `applyProposal`         | UI: atomically apply the proposed change. Calls the existing `variations.upsertGlobal` or `toolsets.updateToolset` internally. Audit-logged. Staleness-checked.                                                                                                    |
| `rollbackProposal`      | UI: revert an `applied` proposal by writing `current_value` back to the resource. Staleness-checked (compares live state to `applied_value`; if drifted, returns conflict and the UI shows a confirm-overwrite prompt). Sets `status='rolled_back'`. Audit-logged. |
| `dismissProposal`       | UI: mark dismissed. Audit-logged.                                                                                                                                                                                                                                  |
| `forgetMemoryById` (UI) | UI version of forget — used by the History tab to "roll back" memories the agent created. Audit-logged.                                                                                                                                                            |
| `rememberFact`          | Agent writes a memory.                                                                                                                                                                                                                                             |
| `forgetMemory`          | Agent or user deletes a memory.                                                                                                                                                                                                                                    |
| `listMemories`          | UI + agent recall. Supports `kind` and `tags` filters.                                                                                                                                                                                                             |
| `recordFinding`         | Agent writes a short bullet during an investigation. Sugar over `rememberFact` with `kind='finding'` and `expires_at = now() + 7d`.                                                                                                                                |

Reuse existing patterns:

- `conv.PtrToPGText` / `conv.FromPGText` for nullable columns
- `access.Manager.Require` for scope enforcement
- `auditlogs` recording on every apply/dismiss
- `oops` for error wrapping
- Unit tests per method (see `server/internal/variations/*_test.go` for the template)

### RBAC (new scopes)

| Scope              | Principal            | Enforced in                                                                                                |
| ------------------ | -------------------- | ---------------------------------------------------------------------------------------------------------- |
| `insights:propose` | Chat session (agent) | `proposeToolVariation`, `proposeToolsetChange`, `rememberFact`, `recordFinding`, `forgetMemory` (own only) |
| `insights:read`    | Any project member   | `listProposals`, `listMemories`                                                                            |
| `insights:apply`   | Editor+              | `applyProposal`, `dismissProposal`, `rollbackProposal`, `forgetMemoryById` (UI)                            |

The agent (chat session) gets `insights:propose` + `insights:read` automatically. Applying _and rolling back_ are human actions — the agent can never apply or roll back on its own.

## MCP tools (new built-in `ai-insights` MCP server)

A new first-party MCP server at `/mcp/ai-insights` exposes six tools, each a thin wrapper over a `/rpc/insights.*` endpoint. The server is implemented in Go alongside Gram's existing MCP serving code (`server/internal/mcp/`).

- `insights_propose_variation` — args: `tool_name`, `proposed_description?`, `proposed_summary?`, `proposed_hint?`, `reasoning`. Calls `proposeToolVariation`.
- `insights_propose_toolset_change` — args: `toolset_slug`, `add_tools[]`, `remove_tools[]`, `new_name?`, `reasoning`. Calls `proposeToolsetChange`.
- `insights_remember` — args: `content`, `kind` (`fact`|`playbook`|`glossary`), `tags[]`. Calls `rememberFact`.
- `insights_forget` — args: `memory_id`. Calls `forgetMemory`.
- `insights_recall_memory` — args: `query`, `kind?`, `tags?`. Calls `listMemories` with ranking.
- `insights_record_finding` — args: `content`, `tags?`. Calls `recordFinding`.

**Authentication:** the MCP server requires the caller to be an authenticated Gram principal (session cookie or API key). Per-tool RBAC is enforced by the underlying `/rpc/insights.*` handlers.

**Discovery:** the MCP server speaks standard MCP protocol (`initialize`, `tools/list`, `tools/call`). The dashboard's Insights sidebar mounts it as a second MCP source alongside observability.

**What's intentionally NOT exposed as MCP tools:** `applyProposal`, `rollbackProposal`, `dismissProposal`, `forgetMemoryById` — these are human-only actions reached through the `ProposalsPanel` UI calling `/rpc/insights.*` directly. The agent must never apply or roll back its own proposals.

Tool descriptions live in Go alongside the handlers — that's where the "agentic behavior" is actually authored, not in a customer toolset.

## System-prompt augmentation

`InsightsProvider` in `insights-sidebar.tsx` currently builds `baseInstructions` as a static string. We extend it with two server-fetched slices:

1. **Memory slice** — on sidebar open, fetch up to 20 most-recently-used memories for the project via `listMemories`. Render them as `<workspace_memory>` bullets at the top of the system prompt. Bumps `last_used_at` via a side-effecting read.
2. **Investigation protocol** — a static paragraph appended to the system prompt telling the agent how to investigate:

   > When asked to diagnose an issue, follow this loop: (1) form a single hypothesis, (2) gather evidence with read tools, (3) record what you learned via `gram_record_finding`, (4) if evidence points to a tool or toolset fix, call `gram_propose_tool_variation` or `gram_propose_toolset_change` with a clear `reasoning`. Do NOT apply proposals yourself; the human will review.

This is where **B** comes from — no new orchestration, just explicit structure in the prompt plus the new tools that reward following it.

## Frontend (dashboard)

Two additions inside the sidebar panel, above the `<Chat />` component:

### `ProposalsPanel`

- Fetches pending proposals via `listProposals`
- Collapsed by default; shows a counter badge (`3 pending`)
- **Two tabs:** `Pending` and `History`
- Each card shows:
  - Kind (tool variation / toolset change / memory remembered) + target name
  - Diff: `before` vs `after` rendered via a small diff component (reuse the one used in audit log detail — see `project_audit_log_redesign.md`)
  - Reasoning (collapsed)
  - **Pending tab actions:** `Apply`, `Dismiss`
  - **History tab actions (when `status='applied'`):** `Roll back` — confirms with a small modal showing what will be written back. If the live state has drifted from `applied_value`, the modal shows an "**This has changed since you applied. Overwrite anyway?**" warning and a side-by-side preview of the divergence.
  - **History tab — memory rows:** `Forget` button (calls `forgetMemoryById`)
- All actions optimistic-update the local list.

### `MemoryPill`

- Fetches up to 5 most-recently-used memories
- Renders as a small chip strip ("Remembering: API v3 migration, customer 'Globex'…")
- Clicking opens a full list where the user can remove stale entries

Both components live under `client/dashboard/src/components/insights/` (new directory) and are lazy-mounted when the sidebar opens.

## Verification plan

End-to-end loop (manual):

1. `mise start:server --dev-single-process` + `cd client/dashboard && pnpm dev`
2. `mise seed` to ensure observability MCP is wired
3. Open the dashboard, open AI Insights
4. Prompt: _"The `create_invoice` tool is failing a lot — see if its description is confusing users."_
5. Expect: agent calls `gram_search_logs` and/or `gram_search_tool_calls`, records a finding, then calls `gram_propose_tool_variation`.
6. Proposals panel badge should go to `1 pending`. Open it, confirm the diff renders.
7. Click **Apply**. Verify via `gram_list_global_variations` that the variation is now updated, and an `auditlogs` entry exists.
8. Open a new session tomorrow: confirm the memory ("customer X has recurring issues with create_invoice") appears in the `MemoryPill`.

Automated:

- Go unit tests per new `insights` method, modeled on `server/internal/variations/upsertglobal_test.go`.
- Integration test using `testenv` that drives the full `propose → list → apply` loop with a fixture tool/toolset.
- Staleness test: mutate the variation after `propose` but before `apply` and assert the proposal is marked `superseded`.
- **Rollback test:** apply a proposal, assert the resource has `proposed_value`, then call `rollbackProposal`, assert the resource is back to `current_value` and the proposal status is `rolled_back`. Audit log has both apply and rollback entries.
- **Rollback drift test:** apply, then mutate the resource externally, then attempt rollback without the `force` flag — assert it returns a conflict. Repeat with `force=true` and assert rollback succeeds.
- RBAC test: assert that a chat-session principal cannot call `applyProposal` or `rollbackProposal`.

Dashboard:

- `cd client/dashboard && pnpm tsc -p tsconfig.app.json --noEmit`
- `cd client/dashboard && pnpm build`
- Playwright smoke (optional): open sidebar, stub an MCP response that creates a proposal, assert the badge appears and Apply succeeds.

## Critical files

**New**

- `server/database/schema.sql` — add two tables
- `server/migrations/YYYYMMDDHHMMSS_insights.sql`
- `server/design/insights/design.go`
- `server/internal/insights/impl.go`, `queries.sql`, `repo/`
- `server/internal/insights/*_test.go` (one per method)
- `client/dashboard/src/components/insights/ProposalsPanel.tsx`
- `client/dashboard/src/components/insights/MemoryPill.tsx`

**Modified**

- `client/dashboard/src/components/insights-sidebar.tsx` — wire ProposalsPanel + MemoryPill, extend system prompt
- `server/cmd/...` — register insights service on the Goa server
- `server/internal/access/` — add three new scopes + default role bindings
- `server/internal/auditlogs/` — add action types `insights.proposal.applied`, `insights.proposal.dismissed`, `insights.proposal.rolled_back`, `insights.memory.forgotten`

**New built-in MCP server (in-repo)**

- `server/internal/aiinsights/mcp.go` — JSON-RPC 2.0 handlers for `initialize` / `tools/list` / `tools/call`
- `server/internal/aiinsights/tools.go` — Go-side definitions of the six tools (name, description, input schema, dispatch function)
- `server/cmd/gram/start.go` — register the MCP at `/mcp/ai-insights`

## Skills to activate during implementation

- `gram-management-api` — designing new Goa methods / service wiring
- `golang` — service impl
- `postgresql` — schema + migration + SQLc queries
- `gram-rbac` — new scopes + enforcement
- `gram-audit-logging` — auditing `applyProposal` / `dismissProposal`
- `frontend` — React components
- `vercel-react-best-practices` — for memoization of ProposalsPanel since it lives in a hot path

## Intentional non-goals (YAGNI)

- **No vector embeddings for memory recall.** Tag + recency ranking is enough for v1. Revisit if users complain that recall misses obvious matches.
- **No approval queue / multi-reviewer workflow.** Single-click Apply by any editor. Audit log is the authority.
- **No proactive/scheduled runs.** Re-read of the original brainstorm: user picked B + C + D, not A. Scheduled insights are a future feature.
- **No sub-agents / multi-agent orchestration.** The system prompt + new tools carry all the "agentic" behavior. Adding an orchestrator is expensive and not required for the first cut.
- **No environment or deployment actions.** Out of C1/C2 scope by explicit user decision.

## Resolved decisions (from user, 2026-04-24)

1. **`proposeToolsetChange` scope:** add/remove/rename — no reorder.
2. **Memory pruning:** auto-expire `fact` and `finding` after 90d of no use; `playbook` and `glossary` never expire.
3. **Action permissions:** any project member can dismiss; editors+ apply / roll back.
4. **History tab:** include applied, dismissed, rolled-back proposals **and** agent-created memories.
5. **Memory scope:** per-project for v1.
6. **Rollback for any agent mutation:** every applied proposal has a `Roll back` button in the History tab; every agent-created memory has a `Forget` button in the History tab. Rollback uses the `current_value` snapshot taken at proposal time. If the live resource state has drifted from `applied_value` since apply, the rollback flow shows a confirm-overwrite prompt rather than blindly restoring.

## Implementation order (for the follow-on plan)

1. Migration + schema + SQLc
2. Goa design + gen
3. `insights/impl.go` read-only methods (`listProposals`, `listMemories`)
4. `insights/impl.go` write methods (`propose*`, `remember*`)
5. `applyProposal` + audit + RBAC
6. `rollbackProposal` + audit + drift handling
7. Build the new built-in `ai-insights` MCP server (`server/internal/aiinsights/`) wrapping the six write tools
8. Frontend: `ProposalsPanel` (Pending tab)
9. Frontend: `ProposalsPanel` (History tab + Roll back / Forget buttons + drift modal)
10. Frontend: `MemoryPill` + system-prompt wiring
11. Integration tests (apply + rollback + drift) + manual end-to-end verification
