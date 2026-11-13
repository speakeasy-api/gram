---
"server": patch
"dashboard": patch
---

MCP tool rules limited to annotations now treat a tool with no annotations as having an unknown disposition. A rule giving access to, say, read-only tools no longer reaches it, and a rule blocking, say, destructive tools now takes it away. This matches the MCP specification, which treats a tool without annotations as possibly destructive. The MCP access page shows the same result.
