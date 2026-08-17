---
"server": minor
---

The MCP approval workflow no longer sits behind the `mcp_approval` product feature. The flag was never enabled anywhere in production and had no enablement surface — approval is part of the shadow-MCP risk surface, gated on `org:admin` like the policies that do the blocking, so a per-organization entitlement added an outage mode without adding control. Blocked-server redemptions now always land in the approval flow rather than falling back to legacy bypass requests when the flag was off.
