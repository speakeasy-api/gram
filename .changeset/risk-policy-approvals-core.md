---
"server": patch
"dashboard": patch
---

Add the `require_approval` risk policy action for MCP tool calls. A covered call is held and returns a request token with a `curl` command: the agent posts a one-sentence reason to `/approvals/request` with the token, waits with the polling command the response returns (with the Monitor tool in Claude Code), and retries once an approver approves. The token is the only credential either step needs. Each approval releases exactly one retry of the same call. The dashboard shows approval policies read-only until their editor ships.
