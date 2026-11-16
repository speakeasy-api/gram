---
name: maintaining-admin-mcp
description: Use when adding, changing, or removing admin dashboard workflows, backend admin APIs, staff permissions, or staff-facing product capabilities in client/admin, server/internal/admin, or server/internal/adminmcp. Assess staff Admin MCP parity and update its tools where applicable.
---

# Maintaining Staff Admin MCP

Staff Admin MCP is a staff-only surface in `server/internal/adminmcp/`. Use this skill when a dashboard/admin capability changes or when editing Admin MCP itself. Read the corresponding `server/internal/admin/` service and `client/admin/` workflow before deciding how to maintain parity.

## Assessment

1. Describe the staff outcome and current dashboard behaviour. Identify the actor, target organisation/project, and evidence that the outcome succeeded.
2. Compare with existing Admin MCP tools. Decide to update a tool, add an outcome-oriented tool, or omit MCP support. Record the decision and reason in the change or PR review. A new dashboard button does not automatically warrant an MCP tool.
3. For additions or changes, keep typed bounded inputs/outputs, descriptions, annotations, server instructions and focused tests in sync. Assess customer Platform MCP separately with `maintaining-platform-mcp` when applicable.
4. Never expose secrets, broad SQL/HTTP proxies, cross-tenant identifiers as authority, or customer-supplied text as instructions. Reuse business service logic where it preserves existing safeguards, not customer principals or tenant-bound Platform MCP storage.
5. For writes, register the tool with `addWriteTool`, which appends the confirmation request and marks it destructive, and run the change through `runWrite` (or `runWriteOn` when it holds connection-scoped locks) so it shares a transaction with its staff event row. Check `requireWriteAuthority` first, read and change the target under row locks inside the transaction, return before/after state in the receipt where the change replaces existing state, and keep the audit and privacy checks. Tenant- or project-scoped operations require explicit canonical tenant/project targeting; platform-global operations (support matrix, registry, global issuers) have no tenant target. There is no server-side preview or approval: applying a change depends on the agent getting explicit user confirmation, as the server instructions and tool descriptions demand, so keep that wording intact. A write must be safe to retry: set a value rather than add to it, or take the expected current revision as input and return `ErrStaleState` when it differs. Do not add a write tool when any boundary is unavailable. `create_image_upload_url` is the one write outside these helpers: it issues a token and changes no row, so it has no staff event.
6. Preserve live staff verification and `admin:read`/future `admin:write` scope enforcement at invocation. Tailnet-only route access must be verified separately; tool discovery is not an authorization boundary.
7. Validate with focused Admin MCP and affected admin tests via `mise run test:server`, plus server build/lint and Platform MCP regressions for shared changes. When dashboard UI changes, run its existing checks. Document unavailable infrastructure instead of claiming end-to-end parity.

## Review checklist

- Is the dashboard outcome represented accurately in Admin MCP, or was an explicit update/add/omit decision recorded?
- Are exact targets, live staff authorisation, bounded results and secret omissions preserved?
- Do tool metadata and tests reflect the new behaviour, including errors and unavailable dependencies?
- For writes, are cross-tenant mismatch, retry safety, audit masking and concurrent execution covered?
- Does the change leave customer Platform MCP and managed assistants unaffected unless separately intended?
