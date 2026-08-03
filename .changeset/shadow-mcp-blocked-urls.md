---
"server": minor
---

Add `shadow_mcp_blocked_urls` to risk policy create/update payloads. Allow-all shadow MCP policies carry a canonical blocked-URL list stored as `risk_policy:block` RBAC grants held by the all-users principal — the mirror of `shadow_mcp_allowed_urls`, which reconciles into `risk_policy:bypass` grants on block-all policies. The two lists are disposition-exclusive: blocked URLs are only valid on allow_all policies and allowed URLs only on block_all policies. Blocked URLs may name servers not yet observed in the project inventory (proactive blocking).
