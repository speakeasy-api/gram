---
"server": minor
---

Retire the legacy Shadow MCP inventory enforcement endpoints — upsert/delete policy bypass and block/unblock server — now that every allow and deny travels through a recorded MCP approval decision. resolveShadowMCPInventoryRequest stays while pre-approval bypass requests drain.
