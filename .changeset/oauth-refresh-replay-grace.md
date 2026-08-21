---
"server": patch
---

Concurrent OAuth refresh requests now share one refresh-token rotation and receive endpoint-valid token responses for a short grace period. This preserves single-use rotation while allowing MCP clients with several open sessions to refresh without racing one another into an `invalid_grant` response.
