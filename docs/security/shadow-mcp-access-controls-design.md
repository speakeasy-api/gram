# Shadow MCP access controls — implementation design

**Date:** 2026-05-11
**Status:** Draft
**Source RFC:** Notion `RFP: Shadow MCP Access Controls`
**Prototype reference branch:** `alexm/age-2208-feat-mock-frontend-for-admin-page`

## Goal

Make Shadow MCP access production-ready by expanding existing Risk Policy and Access systems without making role grants the runtime source of truth.

Risk Policy decides whether Shadow MCP usage is blocked or flagged. Shadow MCP Access Rules decide whether a matching server is explicitly allowed or denied when a blocking policy applies. Admin changes are gated by RBAC and audited.

## Non-goals

- Do not replace Risk Policy as the Shadow MCP block/flag source of truth.
- Do not auto-allow a Shadow MCP server when an admin creates a Gram-hosted MCP server.
- Do not use role grants or per-user grants for the initial Shadow MCP allow audience.
- Do not surface detailed block data to admins until the blocked user requests access.
- Do not model Shadow MCP runtime access with hosted MCP `mcp:connect` grants.

## Product model

### Approval request

An approval request is created after a blocked user clicks the request-access link. It exposes already-known block data to org admins.

States:

- `requested`
- `approved`
- `denied`

Requests are historical. Admins should be able to list all states, not only pending requests.

### Access Rule

An Access Rule is a managed Shadow MCP server entry. It can be `allowed` or `denied`.

Allowed rules apply to an audience of all users in the organization or all users in one project. Denied rules are enforcement entries and take precedence over allowed rules.

Match breadth:

- `full_url` — default when a URL is available.
- `url_host` — broader match for all endpoints on the host.
- `server_identity` — fallback for local, command-based, or non-URL MCP servers.

Audience:

- `organization` — all users in the organization.
- `project` — all users acting in the selected project.

## Data model

Exact DDL should follow the normal schema and migration process. Migrations must ship in a separate PR.

### `shadow_mcp_approval_requests`

Project-scoped request history with org context.

Suggested fields:

- `id uuid primary key`
- `organization_id text not null`
- `project_id uuid references projects(id) on delete set null`
- `requester_user_id text`
- `requester_email text`
- `requester_display_name text`
- `status text not null check in ('requested', 'approved', 'denied')`
- `risk_policy_id uuid`
- `risk_result_id uuid`
- `observed_name text`
- `observed_full_url text`
- `observed_url_host text`
- `observed_server_identity text`
- `tool_name text`
- `tool_call text`
- `block_reason text`
- `blocked_count int not null default 1`
- `first_blocked_at timestamptz`
- `last_blocked_at timestamptz`
- `requested_at timestamptz not null`
- `decided_at timestamptz`
- `decided_by text`
- `decision_note text`
- `created_at timestamptz not null default clock_timestamp()`
- `updated_at timestamptz not null default clock_timestamp() on update clock_timestamp()`
- `deleted_at timestamptz`
- `deleted boolean not null generated always as (deleted_at is not null) stored`

Request creation should be idempotent for the same requester, project, and observed server fingerprint while the request is still `requested`.

### `shadow_mcp_access_rules`

Org-owned managed server list with an explicit audience scope.

Suggested fields:

- `id uuid primary key`
- `organization_id text not null`
- `project_id uuid references projects(id) on delete cascade`
- `access_scope text not null default 'organization' check in ('organization', 'project')`
- `disposition text not null check in ('allowed', 'denied')`
- `match_breadth text not null check in ('full_url', 'url_host', 'server_identity')`
- `match_value text not null`
- `display_name text not null`
- `observed_full_url text`
- `observed_url_host text`
- `observed_server_identity text`
- `source_request_id uuid references shadow_mcp_approval_requests(id) on delete set null`
- `created_by text`
- `updated_by text`
- `reason text`
- `created_at timestamptz not null default clock_timestamp()`
- `updated_at timestamptz not null default clock_timestamp() on update clock_timestamp()`
- `deleted_at timestamptz`
- `deleted boolean not null generated always as (deleted_at is not null) stored`

Use a check constraint so organization-scoped rules have `project_id is null` and project-scoped rules have `project_id is not null`.

Use partial uniqueness guards for active rules:

- organization-scoped uniqueness by organization, match breadth, and match value
- project-scoped uniqueness by organization, project, match breadth, and match value

If a future use case needs both allow and deny at the exact same match and audience, require an explicit product decision because deny precedence will make the allow ineffective.

### Runtime audience

Do not add a separate role-to-rule join table for v1.

Do not use `principal_grants` to authorize runtime Shadow MCP access in the initial implementation. Runtime audience is stored directly on the Access Rule:

- `access_scope = 'organization'` means any user in the organization may use the matching server when the rule is allowed.
- `access_scope = 'project'` means any user acting in that project may use the matching server when the rule is allowed.

RBAC still gates the management API:

- `org:admin` can review requests and mutate rules.
- `org:read` can list managed rules.

If a later product version needs granular user or role audiences, add that as a separate audience model after the org/project audience is proven. Do not keep unused `shadow_mcp:connect` plumbing in v1, because role grants will look like they control access even though the runtime decision is rule-policy based.

### Superseded RBAC-grant approach

The first implementation path attempted to model allowed Shadow MCP runtime access as `shadow_mcp:connect` grants on roles, with Access Rules acting as grant-backed resources. We moved away from that design because it split the source of truth: the admin page presented Access Rules, while the runtime decision actually depended on RBAC grants and role editor state.

The v1 implementation intentionally removes that grant plumbing. RBAC now only protects management operations. Shadow MCP runtime allow/deny is determined by Access Rules directly, scoped to all users in the organization or all users in a project.

Management operations are session-only for v1. API keys are intentionally excluded from the Shadow MCP request/rule management endpoints because API-key authorization is scope-based (`producer`/`consumer`) and does not enforce the same `org:admin` RBAC checks as a dashboard session.

## Management API

Add methods to the existing `access` service because the admin surface belongs under Access and the operations are org-admin access-policy management.

Proposed route group:

- `GET /rpc/access.shadowMcp.requests.list`
- `POST /rpc/access.shadowMcp.requests.create`
- `POST /rpc/access.shadowMcp.requests.approve`
- `POST /rpc/access.shadowMcp.requests.deny`
- `GET /rpc/access.shadowMcp.rules.list`
- `POST /rpc/access.shadowMcp.rules.create`
- `PUT /rpc/access.shadowMcp.rules.update`
- `DELETE /rpc/access.shadowMcp.rules.delete`

### Request APIs

`listShadowMCPApprovalRequests`

- Auth: session-only, require `org:admin` because requests include requester and block details.
- Filters: `status`, `project_id`, `cursor`, `limit`.
- Returns all states.

`createShadowMCPApprovalRequest`

- Auth: session user.
- Input should be a signed request token or opaque block event ID from the block response.
- Creates or returns the active request for the observed server.
- Does not require org admin.

`approveShadowMCPApprovalRequest`

- Auth: session-only, require `org:admin`.
- Inputs: request id, access scope, match breadth, optional edited match value, admin note.
- Creates an `allowed` Access Rule when needed.
- For `access_scope = 'project'`, uses the request project as the rule project.
- For `access_scope = 'organization'`, creates an org-wide allow rule.
- Marks request `approved`.
- Runs in one transaction.

`denyShadowMCPApprovalRequest`

- Auth: session-only, require `org:admin`.
- Inputs: request id, admin note, `create_deny_rule` boolean, match breadth, optional edited match value.
- Marks request `denied`.
- If `create_deny_rule` is true, creates a project-scoped `denied` Access Rule for the request project.
- Runs in one transaction.

### Rule APIs

`listShadowMCPAccessRules`

- Auth: session-only, require `org:read` or `org:admin`.
- Filters: `disposition`, `access_scope`, `project_id`, `cursor`, `limit`.
- Returns the rule audience fields so the UI can show organization/project scope.

`createShadowMCPAccessRule`

- Auth: session-only, require `org:admin`.
- Inputs: disposition, access scope, optional project id for project-scoped rules, evidence fields, match breadth, match value, reason.
- Creates the rule in one transaction.

`updateShadowMCPAccessRule`

- Auth: session-only, require `org:admin`.
- Inputs: rule id, disposition, access scope, optional project id for project-scoped rules, evidence fields, match breadth, match value, reason.
- Audits before/after snapshots.

`deleteShadowMCPAccessRule`

- Auth: session-only, require `org:admin`.
- Soft-deletes the rule.

## Runtime enforcement

When a Shadow MCP connection/tool call is detected:

1. Evaluate Risk Policy.
2. If no blocking Shadow MCP policy applies, continue.
3. If a blocking policy applies, build normalized evidence:
   - full URL, when available
   - URL host, when available
   - normalized server identity
4. Match active denied Access Rules first.
5. If any deny rule matches, block.
6. Match active allowed Access Rules.
7. A matching organization-scoped allow rule permits the call for any user in the organization.
8. A matching project-scoped allow rule permits the call only when the active project matches the rule project.
9. Otherwise block with the configured Risk Policy message plus request-access guidance when enabled.

Deny rule precedence applies across match breadth and audience. For example, a denied `url_host` project rule blocks that project even if a narrower `full_url` organization allow rule exists.

## Request-access link

Risk Policy custom block messages continue to render first.

When a blocked Shadow MCP event can be requested, append request guidance:

```text
Request access: <dashboard link>
```

The link should include either:

- a signed request token containing the observed block event identity, or
- an opaque block event id that the API can resolve server-side.

The dashboard page calls `createShadowMCPApprovalRequest`, then shows a simple success state.

## Frontend shape

Use the current prototype branch for layout reference, but port intentionally.

Production frontend should:

- Add a Shadow MCP tab under Access.
- Show Requests with filters for all states.
- Show Access Rules with allow/deny filtering.
- Use side sheets for review, manual create, and edit.
- Include `server_identity` as a match option.
- Make deny-rule creation optional when denying a request.
- Let admins choose whether an allow rule applies to the request project or the entire organization.
- Let admins create manual rules scoped to all users in the organization or all users in a selected project.
- Do not expose Shadow MCP runtime access through the role permission picker.
- Continue to separate hosted Gram MCP servers from external/shadow entries.

## Audit logging

Add audited subjects for:

- `shadow_mcp_approval_request`
- `shadow_mcp_access_rule`

Audit actions:

- request create
- request approve
- request deny
- access rule create
- access rule update
- access rule delete

Audit entries should include before/after snapshots for updates and metadata for:

- match breadth changes
- match value changes
- source request id
- access scope and project id
- requester user id/email where applicable

All audited mutations must run in the same transaction as the data changes.

## Implementation PR sequence

1. Design doc PR.
2. Database schema PR only.
3. Management API design, SQLc, service implementation, SDK generation.
4. Runtime enforcement and request-access link.
5. Audit logging coverage.
6. Production dashboard UI replacing mock data.
7. Risk Overview aggregation and navigation polish.

## Tests

Backend:

- Approval request creation is idempotent.
- List requests includes `requested`, `approved`, and `denied`.
- Approve creates or reuses an allowed rule with organization or project scope.
- Deny marks the request denied and only creates a deny rule when requested.
- Deny rules override allowed rules.
- Project-scoped allow rules only allow the matching project.
- Organization-scoped allow rules allow every project in the organization.
- Soft-deleted rules do not enforce.
- Mutations require `org:admin`.
- Audit events are emitted for every mutation.

Frontend:

- Requests table renders all states and filters correctly.
- Approval sheet creates an allow rule with the selected audience.
- Deny sheet supports optional deny-rule creation.
- Access Rules table filters allow/deny.
- Rule edit/delete flows update local/server state.
- Role editor does not expose Shadow MCP runtime access as a role grant.

## Open questions

1. What exact block event identifier already exists and can safely power the request-access link?
2. Should exact allow/deny conflicts be prevented at write time, or allowed with a UI warning because deny wins?
3. If granular audiences are needed later, should they attach to users, custom roles, groups, or a separate audience table?
