---
"server": patch
"dashboard": patch
---

Add the `require_approval` risk policy action for MCP tool calls. A covered call is held and returns a request token; the agent files the request with `request_approval` on the new Speakeasy Approvals MCP server, waits with the watcher it returns, and retries once an approver approves. Each approval releases exactly one retry of the same call. The dashboard shows approval policies read-only until their editor ships.
