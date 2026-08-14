---
"server": patch
---

Concurrent OAuth refresh requests now share one rotated token response for a short grace period. This preserves single-use refresh-token rotation while allowing MCP clients with several open sessions to refresh without racing one another into an `invalid_grant` response.
