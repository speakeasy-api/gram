---
"server": patch
---

Every result the hosted and platform MCP surfaces return now carries the `resultType` field that MCP 2026-07-28 requires on every result, and identifies the responding server under the `io.modelcontextprotocol/serverInfo` key in the result's `_meta`. Both fields are filled only when missing, so a result relayed from an upstream MCP server keeps whatever the upstream already supplied.
