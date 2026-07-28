---
"server": minor
---

Add a `shadow_mcp_disposition` field to risk policies. Shadow MCP blocking policies now carry a default disposition — `block_all` (the existing behavior, and the default) or `allow_all` — chosen at creation time. The disposition is immutable after create: switching posture requires deleting and recreating the policy.
