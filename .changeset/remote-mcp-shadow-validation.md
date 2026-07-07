---
"server": patch
---

Remote MCP Shadow MCP validation now uses the routed remote server identity when clients omit Gram's internal `x-gram-toolset-id` field. Remote MCP `tools/list` schemas are no longer mutated to require that field; calls that still provide a malformed or mismatched stale field are rejected, and the internal field is stripped before forwarding upstream.
