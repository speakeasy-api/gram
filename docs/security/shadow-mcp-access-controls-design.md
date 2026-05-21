# Shadow MCP access controls - implementation design

**Date:** 2026-05-21
**Status:** Draft

## Goal

Make Shadow MCP blocking manageable by adding a request and rule workflow on top of Risk Policy enforcement.

Risk Policy remains the source of truth for whether Shadow MCP usage should be blocked or flagged. Access Rules only answer what happens after a blocking Shadow MCP policy applies:

- matching `allowed` rule: allow the call
- matching `denied` rule: block the call
- no matching rule: block and offer request-access guidance when possible

The storage model is generic enough for future access-controlled resource types, but the first product surface is Shadow MCP.

## Non-goals

- Do not replace Risk Policy as the Shadow MCP block/flag source of truth.
- Do not auto-allow external Shadow MCP servers when an admin creates a hosted Gram MCP server.
- Do not model Shadow MCP runtime access with hosted MCP `mcp:connect` grants.
- Do not expose Shadow MCP access through the role permission picker.
- Do not persist every block event in Postgres.
- Do not add a separate ClickHouse event stream for Shadow MCP access requests in v1.

## Product model

### Approval request

An approval request is created when a blocked user opens a signed request-access link. It exposes the blocked server evidence to org admins.

States:

- `requested`
- `approved`
- `denied`

Requests are historical. Admins can list all states, not only pending requests.

### Access Rule

An Access Rule is a managed decision for an access-controlled resource. Shadow MCP stores rules with `resource_type = 'shadow_mcp'`.

Disposition:

- `allowed`
- `denied`

Denied rules win over allowed rules. This precedence applies across match breadth and audience.

Match breadth:

- `full_url` - default when a URL is available
- `url_host` - broader match for all endpoints on the host
- `server_identity` - fallback for local, command-based, or non-URL MCP servers

Match values are normalized before storage and matching. The UI may show a human-friendly display name when one is available, but the stored `match_value` must use the normalized value. For example, a Claude MCP tool named `mcp__claude_ai_Calendly__authenticate` produces the server identity match value `claude_ai_calendly`.

### Audience

The storage and service layer support:

- `organization` - all users in the organization
- `project` - users acting in one project

Shadow MCP v1 should expose project-scoped rules in the dashboard. The dashboard create/review flows send `access_scope = 'project'` with an explicit project id. The broader storage shape is kept so the generic Access Rule model can grow without another schema redesign.

## Data model

Migrations ship in a separate PR and must be generated through Atlas.

### Postgres: `access_approval_requests`

Mutable workflow state for approval requests.

Important fields:

- `id uuid primary key`
- `organization_id text not null`
- `project_id uuid not null`
- `resource_type text not null`
- `requester_user_id text`
- `requester_email text`
- `requester_display_name text`
- `status text not null check in ('requested', 'approved', 'denied')`
- `request_fingerprint text`
- `display_name text`
- `observed_summary jsonb not null default '{}'`
- `blocked_count int not null default 1`
- `first_blocked_at timestamptz`
- `last_blocked_at timestamptz`
- `requested_at timestamptz not null`
- `decided_at timestamptz`
- `decided_by text`
- `decision_note text`
- `created_at timestamptz not null`
- `updated_at timestamptz not null`
- `deleted_at timestamptz`
- `deleted boolean generated from deleted_at`

The active-request uniqueness key is scoped by organization, project, resource type, requester, and request fingerprint while the row is still `requested`.

Repeated request-access submissions for the same active requester fingerprint update this row in place: `blocked_count` increments, `last_blocked_at` advances, and the observed summary/display name are refreshed from the signed token evidence. The first insert emits the create audit event; later idempotent updates do not create duplicate request-create audit rows.

### Postgres: `access_rules`

Mutable rule state used by runtime enforcement.

Important fields:

- `id uuid primary key`
- `organization_id text not null`
- `project_id uuid`
- `access_scope text not null check in ('organization', 'project')`
- `resource_type text not null`
- `disposition text not null check in ('allowed', 'denied')`
- `match_kind text not null`
- `match_value text not null`
- `display_name text not null`
- `observed_summary jsonb not null default '{}'`
- `source_request_id uuid references access_approval_requests(id) on delete set null`
- `created_by text`
- `updated_by text`
- `reason text`
- `created_at timestamptz not null`
- `updated_at timestamptz not null`
- `deleted_at timestamptz`
- `deleted boolean generated from deleted_at`

`access_scope = 'organization'` requires `project_id is null`. `access_scope = 'project'` requires `project_id is not null`.

Uniqueness is enforced for active rules by:

- organization-scoped: organization, resource type, match kind, match value
- project-scoped: organization, project, resource type, match kind, match value

If a future use case needs both allow and deny at the exact same match and audience, make that an explicit product decision. Today deny precedence would make the allow ineffective.

## Management API

The public methods remain Shadow MCP-specific because the first dashboard surface is Shadow MCP-specific. Internally they use the generic `access_approval_requests` and `access_rules` tables with `resource_type = 'shadow_mcp'`.

Request APIs:

- `listShadowMCPApprovalRequests`
- `createShadowMCPApprovalRequest`
- `approveShadowMCPApprovalRequest`
- `denyShadowMCPApprovalRequest`

Rule APIs:

- `listShadowMCPAccessRules`
- `createShadowMCPAccessRule`
- `updateShadowMCPAccessRule`
- `deleteShadowMCPAccessRule`

Auth rules:

- request creation: signed request token from a blocked session
- list requests: session-only, require `org:admin`
- approve/deny requests: session-only, require `org:admin`
- list rules: session-only, require `org:read` or `org:admin`
- create/update/delete rules: session-only, require `org:admin`

API keys are intentionally excluded from Shadow MCP request/rule management because these operations are org-admin workflow actions, not producer/consumer API-key operations.

Approve creates or reuses allowed Access Rules in the same transaction as the request decision. Deny marks the request denied and can create denied Access Rules in the same transaction.

## Runtime enforcement

When a Shadow MCP connection or tool call is detected:

1. Resolve org/project metadata and user identity. Missing metadata fails closed.
2. Evaluate Risk Policy.
3. If no blocking Shadow MCP policy applies, continue.
4. Build normalized evidence from the hook payload.
5. Query active `access_rules` for `resource_type = 'shadow_mcp'`, matching organization and either organization scope or the active project.
6. If a matching denied rule exists, block.
7. If a matching allowed rule exists, allow.
8. Otherwise block with the configured Risk Policy message plus request-access guidance when the hook has enough evidence to sign a request token.

Runtime matching uses the normalized evidence candidates in this order:

- full URL
- URL host
- server identity

The SQL orders denied rules ahead of allowed rules, so the evaluator can preserve deny-wins behavior without an extra evaluator table.

## Evidence sources

Access Rules should only be created from evidence the runtime actually observed. Do not infer a URL or hostname from a server label or brand name.

- Cursor URL-based MCP execution can provide an MCP server URL. Runtime evidence should include normalized `full_url`, `url_host`, and any server identity present in the payload.
- Claude Code `PreToolUse` payloads identify MCP calls by tool name, such as `mcp__<server>__<tool>`. When no URL or host is present, runtime evidence should include only normalized `server_identity`.
- Codex follows the same rule: create URL or host evidence only when the hook payload carries it; otherwise fall back to normalized `server_identity`.

Hook scripts should stay lean. They should forward payload evidence and explicitly designed metadata, not parse local LLM configuration files to synthesize evidence.

## Request-access link

Risk Policy custom block messages continue to render first.

When a blocked Shadow MCP event can be requested, append request guidance:

```text
Request access: <dashboard link>
```

The link contains a signed token in the URL fragment. The token carries enough normalized observed evidence and request identity to call `createShadowMCPApprovalRequest`. The dashboard strips the fragment from browser history after reading it.

## Frontend shape

Production dashboard:

- Adds a Shadow MCP tab under Access.
- Shows Requests with status filters.
- Shows Access Rules with allow/deny filtering and pagination.
- Uses side sheets for review, manual create, and edit.
- Includes `server_identity` as a match option.
- Uses project-scoped create/review flows for v1.
- Does not expose Shadow MCP runtime access through the role permission picker.
- Keeps hosted Gram MCP servers separate from external/shadow entries.

## Audit logging

Audited subjects:

- `shadow_mcp_approval_request`
- `shadow_mcp_access_rule`

Audit actions:

- request create
- request approve
- request deny
- access rule create
- access rule update
- access rule delete

Audit entries include before/after snapshots for updates and metadata for match breadth/value, source request id, access scope, project id, requester identity, and admin reason where applicable.

All audited mutations run in the same transaction as the data changes.

## Implementation PR sequence

1. Design doc PR.
2. Database schema PR only.
3. Management API design, SQLc, service implementation, audit logging, SDK generation.
4. Runtime enforcement.
5. Request-access signed link and request page.
6. Production dashboard UI.

## Tests

Backend:

- Approval request creation is idempotent for an active requester fingerprint.
- List requests includes `requested`, `approved`, and `denied`.
- Approve creates or reuses allowed project-scoped rules.
- Deny marks the request denied and creates deny rules only when requested.
- Deny rules override allowed rules.
- Project-scoped allow rules only allow the matching project.
- Soft-deleted rules do not enforce.
- Mutations require `org:admin`.
- Audit events are emitted for every mutation.
- Runtime enforcement fails closed when metadata or rule evaluation fails.
- URL, host, and server identity evidence normalize consistently across hooks and API writes.

Frontend:

- Requests table renders all states and filters correctly.
- Approval sheet creates a project-scoped allow rule.
- Deny sheet creates a project-scoped deny rule.
- Access Rules table filters allow/deny and paginates.
- Rule edit/delete flows update server state and invalidate queries.
- Role editor does not expose Shadow MCP runtime access as a role grant.

## Open questions

1. Should org-scoped Shadow MCP rules remain API-capable but dashboard-hidden, or should the API reject them until the product surface is ready?
2. Should exact allow/deny conflicts stay write-time conflicts, or be allowed with a UI warning because deny wins?
3. When another resource type adopts generic Access Rules, should it reuse the Shadow MCP-shaped endpoints or get a resource-neutral access API?
