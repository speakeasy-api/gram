# Shadow MCP access controls — implementation design

**Date:** 2026-05-11
**Status:** Draft
**Source RFC:** Notion `RFP: Shadow MCP Access Controls`
**Prototype reference branch:** `alexm/age-2208-feat-mock-frontend-for-admin-page`

## Goal

Make Shadow MCP access production-ready by expanding existing Risk Policy and Roles systems instead of creating a separate control plane.

Risk Policy decides whether Shadow MCP usage is blocked or flagged. Roles and dedicated `shadow_mcp:connect` grants decide whether a role may use a managed Shadow MCP server when a blocking policy applies. Admin changes are audited.

## Non-goals

- Do not replace Risk Policy as the Shadow MCP block/flag source of truth.
- Do not auto-allow a Shadow MCP server when an admin creates a Gram-hosted MCP server.
- Do not create a second role/permission model outside existing access grants.
- Do not surface detailed block data to admins until the blocked user requests access.

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

Allowed rules are selectable by admins when granting role access. Denied rules are global enforcement entries and take precedence over allowed rules and role grants.

Match breadth:

- `full_url` — default when a URL is available.
- `url_host` — broader match for all endpoints on the host.
- `server_identity` — fallback for local, command-based, or non-URL MCP servers.

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

Org-scoped managed server list.

Suggested fields:

- `id uuid primary key`
- `organization_id text not null`
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

Use a partial uniqueness guard for active rules by organization, match breadth, and match value. If a future use case needs both allow and deny at the exact same match, require an explicit product decision because deny precedence will make the allow ineffective.

### Role grants

Do not add a separate role-to-rule join table for v1.

Use existing `principal_grants` rows, but keep Shadow MCP in its own scope/resource family:

- `scope = 'shadow_mcp:connect'`
- `principal_urn = role principal`
- selector `resource_kind = 'shadow_mcp'`
- selector `resource_id = <shadow_mcp_access_rules.id>`

Access Rule approval and manual Access Rule assignment must only assign editable/custom roles. Built-in system roles (`admin`, `member`) are seeded from `SystemRoleGrants` and are not editable through the normal role editor, so Shadow MCP approval must not mutate those grants as a side effect. If admins need broad access, they can assign `shadow_mcp:connect` with a wildcard selector to an editable role through the normal permission picker; do not seed that wildcard grant by default.

At runtime, a matching allowed Access Rule becomes the Shadow MCP resource checked through RBAC:

```text
authz.ShadowMCPConnectCheck(access_rule.id, project_id)
```

Do not model this with hosted MCP `mcp:connect`. Current built-in roles can carry broad hosted-MCP grants, and those grants must not satisfy Shadow MCP approval checks. `shadow_mcp:connect` should not be seeded into built-in admin/member role grants by default; access comes from explicit rule approval or manual Access Rule assignment.

If we later need project-specific Shadow MCP grants, keep the `shadow_mcp:connect` scope and use a `project_id` selector dimension under the `shadow_mcp` resource kind rather than falling back to hosted MCP selectors.

## Management API

Add methods to the existing `access` service because the admin surface is Roles & Permissions and the operations mutate role access.

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

- Auth: session/by-key, require `org:read` or `org:admin`.
- Filters: `status`, `project_id`, `cursor`, `limit`.
- Returns all states.

`createShadowMCPApprovalRequest`

- Auth: session user.
- Input should be a signed request token or opaque block event ID from the block response.
- Creates or returns the active request for the observed server.
- Does not require org admin.

`approveShadowMCPApprovalRequest`

- Auth: require `org:admin`.
- Inputs: request id, match breadth, optional edited match value, role ids, admin note.
- Creates an `allowed` Access Rule when needed.
- Adds `shadow_mcp:connect` grants for selected roles to the allowed rule.
- Marks request `approved`.
- Runs in one transaction.

`denyShadowMCPApprovalRequest`

- Auth: require `org:admin`.
- Inputs: request id, admin note, `create_deny_rule` boolean, match breadth, optional edited match value.
- Marks request `denied`.
- If `create_deny_rule` is true, creates a `denied` Access Rule.
- Runs in one transaction.

### Rule APIs

`listShadowMCPAccessRules`

- Auth: require `org:read` or `org:admin`.
- Filters: `disposition`, `cursor`, `limit`.
- Include role grants for allowed rules so the UI can show which roles have access.

`createShadowMCPAccessRule`

- Auth: require `org:admin`.
- Inputs: disposition, evidence fields, match breadth, match value, role ids for allowed rules, reason.
- Creates rule and role grants in one transaction.

`updateShadowMCPAccessRule`

- Auth: require `org:admin`.
- Inputs: rule id, disposition, evidence fields, match breadth, match value, role ids, reason.
- Audits before/after snapshots.
- If disposition changes from allowed to denied, remove role grants to that rule in the same transaction.

`deleteShadowMCPAccessRule`

- Auth: require `org:admin`.
- Soft-deletes the rule.
- Removes role grants to the rule in the same transaction.

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
7. For each matching allowed rule, require `shadow_mcp:connect` on that rule id.
8. If the user has a matching role grant, allow.
9. Otherwise block with the configured Risk Policy message plus request-access guidance when enabled.

Deny rule precedence applies across match breadth. For example, a denied `url_host` rule blocks even if a narrower `full_url` allow rule exists for a role.

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

- Add a Shadow MCP tab in Roles & Permissions.
- Show Requests with filters for all states.
- Show Access Rules with allow/deny filtering.
- Use side sheets for review, manual create, and edit.
- Include `server_identity` as a match option.
- Make deny-rule creation optional when denying a request.
- Default approval role selection from requester context when available.
- Show role impact, such as member counts.
- Integrate allowed Shadow MCP rules into the existing permission picker as an external/shadow server section backed by `shadow_mcp:connect`, not hosted MCP `mcp:connect`.
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

For v1, role assignment changes caused by request approval or Access Rule mutation are captured as part of the `shadow_mcp_access_rule` before/after snapshots and audit metadata. Do not add separate role grant add/remove audit actions unless the audit product later needs role-grant lifecycle events as independently filterable subjects.

Audit entries should include before/after snapshots for updates and metadata for:

- match breadth changes
- match value changes
- source request id
- affected role ids or role slugs
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
- Approve creates or reuses allowed rule and grants selected roles.
- Deny marks the request denied and only creates a deny rule when requested.
- Deny rules override allowed rules.
- Role without `shadow_mcp:connect` to a matching allow rule remains blocked.
- Soft-deleted rules do not enforce.
- Mutations require `org:admin`.
- Audit events are emitted for every mutation.

Frontend:

- Requests table renders all states and filters correctly.
- Approval sheet creates an allow rule and role grants.
- Deny sheet supports optional deny-rule creation.
- Access Rules table filters allow/deny.
- Rule edit/delete flows update local/server state.
- MCP permission picker can select hosted and external/shadow servers separately.

## Open questions

1. Should Access Rules be purely org-scoped in v1, or should the API expose an optional project selector immediately?
2. What exact block event identifier already exists and can safely power the request-access link?
3. Should exact allow/deny conflicts be prevented at write time, or allowed with a UI warning because deny wins?
4. Should approval request creation be available to API-key actors, or only authenticated dashboard users?
