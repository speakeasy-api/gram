# Instruction Tool — design

Date: 2026-07-19
Status: approved design, pending implementation plan

## Problem

Tool schemas must be valid for every consumer of an API, so they cannot carry
org-specific knowledge: how *this* org identifies a customer, which workflows
require verification after a write, which Jira/Grafana/Salesforce conventions
apply. Agents are left to guess, and the failure mode is "agent messed up
without anything explicitly erroring."

Gram already stores per-server guidance in `mcp_metadata.instructions` and
returns it in the MCP `initialize` response, but many MCP clients ignore that
field. Guidance that is not reliably read does not prevent mistakes.

## Solution overview

Add a synthetic **`instructions` tool** to every MCP server hosted or proxied
by Gram, and (by default) **gate** each MCP session: the first tool call in a
session must be `instructions`; any other call is answered with the
instructions text and a note to retry, instead of being executed. The content
is the existing `mcp_metadata.instructions` blob — one field, two delivery
surfaces (`initialize` and the tool). The `initialize` behavior is unchanged.

## Decisions (validated with Sagar)

- Content source: reuse `mcp_metadata.instructions`. No new content field.
- Presence: tool listed on every Gram MCP server by default, hosted and
  proxied, static and dynamic mode.
- Enforcement: per-session gate; first call must be `instructions`.
- The gate arms only when instructions are non-empty. Unconfigured servers
  behave exactly as today (tool listed, no gate).
- Config: one enum, `instruction_tool_mode` = `disabled` | `optional` |
  `required`, default `required`.
- Tool name: plain `instructions`. If a real upstream tool already uses that
  name, Gram skips injection (never shadows a customer tool).
- Approach: synthetic tool in the MCP serving layer (the existing
  `search_tools`/`describe_tools`/`execute_tool` pattern), not a platform
  tool, not materialized tool rows.

## Tool definition

- Name: `instructions`
- Input schema: empty object (no arguments)
- Annotations: `readOnlyHint: true`, `idempotentHint: true`
- Listed **first** in `tools/list`
- Description (call-first inducing), roughly:
  > Server usage guide for this MCP server. Returns organization-specific
  > conventions, required workflows, and verification steps for using the
  > other tools. Call this once before using any other tool.

### Call behavior

- Returns `mcp_metadata.instructions` as a text content block.
- When the field is empty/unset: returns "No instructions have been
  configured for this server. An administrator can add them in the Gram
  dashboard under Server Instructions."
- Calling it marks the session as gated-through (see below).

## Gate semantics

Applies when `instruction_tool_mode = required` AND instructions are
non-empty.

- State: a Redis flag via the existing `cache.TypedCacheObject` pattern
  (model: `AuthnChallengeState` in `server/internal/mcp/authnchallenge.go`).
  Key: `mcpSessionGate:<toolset-id>:<session-id>` (the serving path always
  resolves a toolset; metadata lookups are toolset-keyed there). TTL ~60
  minutes,
  refreshed on each `tools/call` so active sessions do not expire mid-use.
- In `handleToolsCall`, before proxy/static resolution:
  - `instructions` call → store the flag, return the content.
  - any other call without the flag → do **not** execute. Return a
    *successful* tool result containing the full instructions text followed
    by "Read the above, then retry your original call." One extra round
    trip, guaranteed read, no reliance on model cooperation. A JSON-RPC
    error is deliberately avoided: agents handle a content response more
    predictably than an error.
- Dynamic mode: the gate inspects the unwrapped tool name inside
  `execute_tool` calls, not just `params.Name`.

### Fail-open rules

Instructions must never break tool serving. The gate is skipped (tool still
listed; enforcement degrades to persuasion) when:

- the client never sent an `Mcp-Session-Id` header. Gram currently mints a
  fresh UUID per request for such clients
  (`server/internal/mcp/impl.go:1131-1137`), so they could never pass a
  session-keyed gate. A `sessionProvided` bool is added to the per-request
  `mcpInputs` struct, set where the header is parsed.
- Redis is unavailable (gate lookup/store errors are logged, call proceeds).
- instructions are empty, or mode is not `required`.
- metadata lookup fails during `tools/list` or `tools/call` (log; serve
  without the synthetic tool / without the gate).

## Data model and API

- Migration: `ALTER TABLE mcp_metadata ADD COLUMN instruction_tool_mode text
  NOT NULL DEFAULT 'required'` (values: `disabled`, `optional`, `required`),
  plus sqlc query updates in `server/internal/mcpmetadata/queries.sql`
  (both the toolset-keyed and mcp-server-keyed get/upsert paths).
- `mcpMetadata.get` / `mcpMetadata.set` RPCs
  (`server/design/mcpmetadata/design.go`,
  `server/internal/mcpmetadata/impl.go`): new optional `instruction_tool_mode`
  attribute; server default `required`.

## Serving-layer changes (`server/internal/mcp/`)

- New `instruction_tool.go`: tool definition (name, description, schema,
  annotations), the gate `CacheableObject`, the gated-response builder, and
  the injection/skip logic (mode check, empty check, collision check).
- `rpc_tools_list.go` (`buildToolListEntries`): after proxy `DoList` results
  and static tools are assembled, prepend the synthetic entry unless mode is
  `disabled` or a real tool is named `instructions`.
- `dynamic_tool_calling.go` (`buildDynamicSessionTools`): same injection for
  dynamic mode, alongside the three existing synthetic tools.
- `rpc_tools_call.go` (`handleToolsCall`): interception case for
  `instructions` plus the gate check, placed before proxy matching and the
  static name scan; unwraps `execute_tool`.
- `impl.go`: new `cache.TypedCacheObject[MCPSessionGate]` field constructed
  with the other typed caches; `sessionProvided` on `mcpInputs`.
- RBAC per-tool filter in `rpc_tools_list.go`: explicit allow for the
  synthetic name so private servers do not silently drop it.

## Dashboard (`client/dashboard`)

- **Server Instructions** section on the MCP server detail page
  (`src/pages/mcp/MCPDetails.tsx`) gains a three-way mode control:
  - Disabled — "The instructions tool is not listed."
  - Optional — "Agents see an instructions tool but are not required to
    call it."
  - Required — "Agents must read instructions before their first tool call
    in each session."
- Same `mcp:write` RBAC as the existing textarea; saved through
  `mcpMetadata.set` via `useMcpMetadataForm` alongside `instructions`.

## Telemetry

- PostHog event `mcp_instructions_gate_triggered` (fired when the gate blocks
  a call), with toolset/server, session id, and the blocked tool name. This
  measures how often agents skip reading — the success metric for the
  feature.

## Testing (`server/internal/mcp/servepublic_test.go` pattern)

- tools/list: `instructions` listed first on hosted and proxied servers; not
  listed when mode `disabled`; not listed on name collision; survives the
  RBAC filter on private servers.
- tools/call: returns configured text; returns not-configured message when
  empty.
- Gate: other tool → blocked response contains instructions + retry note →
  `instructions` called → same tool now executes. Flag TTL refresh on
  subsequent calls.
- Gate inactive: empty instructions, mode `optional`, missing
  `Mcp-Session-Id` header, Redis error (fail-open).
- Dynamic mode: `execute_tool`-wrapped calls are gated and unwrapped
  correctly; synthetic tool present alongside `search_tools` et al.

## Out of scope

- Multiple named instruction documents per server (per-task "skills").
- Changing the `initialize` instructions behavior.
- Client-side (dashboard playground) rendering of the gate.
- Backfilling or generating instructions content (the existing AI
  "Generate" button already covers authoring).
