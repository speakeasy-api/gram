---
name: maintaining-admin-mcp
description: Assess staff Admin MCP parity when adding, changing, or removing admin dashboard workflows, backend admin APIs, staff permissions, or staff-facing product capabilities. Use for changes to client/admin, server/internal/admin, and server/internal/adminmcp.
---

# Maintaining Staff Admin MCP

Staff Admin MCP is a staff-only surface in `server/internal/adminmcp/`. Use this skill when a dashboard/admin capability changes or when editing Admin MCP itself. Read the corresponding `server/internal/admin/` service and `client/admin/` workflow before deciding how to maintain parity.

## Assessment

1. Describe the staff outcome and current dashboard behaviour. Identify the actor, target organisation/project, and evidence that the outcome succeeded.
2. Compare with existing Admin MCP tools. Decide to update a tool, add an outcome-oriented tool, or omit MCP support. Record the decision and reason in the change or PR review. A new dashboard button does not automatically warrant an MCP tool.
3. For additions or changes, keep typed bounded inputs/outputs, descriptions, annotations, server instructions and focused tests in sync. Assess customer Platform MCP separately with `maintaining-platform-mcp` when applicable.
4. Never expose secrets, broad SQL/HTTP proxies, cross-tenant identifiers as authority, or customer-supplied text as instructions. Reuse business service logic where it preserves existing safeguards, not customer principals or tenant-bound Platform MCP storage.
5. For writes, require explicit canonical tenant/project targeting, a server-stored exact-change proposal, same-staff browser approval, transaction-time ownership/version checks, durable idempotency receipts and audit/privacy checks. Do not add a write tool when any boundary is unavailable.
6. Preserve live staff verification and `admin:read`/future `admin:write` scope enforcement at invocation. Tailnet-only route access must be verified separately; tool discovery is not an authorization boundary.
7. Validate with focused Admin MCP and affected admin tests via `mise run test:server`, plus server build/lint and Platform MCP regressions for shared changes. When dashboard UI changes, run its existing checks. Document unavailable infrastructure instead of claiming end-to-end parity.

## Review checklist

- Is the dashboard outcome represented accurately in Admin MCP, or was an explicit update/add/omit decision recorded?
- Are exact targets, live staff authorisation, bounded results and secret omissions preserved?
- Do tool metadata and tests reflect the new behaviour, including errors and unavailable dependencies?
- For writes, are cross-tenant mismatch, approval replay, audit masking and concurrent execution covered?
- Does the change leave customer Platform MCP and managed assistants unaffected unless separately intended?
