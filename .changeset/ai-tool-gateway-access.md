---
"server": patch
---

The MCP gateway now recognises which AI tool is calling it and enforces the organization's decision for that tool. A caller is matched by its CIMD vendor key, its OAuth client id, or the client name it reports at initialize, and a tool the organization has blocked is refused at the gateway. Blocking only takes effect for callers that present a CIMD client id; anything else stays unreviewed.
