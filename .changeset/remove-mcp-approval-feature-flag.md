---
"server": minor
---

The MCP approval workflow's rollout gate moves from the `mcp_approval` product feature to the `gram-mcp-approval` PostHog flag, targeted by organization group like other rollout gates. The product feature had no enablement surface and was never on anywhere, so every approval surface answered 403; the PostHog flag is toggled from the console with no deploy or database access. The gate fails closed, and blocked-server redemptions in orgs off the flag still fall back to legacy bypass requests.
