# Internal audit events

## Goal

Replace PR #8433's permanent `catalog_promotion` subject-type exclusion with an explicit, persisted `is_internal` classification. Internal events remain auditable by staff but are neither exposed to customers nor published to the outbox.

## Agreed semantics

- `is_internal = true`: persist the audit row, include it in staff activity, exclude it from customer audit lists and facets, and do not publish any outbox event.
- Suppressing publication applies to every outbox consumer, including customer webhooks and internal growth signals. This is not merely a presentation flag.
- Classify events explicitly at their audit writer. Do not infer classification from actor identity, staff status, or subject type at read time.
- Default existing and unrelated event writers to `false`.
- Staff actions affecting customer resources are not inherently internal; classify according to whether the activity is customer-relevant.
- `WithoutOutbox` retains its independent operator-backfill purpose: suppressing publication does not itself make a row internal.

## Initial event coverage

All existing catalog-management and promotion audit actions are internal:

| Action                         | Subject type        |
| ------------------------------ | ------------------- |
| `catalog-promotion:archive`    | `catalog_promotion` |
| `catalog-promotion:reject`     | `catalog_promotion` |
| `catalog-promotion:restore`    | `catalog_promotion` |
| `catalog-promotion:retry-docs` | `catalog_promotion` |
| `mcp-server:promote-catalogue` | `mcp_server`        |

Publication is a staff operation through the admin service or Admin MCP. There is no customer-initiated promotion exception. The promotion action must be classified even though its subject type differs from the curation actions. Ordinary MCP-server creation, updates, and deletion remain unchanged.

## Storage and write path

Add `is_internal BOOLEAN NOT NULL DEFAULT false` to audit storage, keeping the security-audit mirror used by the PR aligned where required by its schema/query contracts. Modify schema sources and generate migrations and SQLc bindings using repository workflows; do not hand-edit generated artifacts.

Set the flag in the five relevant writers under `server/internal/audit/catalogpromotions.go` and `server/internal/audit/mcpservers.go`. The shared logger persists the classification in the existing mutation transaction and skips outbox publication when the event is internal, independently of the logger's `WithoutOutbox` option. Avoid relying on a special logger instance at each service call site.

## Read paths and staff presentation

Replace the PR's literal exclusion with flag-based filtering in `ListAuditLogs` and actor, action, and surface facet queries. Customer callers cannot opt into internal events, including by explicitly filtering by action, subject type, subject ID, or subject-ID list. Preserve unrelated assistant-event filtering.

Staff organization activity explicitly includes internal events alongside other rows, with unchanged ordering and pagination. Add an **Internal** badge to internal rows in the admin activity UI; do not add a filter or toggle.

Expose `is_internal` as a required boolean in the shared audit-log response model and regenerate affected API/SDK bindings. Customer responses therefore contain `false`; the field does not grant access to internal rows.

Both Admin MCP tools must return the explicit boolean for every row, including `false`:

- `list_organization_activity`
- `list_audit_log_entries`

Update mappings, tool output contracts/descriptions, and tests. Retain existing staff authorization and result-safety behavior. Customer Platform MCP access must preserve the customer API's exclusion; no internal-event opt-in is introduced.

## Rollout and operator backfill

1. Ship and deploy the additive migration in a prerequisite schema-only PR, as required by the repository's PostgreSQL migration rules.
2. Merge/deploy the dependent application PR with updated writers and flag-based readers.
3. After old writer instances have drained, the operator runs the supplied backfill script(s).
4. Run verification counts to confirm all targeted historical rows are internal.

The data backfill is not part of the migration. Provide one or more manually invoked scripts with bounded batches, rerunnable updates, progress/count reporting, and before/after verification. Match historical `catalog_promotion` rows plus the exact `mcp-server:promote-catalogue` action, not all `mcp_server` subjects. Cover applicable mirrored storage as well. Update classification only; preserve audit timestamps and other event data, and emit no events.

Use only generic configuration and placeholders in committed scripts and documentation; do not embed tenant identifiers or credentials.

No transitional subject-type exclusion or cleanup PR is required. The accepted tradeoff is that historical events remain customer-visible until the backfill runs, matching their pre-PR visibility. Newly written events are internal once updated writers are deployed. Backfilling audit rows does not retract already-delivered webhooks or remove already-published outbox events; modifying historical outbox delivery is outside this change.

## Documentation and future-agent guidance

Update `.agents/skills/gram-audit-logging/SKILL.md` in the same change. Document the classification, when to use it, when not to infer it, all-consumer outbox suppression, and its distinction from `WithoutOutbox`. Update existing statements implying every audit row publishes an event or is customer-visible. Include required test coverage and synchronize skills through the repository's supported workflow.

Add concise semantics at the flag/shared logger and in API/MCP field descriptions so developers and tool consumers understand the publication consequence without reading the skill.

Relevant implementation guidance: audit logging, PostgreSQL migrations, maintaining Admin MCP, maintaining Platform MCP, writing skills, React best practices, Speakeasy UI conventions, and demo-seed assessment. Assess whether staff demo fixtures need updating without inventing customer-facing catalog activity.

## Verification

- Each of the five writers persists `is_internal = true` and produces no outbox record.
- Ordinary events remain non-internal and retain normal publication; `WithoutOutbox` still suppresses publication without changing classification.
- Customer lists and all facets exclude internal rows, including explicit action/subject filters and mixed internal/non-internal rows of the same subject type.
- Staff reads include both classifications with stable pagination.
- Both Admin MCP tools return explicit true/false classification while preserving authorization.
- Admin UI renders the badge only for internal rows.
- Backfill is rerunnable, covers promotion events with `mcp_server` subjects, leaves unrelated rows unchanged, preserves timestamps, and produces no publication.
- Generated bindings and affected packages pass the smallest relevant checks.

## Non-goals

No general event-visibility enum, classification registry, staff filter UI, event-catalog removal, webhook retraction, or new internal event bus. Future internal event types use the same explicit flag.

## Open questions

None at the product-design level. Keep configuration and security audit storage conversion contracts aligned and enforce classification at their shared publication boundary.
