# Shadow MCP Redis Access Store Design

## Goal

Ship the Shadow MCP alpha flow soon without the SQL access-control migration.
The dashboard and blocked-to-request-access flow should stay intact while the
backing store changes from Postgres tables to Redis. Future RBAC work will
replace this backing store, so the implementation should avoid baking Shadow
MCP into generic access-rule concepts.

## Current Stack

The existing stack has five branches:

1. Schema and migrations for access-control tables.
2. Shadow MCP access API and generated SDK.
3. Runtime enforcement.
4. Request-link flow.
5. Dashboard UI.

The schema/migration PR has been closed and should be removed from the stack.
The remaining PRs should be reshaped, not replaced with a brand-new stack.

## Backend Design

Keep the management API and dashboard flow as the alpha contract. Replace the
SQL repository calls behind those endpoints with a Redis-backed access store.

Use generic internal names:

- `AccessRule`
- `AccessApprovalRequest`
- `AccessRuleStore`
- `AccessRequestStore`
- `resource_type`
- `match_kind`
- `match_value`
- `disposition`

Use `shadow_mcp` only as the current `resource_type`, and in UI/API labels
that specifically describe the Shadow MCP feature.

The Redis store should model:

- approval requests
- access rules
- request status: `requested`, `approved`, `denied`
- generated IDs
- timestamps
- requester and decision metadata needed by the current dashboard
- observed server metadata needed to render review sheets and rule tables

Treat this data as best-effort alpha state. Use long TTLs and idempotent writes,
but do not try to provide audit-grade durability or SQL-equivalent history in
Redis.

## Data Flow

1. A Shadow MCP call is blocked by policy.
2. The hook response includes a request-access link.
3. The user opens the link and submits an approval request.
4. An admin reviews the request in Access -> Shadow MCP.
5. The admin approves or denies the request.
6. Approval creates an `AccessRule` with `resource_type = "shadow_mcp"` and
   `disposition = "allowed"`.
7. Runtime enforcement checks Redis access rules for `shadow_mcp` before
   blocking future matching calls.

Deny should remain a request decision for the alpha unless the existing UI
requires deny-rule creation. Avoid adding Redis deny-rule semantics unless they
are needed for the current release.

## Redis Scope

The Redis store only needs enough indexing for the current UI:

- organization
- resource type
- request status
- project, where the UI filters by project
- timestamp ordering for list views

Do not emulate SQL cursor and indexing behavior beyond the dashboard's current
needs. If pagination is needed, use simple timestamp and ID ordering.

The existing Shadow MCP Redis approval behavior can guide canonicalization and
TTL choices, but the new store should be generic enough for future access-rule
resource types.

## PR Stack

Reshape the existing PRs:

1. Drop the schema/migration PR.
2. Reuse the current API PR for the generic Redis access store and Shadow MCP
   API facade. If it is too large, split store and facade into separate commits
   inside the same PR first.
3. Keep the runtime enforcement PR, but point it at generic Redis access rules
   instead of SQL access rules.
4. Keep the request-link PR as the request-link and request-submission flow,
   backed by Redis requests.
5. Keep the dashboard PR as the current UI, with only necessary API wiring
   changes.

The resulting stack should be:

```text
api/store facade -> runtime enforcement -> request links -> dashboard
```

## Testing

Add focused backend tests for:

- Redis CRUD for requests and rules
- TTL behavior at the store boundary
- idempotent request submission and approval
- filtering by organization, resource type, status, and project
- canonical matching behavior for Shadow MCP allow rules
- runtime enforcement allowing a previously approved match

Keep dashboard tests focused on preserving the existing UI flow:

- pending requests render
- approve creates an allow rule
- deny marks a request denied
- existing rule table interactions still work
- request-access page submits a token-backed request

## Non-Goals

- No database migration for access-control tables.
- No durable audit-grade Redis history.
- No full RBAC design in this refactor.
- No dashboard redesign.
- No broad component split unless needed for reviewability.
