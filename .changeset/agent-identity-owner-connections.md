---
"dashboard": minor
"server": patch
---

Agent identities can use personal OAuth connections assigned by their owner or another authorized editor. Editors can attach only their own connections, remove another member's assignment, and explicitly replace it with their own. MCP client authorization alone does not grant permission to change connections.

Servers appear in one list ordered by recent tool activity, with shared access labeled separately. Assigned connections show whose account is used. All servers preserves valid assignments and names servers without an available connection. Expired assignments are flagged in the agents list and overview and can be refreshed by their account holder. Agent gateways resolve each server's assigned connection when executing tools.
